import { newId } from "@/shared/lib/id";
import {
  changedFields,
  installClosedOutbox,
  installCreatedOutbox,
  mountClosedOutbox,
  mountCreatedOutbox,
  movementOutboxStatement,
  notifyLocalWrite,
  outboxStatement,
} from "@/sync/outbox";
import { OpKind } from "@/sync/signing";
import { LocalConstraintError, CorruptLocalDataError } from "@/db/errors";

import type {
  Car,
  CarInput,
  CarSummary,
  Engine,
  EngineInput,
  EngineMount,
  CountDraft,
  EngineWithMount,
  InstallFilter,
  InstallPartInput,
  PartCreateInput,
  PartInstall,
  PartKind,
  PartInput,
  PartWithStock,
  ReadScope,
  RemovePartInput,
  ShoppingLine,
  StockChange,
  StockMovement,
  StockMovementInput,
  TyreSet,
  TyreSetInput,
} from "../domain/models";
import { GarageConflictError, GarageNotFoundError } from "../domain/errors";
import {
  carRowSchema,
  countDraftRowSchema,
  countLineRowSchema,
  engineMountRowSchema,
  engineRowSchema,
  engineWithMountRowSchema,
  partRowSchema,
  partInstallRowSchema,
  shoppingItemStateRowSchema,
  shoppingLineRowSchema,
  stockMovementRowSchema,
  tyreSetRowSchema,
} from "../domain/schemas";
import type { SqlStatement } from "@/db/protocol";

import type { GarageRepository } from "./garage-repository";
import { defaultGarageDatabase, type GarageDatabase } from "./garage-database";

const CAR_COLUMNS = `
  id, team_id, make, model, model_year, serial_number, nickname, transponder_number,
  status, scale, powertrain, notes, created_at, updated_at, version, local_rev
`;

const ENGINE_RAW_COLUMNS = `
  id, team_id, make, model, serial_number, purchased_on, status, tanks_total,
  run_seconds_total, sessions_total, notes, created_at, updated_at, version, local_rev
`;

const ENGINE_SELECT_COLUMNS = `
  e.id, e.team_id, e.make, e.model, e.serial_number, e.purchased_on, e.status,
  e.tanks_total, e.run_seconds_total, e.sessions_total, e.notes, e.created_at,
  e.updated_at, e.version, e.local_rev
`;

const ENGINE_WITH_MOUNT_COLUMNS = `
  ${ENGINE_SELECT_COLUMNS},
  m.car_id AS current_car_id,
  m.mounted_at AS current_mounted_at,
  c.make AS current_car_make,
  c.model AS current_car_model,
  c.nickname AS current_car_nickname,
  c.status AS current_car_status
`;

const TYRE_COLUMNS = `
  id, team_id, label, make, model, compound, insert_type, glued_on, status,
  sessions_total, run_seconds_total, wear_notes, created_at, updated_at, version, local_rev
`;

/** The unit cost is only selected for readers allowed to see costs (ADR 0023). */
function partSelect(scope: ReadScope): string {
  return `
  SELECT
    p.id, p.team_id, p.kind, p.brand, p.reference, p.description, p.unit, p.specs,
    p.min_stock_centi, p.reorder_to_centi, p.has_service_life, p.created_at, p.updated_at,
    p.version, p.local_rev,${scope.costs ? " p.unit_cost_cents," : ""}
    COALESCE(SUM(m.qty_delta_centi), 0) AS qty_centi,
    CASE WHEN COALESCE(SUM(m.qty_delta_centi), 0) < p.min_stock_centi THEN 1 ELSE 0 END AS below_min
  FROM part p
  LEFT JOIN stock_movement m
    ON m.team_id = p.team_id AND m.part_id = p.id
`;
}

const NO_COSTS: ReadScope = { costs: false };

/**
 * Same housekeeping as the server's afterStockChange: a purchase takes the part off the
 * shopping list; any other change brings a hidden automatic line back once the stock is
 * at the minimum again.
 */
function afterStockChange(teamId: string, partId: string, purchase: boolean): SqlStatement {
  const params = { $teamId: teamId, $partId: partId };
  if (purchase) {
    return {
      sql: "DELETE FROM shopping_item WHERE team_id = $teamId AND part_id = $partId",
      params,
    };
  }
  return {
    sql: `DELETE FROM shopping_item
           WHERE team_id = $teamId AND part_id = $partId AND dismissed = 1
             AND (SELECT COALESCE(SUM(qty_delta_centi), 0) FROM stock_movement
                   WHERE team_id = $teamId AND part_id = $partId)
                 >= (SELECT min_stock_centi FROM part WHERE team_id = $teamId AND id = $partId)`,
    params,
  };
}

const MOVEMENT_COLUMNS = `
  m.id, m.part_id, m.qty_delta_centi, m.reason, m.loss_reason, m.note, m.store,
  m.occurred_at, m.created_at, m.created_by, m.reverts_movement_id,
  EXISTS (
    SELECT 1 FROM stock_movement r
     WHERE r.team_id = m.team_id AND r.reverts_movement_id = m.id
  ) AS undone
`;

const ENGINE_JOIN = `
  FROM engine e
  LEFT JOIN engine_mount m
    ON m.team_id = e.team_id AND m.engine_id = e.id AND m.unmounted_at IS NULL
  LEFT JOIN car c
    ON c.team_id = m.team_id AND c.id = m.car_id
`;

type Clock = () => string;

function parse<T>(schema: { safeParse: (value: unknown) => { success: boolean; data?: T; error?: unknown } }, row: unknown): T {
  const result = schema.safeParse(row);
  if (!result.success || result.data === undefined) {
    throw new CorruptLocalDataError("a garage row in the local database is invalid", {
      cause: result.error,
    });
  }
  return result.data;
}

/** The day a movement happened on: now when it is today, otherwise noon UTC of that day. */
function occurredAt(day: string, now: string): string {
  return day === now.slice(0, 10) ? now : `${day}T12:00:00.000Z`;
}

export class LocalGarageRepository implements GarageRepository {
  constructor(
    private readonly db: GarageDatabase = defaultGarageDatabase,
    private readonly now: Clock = () => new Date().toISOString(),
  ) {}

  async listCars(teamId: string): Promise<Car[]> {
    const rows = await this.db.query(
      `SELECT ${CAR_COLUMNS}
         FROM car
        WHERE team_id = $teamId AND deleted_at IS NULL
        ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'spare' THEN 1 ELSE 2 END,
                 updated_at DESC, make COLLATE NOCASE, model COLLATE NOCASE`,
      { $teamId: teamId },
    );
    return rows.map((row) => this.carFrom(row));
  }

  async getCar(teamId: string, id: string): Promise<Car | undefined> {
    const rows = await this.db.query(
      `SELECT ${CAR_COLUMNS}
         FROM car
        WHERE team_id = $teamId AND id = $id AND deleted_at IS NULL`,
      { $teamId: teamId, $id: id },
    );
    return rows[0] ? this.carFrom(rows[0]) : undefined;
  }

  async createCar(teamId: string, input: CarInput): Promise<Car> {
    const id = newId();
    const now = this.now();
    try {
      const rows = await this.writeTx(
        [
          // First: the last statement must be the one that returns the row.
          outboxStatement({
            teamId,
            entity: "garage.car",
            entityId: id,
            kind: OpKind.create,
            baseVersion: null,
            fields: {
              make: input.make,
              model: input.model,
              model_year: input.modelYear,
              serial_number: input.serialNumber,
              nickname: input.nickname,
              transponder_number: input.transponderNumber,
              status: input.status,
              scale: "1/8",
              powertrain: "nitro",
              notes: input.notes,
            },
          }),
          {
            sql: `INSERT INTO car (
                    id, team_id, make, model, model_year, serial_number, nickname,
                    transponder_number, status, scale, powertrain, notes, created_at,
                    updated_at, version, local_rev
                  ) VALUES (
                    $id, $teamId, $make, $model, $modelYear, $serialNumber, $nickname,
                    $transponderNumber, $status, '1/8', 'nitro', $notes, $createdAt,
                    $updatedAt, 1, 0
                  )
                  RETURNING ${CAR_COLUMNS}`,
            params: {
              $id: id,
              $teamId: teamId,
              $make: input.make,
              $model: input.model,
              $modelYear: input.modelYear,
              $serialNumber: input.serialNumber,
              $nickname: input.nickname,
              $transponderNumber: input.transponderNumber,
              $status: input.status,
              $notes: input.notes,
              $createdAt: now,
              $updatedAt: now,
            },
          },
        ],
        true,
      );
      return this.carFrom(rows[0]);
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  async updateCar(
    teamId: string,
    id: string,
    localRev: number,
    input: CarInput,
  ): Promise<Car> {
    if (input.status === "retired" && (await this.hasOpenCarMount(teamId, id))) {
      throw new GarageConflictError("retired", "car has an open engine mount");
    }
    const current = await this.getCar(teamId, id);
    if (!current) throw new GarageNotFoundError("car not found");
    try {
      const rows = await this.writeTx(
        [
          // Before the UPDATE: it checks the state the UPDATE is about to match, and the
          // UPDATE stays last because it returns the row.
          ...this.carPatchStatements(teamId, id, localRev, current, input),
          {
            sql: `UPDATE car
                     SET make = $make, model = $model, model_year = $modelYear,
                         serial_number = $serialNumber, nickname = $nickname,
                         transponder_number = $transponderNumber, status = $status,
                         notes = $notes, updated_at = $updatedAt,
                         local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL
                RETURNING ${CAR_COLUMNS}`,
            params: {
              $teamId: teamId,
              $id: id,
              $localRev: localRev,
              $make: input.make,
              $model: input.model,
              $modelYear: input.modelYear,
              $serialNumber: input.serialNumber,
              $nickname: input.nickname,
              $transponderNumber: input.transponderNumber,
              $status: input.status,
              $notes: input.notes,
              $updatedAt: this.now(),
            },
          },
        ],
        true,
      );
      return this.carFrom(rows[0]);
    } catch (error) {
      this.rethrowStale(error, "car");
    }
  }

  async listEngines(teamId: string): Promise<EngineWithMount[]> {
    const rows = await this.db.query(
      `SELECT ${ENGINE_WITH_MOUNT_COLUMNS}
       ${ENGINE_JOIN}
       WHERE e.team_id = $teamId AND e.deleted_at IS NULL
       ORDER BY CASE e.status WHEN 'active' THEN 0 WHEN 'spare' THEN 1 ELSE 2 END,
                e.updated_at DESC, e.make COLLATE NOCASE, e.model COLLATE NOCASE`,
      { $teamId: teamId },
    );
    return rows.map((row) => this.engineWithMountFrom(row));
  }

  async getEngine(teamId: string, id: string): Promise<EngineWithMount | undefined> {
    const rows = await this.db.query(
      `SELECT ${ENGINE_WITH_MOUNT_COLUMNS}
       ${ENGINE_JOIN}
       WHERE e.team_id = $teamId AND e.id = $id AND e.deleted_at IS NULL`,
      { $teamId: teamId, $id: id },
    );
    return rows[0] ? this.engineWithMountFrom(rows[0]) : undefined;
  }

  async createEngine(teamId: string, input: EngineInput): Promise<Engine> {
    const id = newId();
    const now = this.now();
    try {
      const rows = await this.writeTx(
        [
          // First: the last statement must be the one that returns the row.
          outboxStatement({
            teamId,
            entity: "garage.engine",
            entityId: id,
            kind: OpKind.create,
            baseVersion: null,
            fields: {
              make: input.make,
              model: input.model,
              serial_number: input.serialNumber,
              purchased_on: input.purchasedOn,
              status: input.status,
              notes: input.notes,
            },
          }),
          {
            sql: `INSERT INTO engine (
                    id, team_id, make, model, serial_number, purchased_on, status,
                    tanks_total, run_seconds_total, sessions_total, notes, created_at,
                    updated_at, version, local_rev
                  ) VALUES (
                    $id, $teamId, $make, $model, $serialNumber, $purchasedOn, $status,
                    0, 0, 0, $notes, $createdAt, $updatedAt, 1, 0
                  )
                  RETURNING ${ENGINE_RAW_COLUMNS}`,
            params: {
              $id: id,
              $teamId: teamId,
              $make: input.make,
              $model: input.model,
              $serialNumber: input.serialNumber,
              $purchasedOn: input.purchasedOn,
              $status: input.status,
              $notes: input.notes,
              $createdAt: now,
              $updatedAt: now,
            },
          },
        ],
        true,
      );
      return this.engineFrom(rows[0]);
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  async updateEngine(
    teamId: string,
    id: string,
    localRev: number,
    input: EngineInput,
  ): Promise<EngineWithMount> {
    if (input.status === "retired" && (await this.hasOpenEngineMount(teamId, id))) {
      throw new GarageConflictError("retired", "engine has an open mount");
    }
    const queue = this.patchQueue(
      "garage.engine",
      "engine",
      teamId,
      id,
      localRev,
      await this.syncRow("engine", teamId, id, "make, model, serial_number, purchased_on, status, notes"),
      {
        make: input.make,
        model: input.model,
        serial_number: input.serialNumber,
        purchased_on: input.purchasedOn,
        status: input.status,
        notes: input.notes,
      },
    );
    try {
      await this.writeTx(
        [
          ...queue,
          {
            sql: `UPDATE engine
                     SET make = $make, model = $model, serial_number = $serialNumber,
                         purchased_on = $purchasedOn, status = $status, notes = $notes,
                         updated_at = $updatedAt, local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL
                RETURNING id`,
            params: {
              $teamId: teamId,
              $id: id,
              $localRev: localRev,
              $make: input.make,
              $model: input.model,
              $serialNumber: input.serialNumber,
              $purchasedOn: input.purchasedOn,
              $status: input.status,
              $notes: input.notes,
              $updatedAt: this.now(),
            },
          },
        ],
        true,
      );
      const engine = await this.getEngine(teamId, id);
      if (!engine) throw new GarageNotFoundError("engine not found after update");
      return engine;
    } catch (error) {
      this.rethrowStale(error, "engine");
    }
  }

  async listEngineMounts(teamId: string, engineId: string): Promise<EngineMount[]> {
    const rows = await this.db.query(
      `SELECT m.id, m.engine_id, m.car_id, m.mounted_at, m.unmounted_at,
              c.make AS car_make, c.model AS car_model, c.nickname AS car_nickname,
              c.status AS car_status
         FROM engine_mount m
         JOIN car c ON c.team_id = m.team_id AND c.id = m.car_id
        WHERE m.team_id = $teamId AND m.engine_id = $engineId
        ORDER BY m.mounted_at DESC, m.id DESC`,
      { $teamId: teamId, $engineId: engineId },
    );
    return rows.map((row) => this.mountFrom(row));
  }

  /** The mount row the engine / car has open now, if any. */
  private async openMountId(teamId: string, column: "engine_id" | "car_id", id: string) {
    const rows = await this.db.query<{ id: string }>(
      `SELECT id FROM engine_mount WHERE team_id = $teamId AND ${column} = $id AND unmounted_at IS NULL`,
      { $teamId: teamId, $id: id },
    );
    return rows[0]?.id ?? null;
  }

  /** Closes a mount and queues the close. Nothing is queued when it was no longer open. */
  private closeMountStatements(teamId: string, mountId: string, at: string): SqlStatement[] {
    return [
      {
        sql: `UPDATE engine_mount SET unmounted_at = $at
               WHERE team_id = $teamId AND id = $mountId AND unmounted_at IS NULL`,
        params: { $teamId: teamId, $mountId: mountId, $at: at },
      },
      mountClosedOutbox(teamId, mountId, at),
    ];
  }

  /**
   * Opens a mount (only if neither the engine nor the car has one open and both are usable)
   * and queues it. The last statement makes the transaction fail when nothing was inserted.
   */
  private openMountStatements(teamId: string, engineId: string, carId: string, mountId: string, at: string): SqlStatement[] {
    return [
      {
        sql: `INSERT INTO engine_mount (
                id, team_id, engine_id, car_id, mounted_at, unmounted_at, created_at
              )
              SELECT $id, e.team_id, e.id, c.id, $mountedAt, NULL, $createdAt
                FROM engine e
                JOIN car c ON c.team_id = e.team_id AND c.id = $carId
               WHERE e.team_id = $teamId AND e.id = $engineId
                 AND e.deleted_at IS NULL AND c.deleted_at IS NULL
                 AND e.status <> 'retired' AND c.status <> 'retired'
                 AND NOT EXISTS (
                   SELECT 1 FROM engine_mount
                    WHERE team_id = $teamId AND engine_id = $engineId
                      AND unmounted_at IS NULL
                 )
                 AND NOT EXISTS (
                   SELECT 1 FROM engine_mount
                    WHERE team_id = $teamId AND car_id = $carId
                      AND unmounted_at IS NULL
                 )`,
        params: {
          $id: mountId,
          $teamId: teamId,
          $engineId: engineId,
          $carId: carId,
          $mountedAt: at,
          $createdAt: at,
        },
      },
      mountCreatedOutbox(teamId, mountId),
      {
        sql: "SELECT id FROM engine_mount WHERE team_id = $teamId AND id = $id",
        params: { $teamId: teamId, $id: mountId },
      },
    ];
  }

  async mountEngine(teamId: string, engineId: string, carId: string): Promise<void> {
    await this.mountWrite(this.openMountStatements(teamId, engineId, carId, newId(), this.now()));
  }

  async unmountEngine(teamId: string, engineId: string): Promise<void> {
    const mountId = await this.openMountId(teamId, "engine_id", engineId);
    if (mountId === null) throw new GarageConflictError("mount", "engine is not mounted");
    const at = this.now();
    await this.mountWrite([
      ...this.closeMountStatements(teamId, mountId, at),
      {
        sql: "SELECT id FROM engine_mount WHERE team_id = $teamId AND id = $mountId AND unmounted_at = $at",
        params: { $teamId: teamId, $mountId: mountId, $at: at },
      },
    ]);
  }

  async moveEngine(teamId: string, engineId: string, toCarId: string): Promise<void> {
    const current = await this.db.query<{ id: string; car_id: string }>(
      `SELECT id, car_id FROM engine_mount
        WHERE team_id = $teamId AND engine_id = $engineId AND unmounted_at IS NULL`,
      { $teamId: teamId, $engineId: engineId },
    );
    if (current[0]?.car_id === toCarId) return;
    const at = this.now();
    await this.mountWrite([
      ...(current[0] ? this.closeMountStatements(teamId, current[0].id, at) : []),
      ...this.openMountStatements(teamId, engineId, toCarId, newId(), at),
    ]);
  }

  async swapCarEngine(teamId: string, carId: string, engineId: string): Promise<void> {
    const current = await this.db.query<{ id: string; engine_id: string }>(
      `SELECT id, engine_id FROM engine_mount
        WHERE team_id = $teamId AND car_id = $carId AND unmounted_at IS NULL`,
      { $teamId: teamId, $carId: carId },
    );
    if (current[0]?.engine_id === engineId) return;
    const at = this.now();
    await this.mountWrite([
      ...(current[0] ? this.closeMountStatements(teamId, current[0].id, at) : []),
      ...this.openMountStatements(teamId, engineId, carId, newId(), at),
    ]);
  }

  async listTyreSets(teamId: string): Promise<TyreSet[]> {
    const rows = await this.db.query(
      `SELECT ${TYRE_COLUMNS}
         FROM tyre_set
        WHERE team_id = $teamId AND deleted_at IS NULL
        ORDER BY CASE status WHEN 'new' THEN 0 WHEN 'used' THEN 1 WHEN 'worn' THEN 2 ELSE 3 END,
                 updated_at DESC, label COLLATE NOCASE`,
      { $teamId: teamId },
    );
    return rows.map((row) => this.tyreFrom(row));
  }

  async getTyreSet(teamId: string, id: string): Promise<TyreSet | undefined> {
    const rows = await this.db.query(
      `SELECT ${TYRE_COLUMNS} FROM tyre_set
        WHERE team_id = $teamId AND id = $id AND deleted_at IS NULL`,
      { $teamId: teamId, $id: id },
    );
    return rows[0] ? this.tyreFrom(rows[0]) : undefined;
  }

  async createTyreSet(teamId: string, input: TyreSetInput): Promise<TyreSet> {
    const id = newId();
    const now = this.now();
    try {
      const rows = await this.writeTx(
        [
          outboxStatement({
            teamId,
            entity: "garage.tyre_set",
            entityId: id,
            kind: OpKind.create,
            baseVersion: null,
            fields: {
              label: input.label,
              make: input.make,
              model: input.model,
              compound: input.compound,
              insert_type: input.insertType,
              glued_on: input.gluedOn,
              status: input.status,
              wear_notes: input.wearNotes,
            },
          }),
          {
            sql: `INSERT INTO tyre_set (
                    id, team_id, label, make, model, compound, insert_type, glued_on,
                    status, sessions_total, run_seconds_total, wear_notes, created_at,
                    updated_at, version, local_rev
                  ) VALUES (
                    $id, $teamId, $label, $make, $model, $compound, $insertType, $gluedOn,
                    $status, 0, 0, $wearNotes, $createdAt, $updatedAt, 1, 0
                  )
                  RETURNING ${TYRE_COLUMNS}`,
            params: {
              $id: id,
              $teamId: teamId,
              $label: input.label,
              $make: input.make,
              $model: input.model,
              $compound: input.compound,
              $insertType: input.insertType,
              $gluedOn: input.gluedOn,
              $status: input.status,
              $wearNotes: input.wearNotes,
              $createdAt: now,
              $updatedAt: now,
            },
          },
        ],
        true,
      );
      return this.tyreFrom(rows[0]);
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  async updateTyreSet(
    teamId: string,
    id: string,
    localRev: number,
    input: TyreSetInput,
  ): Promise<TyreSet> {
    const queue = this.patchQueue(
      "garage.tyre_set",
      "tyre_set",
      teamId,
      id,
      localRev,
      await this.syncRow(
        "tyre_set",
        teamId,
        id,
        "label, make, model, compound, insert_type, glued_on, status, wear_notes",
      ),
      {
        label: input.label,
        make: input.make,
        model: input.model,
        compound: input.compound,
        insert_type: input.insertType,
        glued_on: input.gluedOn,
        status: input.status,
        wear_notes: input.wearNotes,
      },
    );
    try {
      const rows = await this.writeTx(
        [
          ...queue,
          {
            sql: `UPDATE tyre_set
                     SET label = $label, make = $make, model = $model, compound = $compound,
                         insert_type = $insertType, glued_on = $gluedOn, status = $status,
                         wear_notes = $wearNotes, updated_at = $updatedAt,
                         local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL
                  RETURNING ${TYRE_COLUMNS}`,
            params: {
              $teamId: teamId,
              $id: id,
              $localRev: localRev,
              $label: input.label,
              $make: input.make,
              $model: input.model,
              $compound: input.compound,
              $insertType: input.insertType,
              $gluedOn: input.gluedOn,
              $status: input.status,
              $wearNotes: input.wearNotes,
              $updatedAt: this.now(),
            },
          },
        ],
        true,
      );
      return this.tyreFrom(rows[0]);
    } catch (error) {
      this.rethrowStale(error, "tyre set");
    }
  }

  async listParts(teamId: string, scope: ReadScope = NO_COSTS): Promise<PartWithStock[]> {
    const rows = await this.db.query(
      `${partSelect(scope)}
       WHERE p.team_id = $teamId AND p.deleted_at IS NULL
       GROUP BY p.id
       ORDER BY below_min DESC, p.description COLLATE NOCASE, p.id`,
      { $teamId: teamId },
    );
    return rows.map((row) => this.partFrom(row));
  }

  async getPart(
    teamId: string,
    id: string,
    scope: ReadScope = NO_COSTS,
  ): Promise<PartWithStock | undefined> {
    const rows = await this.db.query(
      `${partSelect(scope)}
       WHERE p.team_id = $teamId AND p.id = $id AND p.deleted_at IS NULL
       GROUP BY p.id`,
      { $teamId: teamId, $id: id },
    );
    return rows[0] ? this.partFrom(rows[0]) : undefined;
  }

  async createPart(teamId: string, input: PartCreateInput): Promise<PartWithStock> {
    const id = newId();
    const initialMovementId = newId();
    const now = this.now();
    const statements: SqlStatement[] = [
      // The initial quantity is NOT in this operation: it syncs with its own purchase movement.
      outboxStatement({
        teamId,
        entity: "garage.part",
        entityId: id,
        kind: OpKind.create,
        baseVersion: null,
        fields: {
          kind: input.kind,
          brand: input.brand,
          reference: input.reference,
          description: input.description,
          unit: input.unit,
          specs: input.specs,
          min_stock_centi: input.minStockCenti,
          reorder_to_centi: input.reorderToCenti,
          has_service_life: input.hasServiceLife,
          ...(input.unitCostCents === undefined ? {} : { unit_cost_cents: input.unitCostCents }),
        },
      }),
      {
        sql: `INSERT INTO part (
                id, team_id, kind, brand, reference, description, unit, specs,
                min_stock_centi, reorder_to_centi, unit_cost_cents, currency, has_service_life,
                created_at, updated_at, version, local_rev
              ) VALUES (
                $id, $teamId, $kind, $brand, $reference, $description, $unit, $specs,
                $minStockCenti, $reorderToCenti, $unitCostCents, 'EUR', $hasServiceLife,
                $createdAt, $updatedAt, 1, 0
              )`,
        params: {
          $id: id,
          $teamId: teamId,
          $kind: input.kind,
          $brand: input.brand,
          $reference: input.reference,
          $description: input.description,
          $unit: input.unit,
          $specs: JSON.stringify(input.specs),
          $minStockCenti: input.minStockCenti,
          $reorderToCenti: input.reorderToCenti,
          $unitCostCents: input.unitCostCents ?? null,
          $hasServiceLife: input.hasServiceLife ? 1 : 0,
          $createdAt: now,
          $updatedAt: now,
        },
      },
      ...(input.initialQtyCenti > 0
        ? [
            {
              sql: `INSERT INTO stock_movement (
                      id, team_id, part_id, qty_delta_centi, reason, note, unit_cost_cents,
                      occurred_at, created_at, created_by
                    ) VALUES (
                      $movementId, $teamId, $partId, $qty, 'purchase', NULL, $unitCostCents,
                      $now, $now, $createdBy
                    )`,
              params: {
                $movementId: initialMovementId,
                $teamId: teamId,
                $partId: id,
                $qty: input.initialQtyCenti,
                $unitCostCents: input.unitCostCents ?? null,
                $now: now,
                $createdBy: input.createdBy ?? null,
              },
            },
            movementOutboxStatement(teamId, initialMovementId),
          ]
        : []),
      {
        sql: `${partSelect({ costs: input.unitCostCents !== undefined })}
               WHERE p.team_id = $teamId AND p.id = $id AND p.deleted_at IS NULL
               GROUP BY p.id`,
        params: { $teamId: teamId, $id: id },
      },
    ];

    try {
      const rows = await this.writeTx(statements, true);
      return this.partFrom(rows[0]);
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  async updatePart(
    teamId: string,
    id: string,
    localRev: number,
    input: PartInput,
  ): Promise<PartWithStock> {
    const row = await this.syncRow(
      "part",
      teamId,
      id,
      "kind, brand, reference, description, unit, specs, min_stock_centi, reorder_to_centi, unit_cost_cents, has_service_life",
    );
    const queue = this.patchQueue(
      "garage.part",
      "part",
      teamId,
      id,
      localRev,
      {
        ...row,
        specs: typeof row.specs === "string" ? (JSON.parse(row.specs) as unknown) : row.specs,
        has_service_life: row.has_service_life === 1,
      },
      {
        kind: input.kind,
        brand: input.brand,
        reference: input.reference,
        description: input.description,
        unit: input.unit,
        specs: input.specs,
        min_stock_centi: input.minStockCenti,
        reorder_to_centi: input.reorderToCenti,
        has_service_life: input.hasServiceLife,
        // Left out when the reader may not touch costs (undefined leaves the stored value alone).
        ...(input.unitCostCents === undefined ? {} : { unit_cost_cents: input.unitCostCents }),
      },
    );
    try {
      await this.writeTx(
        [
          ...queue,
          {
            sql: `UPDATE part
                     SET kind = $kind, brand = $brand, reference = $reference,
                         description = $description, unit = $unit, specs = $specs,
                         min_stock_centi = $minStockCenti, reorder_to_centi = $reorderToCenti,
                         unit_cost_cents = CASE WHEN $costSet = 1 THEN $unitCostCents
                                                ELSE unit_cost_cents END,
                         has_service_life = $hasServiceLife, updated_at = $updatedAt,
                         local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL`,
            params: {
              $teamId: teamId,
              $id: id,
              $localRev: localRev,
              $kind: input.kind,
              $brand: input.brand,
              $reference: input.reference,
              $description: input.description,
              $unit: input.unit,
              $specs: JSON.stringify(input.specs),
              $minStockCenti: input.minStockCenti,
              $reorderToCenti: input.reorderToCenti,
              $costSet: input.unitCostCents === undefined ? 0 : 1,
              $unitCostCents: input.unitCostCents ?? null,
              $hasServiceLife: input.hasServiceLife ? 1 : 0,
              $updatedAt: this.now(),
            },
          },
          // A lower minimum may bring a hidden shopping line back.
          afterStockChange(teamId, id, false),
          {
            sql: `SELECT id FROM part
                   WHERE team_id = $teamId AND id = $id AND local_rev = $applied`,
            params: { $teamId: teamId, $id: id, $applied: localRev + 1 },
          },
        ],
        true,
      );
      const part = await this.getPart(teamId, id, { costs: input.unitCostCents !== undefined });
      if (!part) throw new GarageNotFoundError("part not found after update");
      return part;
    } catch (error) {
      this.rethrowStale(error, "part");
    }
  }

  /** Archives a part: it leaves the lists and the shopping list; its history stays. */
  async archivePart(teamId: string, id: string, localRev: number): Promise<void> {
    const { version } = await this.syncRow("part", teamId, id, "id");
    try {
      await this.writeTx(
        [
          outboxStatement(
            { teamId, entity: "garage.part", entityId: id, kind: OpKind.delete, baseVersion: version, fields: {} },
            {
              sql: `SELECT 1 FROM part
                     WHERE team_id = $teamId AND id = $id AND local_rev = $applied AND deleted_at IS NULL`,
              params: { $teamId: teamId, $id: id, $applied: localRev },
            },
          ),
          {
            sql: `UPDATE part
                     SET deleted_at = $now, updated_at = $now, local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL`,
            params: { $teamId: teamId, $id: id, $localRev: localRev, $now: this.now() },
          },
          {
            sql: `DELETE FROM shopping_item
                   WHERE team_id = $teamId AND part_id = $id
                     AND EXISTS (SELECT 1 FROM part WHERE team_id = $teamId AND id = $id
                                    AND deleted_at IS NOT NULL AND local_rev = $applied)`,
            params: { $teamId: teamId, $id: id, $applied: localRev + 1 },
          },
          {
            sql: `SELECT id FROM part
                   WHERE team_id = $teamId AND id = $id AND deleted_at IS NOT NULL
                     AND local_rev = $applied`,
            params: { $teamId: teamId, $id: id, $applied: localRev + 1 },
          },
        ],
        true,
      );
    } catch (error) {
      this.rethrowStale(error, "part");
    }
  }

  async listStockMovements(
    teamId: string,
    partId: string,
    scope: ReadScope = NO_COSTS,
  ): Promise<StockMovement[]> {
    const rows = await this.db.query(
      `SELECT ${MOVEMENT_COLUMNS}${scope.costs ? ", m.unit_cost_cents" : ""}
         FROM stock_movement m
        WHERE m.team_id = $teamId AND m.part_id = $partId
        ORDER BY m.occurred_at DESC, m.rowid DESC`,
      { $teamId: teamId, $partId: partId },
    );
    return rows.map((row) => this.movementFrom(row));
  }

  async addStockMovement(teamId: string, input: StockMovementInput): Promise<StockChange> {
    const id = newId();
    const now = this.now();
    try {
      const rows = await this.writeTx(
        [
          {
            // INSERT ... SELECT so a part that was archived meanwhile inserts nothing.
            sql: `INSERT INTO stock_movement (
                    id, team_id, part_id, qty_delta_centi, reason, loss_reason, note, store,
                    unit_cost_cents, occurred_at, created_at, created_by
                  )
                  SELECT $id, p.team_id, p.id, $qty, $reason, $lossReason, $note, $store,
                         $unitCostCents, $occurredAt, $now, $createdBy
                    FROM part p
                   WHERE p.team_id = $teamId AND p.id = $partId AND p.deleted_at IS NULL`,
            params: {
              $id: id,
              $teamId: teamId,
              $partId: input.partId,
              $qty: input.qtyDeltaCenti,
              $reason: input.reason,
              $lossReason: input.lossReason,
              $note: input.note,
              $store: input.store,
              $unitCostCents: input.unitCostCents,
              $occurredAt: occurredAt(input.occurredOn, now),
              $now: now,
              $createdBy: input.createdBy,
            },
          },
          movementOutboxStatement(teamId, id),
          afterStockChange(teamId, input.partId, input.reason === "purchase"),
          this.movementWithStock(teamId, input.partId, id),
        ],
        true,
      );
      return this.changeFrom(rows[0]);
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  /**
   * Movements are immutable, so undoing one appends the inverse as an adjustment (a purchase
   * can never be negative). A movement can be undone once, and an undo cannot be undone.
   */
  async undoMovement(
    teamId: string,
    movementId: string,
    undo: { occurredOn: string; note: string; createdBy: string | null },
  ): Promise<StockChange> {
    const id = newId();
    const now = this.now();
    try {
      const rows = await this.writeTx(
        [
          {
            sql: `INSERT INTO stock_movement (
                    id, team_id, part_id, qty_delta_centi, reason, note, occurred_at,
                    created_at, created_by, reverts_movement_id
                  )
                  SELECT $id, m.team_id, m.part_id, -m.qty_delta_centi, 'adjustment', $note,
                         $occurredAt, $now, $createdBy, m.id
                    FROM stock_movement m
                    JOIN part p ON p.team_id = m.team_id AND p.id = m.part_id
                   WHERE m.team_id = $teamId AND m.id = $movementId
                     AND m.reverts_movement_id IS NULL AND p.deleted_at IS NULL`,
            params: {
              $id: id,
              $teamId: teamId,
              $movementId: movementId,
              $note: undo.note,
              $occurredAt: occurredAt(undo.occurredOn, now),
              $now: now,
              $createdBy: undo.createdBy,
            },
          },
          movementOutboxStatement(teamId, id),
          {
            sql: `DELETE FROM shopping_item
                   WHERE team_id = $teamId AND dismissed = 1
                     AND part_id = (SELECT part_id FROM stock_movement
                                     WHERE team_id = $teamId AND id = $id)
                     AND (SELECT COALESCE(SUM(qty_delta_centi), 0) FROM stock_movement
                           WHERE team_id = $teamId AND part_id = shopping_item.part_id)
                         >= (SELECT min_stock_centi FROM part
                              WHERE team_id = $teamId AND id = shopping_item.part_id)`,
            params: { $teamId: teamId, $id: id },
          },
          {
            sql: `SELECT ${MOVEMENT_COLUMNS},
                         (SELECT COALESCE(SUM(s.qty_delta_centi), 0) FROM stock_movement s
                           WHERE s.team_id = m.team_id AND s.part_id = m.part_id) AS part_qty_centi,
                         (SELECT min_stock_centi FROM part
                           WHERE team_id = m.team_id AND id = m.part_id) AS part_min_stock_centi
                    FROM stock_movement m
                   WHERE m.team_id = $teamId AND m.id = $id`,
            params: { $teamId: teamId, $id: id },
          },
        ],
        true,
      );
      return this.changeFrom(rows[0]);
    } catch (error) {
      if (error instanceof LocalConstraintError) {
        const message = error.message.toLowerCase();
        if (message.includes("reverts_movement_id") || message.includes("unique")) {
          throw new GarageConflictError("undone", "movement already undone", { cause: error });
        }
        // Nothing was inserted: unknown movement, an undo, or an archived part.
        throw new GarageConflictError("not_serviceable", "movement cannot be undone", {
          cause: error,
        });
      }
      throw error;
    }
  }

  async listPartInstalls(
    teamId: string,
    filter: InstallFilter,
    history = false,
  ): Promise<PartInstall[]> {
    const rows = await this.db.query(
      `SELECT i.id, i.part_id, p.kind AS part_kind, p.description AS part_description,
              p.brand AS part_brand, p.reference AS part_reference, i.car_id, i.engine_id,
              COALESCE(c.nickname, c.make || ' ' || c.model, e.make || ' ' || e.model) AS target_name,
              i.installed_at, i.removed_at, i.removal_destination, i.removal_reason,
              i.run_seconds, i.sessions, i.note
         FROM part_install i
         JOIN part p ON p.team_id = i.team_id AND p.id = i.part_id
         LEFT JOIN car c ON c.team_id = i.team_id AND c.id = i.car_id
         LEFT JOIN engine e ON e.team_id = i.team_id AND e.id = i.engine_id
        WHERE i.team_id = $teamId
          AND ($partId IS NULL OR i.part_id = $partId)
          AND ($carId IS NULL OR i.car_id = $carId)
          AND ($engineId IS NULL OR i.engine_id = $engineId)
          AND ($history = 1 OR i.removed_at IS NULL)
        ORDER BY i.installed_at DESC, i.rowid DESC`,
      {
        $teamId: teamId,
        $partId: "partId" in filter ? filter.partId : null,
        $carId: "carId" in filter ? filter.carId : null,
        $engineId: "engineId" in filter ? filter.engineId : null,
        $history: history ? 1 : 0,
      },
    );
    return rows.map((row) => this.installFrom(row));
  }

  async installPart(teamId: string, input: InstallPartInput): Promise<StockChange> {
    const installId = newId();
    const movementId = newId();
    const now = this.now();
    const at = occurredAt(input.occurredOn, now);
    const carId = "carId" in input.target ? input.target.carId : null;
    const engineId = "engineId" in input.target ? input.target.engineId : null;
    try {
      const rows = await this.writeTx(
        [
          {
            // Nothing is inserted for an archived part, a part without service life or a
            // missing or retired car/engine; the last statement then fails the transaction.
            sql: `INSERT INTO part_install (
                    id, team_id, part_id, car_id, engine_id, installed_at, note,
                    created_at, updated_at
                  )
                  SELECT $installId, p.team_id, p.id, $carId, $engineId, $at, $note, $now, $now
                    FROM part p
                   WHERE p.team_id = $teamId AND p.id = $partId
                     AND p.deleted_at IS NULL AND p.has_service_life = 1
                     AND (
                       ($carId IS NOT NULL AND EXISTS (
                         SELECT 1 FROM car WHERE team_id = $teamId AND id = $carId
                           AND deleted_at IS NULL AND status <> 'retired'))
                       OR ($engineId IS NOT NULL AND EXISTS (
                         SELECT 1 FROM engine WHERE team_id = $teamId AND id = $engineId
                           AND deleted_at IS NULL AND status <> 'retired'))
                     )`,
            params: {
              $installId: installId,
              $teamId: teamId,
              $partId: input.partId,
              $carId: carId,
              $engineId: engineId,
              $at: at,
              $note: input.note,
              $now: now,
            },
          },
          installCreatedOutbox(teamId, installId),
          {
            sql: `INSERT INTO stock_movement (
                    id, team_id, part_id, qty_delta_centi, reason, note, occurred_at,
                    created_at, created_by
                  )
                  SELECT $movementId, i.team_id, i.part_id, -100, 'installed', $note, $at,
                         $now, $createdBy
                    FROM part_install i
                   WHERE i.team_id = $teamId AND i.id = $installId`,
            params: {
              $movementId: movementId,
              $teamId: teamId,
              $installId: installId,
              $note: input.note,
              $at: at,
              $now: now,
              $createdBy: input.createdBy,
            },
          },
          movementOutboxStatement(teamId, movementId),
          afterStockChange(teamId, input.partId, false),
          this.movementWithStock(teamId, input.partId, movementId),
        ],
        true,
      );
      return this.changeFrom(rows[0]);
    } catch (error) {
      if (error instanceof LocalConstraintError && /required at least one row/i.test(error.message)) {
        throw new GarageConflictError("not_serviceable", "part cannot be installed", { cause: error });
      }
      this.rethrowWrite(error);
    }
  }

  async removePart(teamId: string, installId: string, input: RemovePartInput): Promise<void> {
    const movementId = newId();
    const now = this.now();
    const at = occurredAt(input.occurredOn, now);
    try {
      await this.writeTx(
        [
          {
            // Only `stock` returns the unit; a closed install inserts nothing here.
            sql: `INSERT INTO stock_movement (
                    id, team_id, part_id, qty_delta_centi, reason, note, occurred_at,
                    created_at, created_by
                  )
                  SELECT $movementId, i.team_id, i.part_id, 100, 'returned', $note, $at,
                         $now, $createdBy
                    FROM part_install i
                   WHERE i.team_id = $teamId AND i.id = $installId
                     AND i.removed_at IS NULL AND $destination = 'stock'`,
            params: {
              $movementId: movementId,
              $teamId: teamId,
              $installId: installId,
              $destination: input.destination,
              $note: input.note,
              $at: at,
              $now: now,
              $createdBy: input.createdBy,
            },
          },
          movementOutboxStatement(teamId, movementId),
          {
            sql: `DELETE FROM shopping_item
                   WHERE team_id = $teamId AND dismissed = 1
                     AND part_id = (SELECT part_id FROM part_install
                                     WHERE team_id = $teamId AND id = $installId)
                     AND (SELECT COALESCE(SUM(qty_delta_centi), 0) FROM stock_movement
                           WHERE team_id = $teamId AND part_id = shopping_item.part_id)
                         >= (SELECT min_stock_centi FROM part
                              WHERE team_id = $teamId AND id = shopping_item.part_id)`,
            params: { $teamId: teamId, $installId: installId },
          },
          installClosedOutbox(teamId, installId, {
            at,
            destination: input.destination,
            reason: input.destination === "scrap" ? input.reason : null,
          }),
          {
            sql: `UPDATE part_install
                     SET removed_at = CASE WHEN $at > installed_at THEN $at
                           ELSE strftime('%Y-%m-%dT%H:%M:%fZ', installed_at, '+0.001 seconds') END,
                         removal_destination = $destination,
                         removal_reason = $reason,
                         updated_at = $now
                   WHERE team_id = $teamId AND id = $installId AND removed_at IS NULL
                  RETURNING id`,
            params: {
              $teamId: teamId,
              $installId: installId,
              $at: at,
              $destination: input.destination,
              $reason: input.destination === "scrap" ? input.reason : null,
              $now: now,
            },
          },
        ],
        true,
      );
    } catch (error) {
      if (error instanceof LocalConstraintError && /required at least one row/i.test(error.message)) {
        throw new GarageConflictError("install_closed", "install already closed", { cause: error });
      }
      this.rethrowWrite(error);
    }
  }

  async getCountDraft(teamId: string): Promise<CountDraft | null> {
    const [head] = await this.db.query(
      `SELECT c.scope_kind, c.started_at,
              (SELECT count(*) FROM stock_movement m
                WHERE m.team_id = c.team_id AND m.rowid > c.base_rowid
                  AND EXISTS (SELECT 1 FROM inventory_count_line l
                               WHERE l.team_id = m.team_id AND l.part_id = m.part_id)
              ) AS movements_since
         FROM inventory_count c
        WHERE c.team_id = $teamId`,
      { $teamId: teamId },
    );
    if (head === undefined) return null;
    const draft = parse(countDraftRowSchema, head);
    const lines = await this.db.query(
      `SELECT part_id, counted_centi FROM inventory_count_line WHERE team_id = $teamId`,
      { $teamId: teamId },
    );
    return {
      scopeKind: draft.scope_kind,
      startedAt: draft.started_at,
      movementsSince: draft.movements_since,
      lines: Object.fromEntries(
        lines.map((row) => {
          const line = parse(countLineRowSchema, row);
          return [line.part_id, line.counted_centi];
        }),
      ),
    };
  }

  async startCount(teamId: string, scopeKind: PartKind | null): Promise<CountDraft> {
    try {
      await this.writeTx([
        {
          sql: `INSERT INTO inventory_count (team_id, scope_kind, started_at, base_rowid)
                SELECT t.id, $scopeKind, $now, COALESCE((SELECT max(rowid) FROM stock_movement), 0)
                  FROM team t WHERE t.id = $teamId
                ON CONFLICT (team_id) DO NOTHING`,
          params: { $teamId: teamId, $scopeKind: scopeKind, $now: this.now() },
        },
      ]);
    } catch (error) {
      this.rethrowWrite(error);
    }
    const draft = await this.getCountDraft(teamId);
    if (draft === null) throw new GarageNotFoundError("team not found");
    return draft;
  }

  async setCountLine(teamId: string, partId: string, countedCenti: number | null): Promise<void> {
    try {
      if (countedCenti === null) {
        await this.writeTx([
          {
            sql: `DELETE FROM inventory_count_line WHERE team_id = $teamId AND part_id = $partId`,
            params: { $teamId: teamId, $partId: partId },
          },
        ]);
        return;
      }
      await this.writeTx(
        [
          {
            // Needs an open draft and a live part of the same team, otherwise nothing is
            // written and the transaction fails.
            sql: `INSERT INTO inventory_count_line (team_id, part_id, counted_centi)
                  SELECT c.team_id, p.id, $counted
                    FROM inventory_count c
                    JOIN part p ON p.team_id = c.team_id AND p.id = $partId AND p.deleted_at IS NULL
                   WHERE c.team_id = $teamId
                  ON CONFLICT (team_id, part_id) DO UPDATE SET counted_centi = excluded.counted_centi
                  RETURNING part_id`,
            params: { $teamId: teamId, $partId: partId, $counted: countedCenti },
          },
        ],
        true,
      );
    } catch (error) {
      if (error instanceof LocalConstraintError && /required at least one row/i.test(error.message)) {
        throw new GarageConflictError("not_serviceable", "no open count for this part", {
          cause: error,
        });
      }
      this.rethrowWrite(error);
    }
  }

  async discardCount(teamId: string): Promise<void> {
    await this.writeTx([
      { sql: "DELETE FROM inventory_count WHERE team_id = $teamId", params: { $teamId: teamId } },
    ]);
  }

  async applyCount(
    teamId: string,
    apply: { occurredOn: string; note: string; createdBy: string | null },
  ): Promise<number> {
    const draft = await this.getCountDraft(teamId);
    if (draft === null) throw new GarageConflictError("not_serviceable", "no open count");
    const now = this.now();
    const at = occurredAt(apply.occurredOn, now);
    const statements: SqlStatement[] = [];
    for (const partId of Object.keys(draft.lines)) {
      const movementId = newId();
      statements.push(
        {
          // The difference is computed here, inside the transaction, against the stock at
          // this moment, so movements made while counting are respected.
          sql: `INSERT INTO stock_movement (
                  id, team_id, part_id, qty_delta_centi, reason, note, occurred_at,
                  created_at, created_by
                )
                SELECT $id, l.team_id, l.part_id,
                       l.counted_centi - COALESCE((SELECT SUM(qty_delta_centi) FROM stock_movement
                                                    WHERE team_id = l.team_id AND part_id = l.part_id), 0),
                       'adjustment', $note, $at, $now, $createdBy
                  FROM inventory_count_line l
                  JOIN part p ON p.team_id = l.team_id AND p.id = l.part_id AND p.deleted_at IS NULL
                 WHERE l.team_id = $teamId AND l.part_id = $partId
                   AND l.counted_centi <> COALESCE((SELECT SUM(qty_delta_centi) FROM stock_movement
                                                     WHERE team_id = l.team_id AND part_id = l.part_id), 0)`,
          params: {
            $id: movementId,
            $teamId: teamId,
            $partId: partId,
            $note: apply.note,
            $at: at,
            $now: now,
            $createdBy: apply.createdBy,
          },
        },
        // Nothing is queued for a line whose count equals the stock (no movement inserted).
        movementOutboxStatement(teamId, movementId),
        afterStockChange(teamId, partId, false),
      );
    }
    statements.push({
      sql: `DELETE FROM inventory_count WHERE team_id = $teamId
            RETURNING (SELECT count(*) FROM stock_movement
                        WHERE team_id = $teamId AND reason = 'adjustment'
                          AND created_at = $now AND note = $note) AS applied`,
      params: { $teamId: teamId, $now: now, $note: apply.note },
    });
    try {
      const rows = await this.writeTx<{ applied: number }>(statements, true);
      return rows[0]?.applied ?? 0;
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  async listShoppingList(teamId: string, scope: ReadScope = NO_COSTS): Promise<ShoppingLine[]> {
    const rows = await this.db.query(this.shoppingListSql(scope), { $teamId: teamId });
    return rows.map((row) => this.shoppingFrom(row));
  }

  /** The stored line of a part (not the computed list), for the change the queue must describe. */
  private async shoppingItemState(teamId: string, partId: string) {
    const rows = await this.db.query(
      `SELECT id, version, local_rev, qty_centi, dismissed FROM shopping_item
        WHERE team_id = $teamId AND part_id = $partId`,
      { $teamId: teamId, $partId: partId },
    );
    const row = rows[0];
    return row === undefined ? null : parse(shoppingItemStateRowSchema, row);
  }

  async setShoppingItem(teamId: string, partId: string, qtyCenti: number | null): Promise<void> {
    const now = this.now();
    const existing = await this.shoppingItemState(teamId, partId);
    const id = existing?.id ?? newId();
    // The effective quantity (stored or suggested) must be positive, like the server.
    const allowed = `SELECT 1 FROM part p
                      WHERE p.team_id = $teamId AND p.id = $partId AND p.deleted_at IS NULL
                        AND COALESCE($qty, MAX(0, COALESCE(p.reorder_to_centi, p.min_stock_centi)
                              - COALESCE((SELECT SUM(qty_delta_centi) FROM stock_movement
                                           WHERE team_id = p.team_id AND part_id = p.id), 0))) > 0`;
    const guardParams = { $teamId: teamId, $partId: partId, $qty: qtyCenti };
    const change = existing
      ? changedFields(
          { qty_centi: existing.qty_centi, dismissed: existing.dismissed === 1 },
          { qty_centi: qtyCenti, dismissed: false },
        )
      : {};
    const statements: SqlStatement[] = [];
    if (existing === null) {
      statements.push(
        outboxStatement(
          {
            teamId,
            entity: "garage.shopping_item",
            entityId: id,
            kind: OpKind.create,
            baseVersion: null,
            fields: { part_id: partId, qty_centi: qtyCenti, dismissed: false },
          },
          {
            sql: `${allowed} AND NOT EXISTS (SELECT 1 FROM shopping_item
                                              WHERE team_id = $teamId AND part_id = $partId)`,
            params: guardParams,
          },
        ),
      );
    } else if (Object.keys(change).length > 0) {
      statements.push(
        outboxStatement(
          {
            teamId,
            entity: "garage.shopping_item",
            entityId: existing.id,
            kind: OpKind.patch,
            baseVersion: existing.version,
            fields: change,
          },
          {
            sql: `${allowed} AND EXISTS (SELECT 1 FROM shopping_item
                                          WHERE team_id = $teamId AND part_id = $partId
                                            AND local_rev = $rev)`,
            params: { ...guardParams, $rev: existing.local_rev },
          },
        ),
      );
    }
    try {
      await this.writeTx(
        [
          ...statements,
          {
            sql: `INSERT INTO shopping_item (
                    id, team_id, part_id, qty_centi, dismissed, created_at, updated_at
                  )
                  SELECT $id, p.team_id, p.id, $qty, 0, $now, $now
                    FROM part p
                   WHERE p.team_id = $teamId AND p.id = $partId AND p.deleted_at IS NULL
                     AND COALESCE($qty, MAX(0, COALESCE(p.reorder_to_centi, p.min_stock_centi)
                           - COALESCE((SELECT SUM(qty_delta_centi) FROM stock_movement
                                        WHERE team_id = p.team_id AND part_id = p.id), 0))) > 0
                  ON CONFLICT (team_id, part_id) DO UPDATE
                     SET qty_centi = excluded.qty_centi, dismissed = 0,
                         updated_at = excluded.updated_at, local_rev = local_rev + 1
                  RETURNING id`,
            params: { $id: id, $teamId: teamId, $partId: partId, $qty: qtyCenti, $now: now },
          },
        ],
        true,
      );
    } catch (error) {
      if (error instanceof LocalConstraintError && /required at least one row/i.test(error.message)) {
        throw new GarageConflictError("not_serviceable", "shopping line not allowed", { cause: error });
      }
      this.rethrowWrite(error);
    }
  }

  async removeShoppingItem(teamId: string, partId: string): Promise<void> {
    const onList = (await this.listShoppingList(teamId)).some((line) => line.partId === partId);
    if (!onList) throw new GarageNotFoundError("part is not on the shopping list");
    const now = this.now();
    const existing = await this.shoppingItemState(teamId, partId);
    const id = existing?.id ?? newId();
    const stock = `COALESCE((SELECT SUM(qty_delta_centi) FROM stock_movement
                              WHERE team_id = $teamId AND part_id = $partId), 0)`;
    const below = `SELECT 1 FROM part WHERE team_id = $teamId AND id = $partId
                      AND deleted_at IS NULL AND ${stock} < min_stock_centi`;
    const notBelow = `SELECT 1 FROM part WHERE team_id = $teamId AND id = $partId
                         AND ${stock} >= min_stock_centi`;
    const params = { $teamId: teamId, $partId: partId };
    const statements: SqlStatement[] = [];
    // Below the minimum the line is automatic: hide it. Otherwise it was manual: remove it.
    if (existing === null) {
      statements.push(
        outboxStatement(
          {
            teamId,
            entity: "garage.shopping_item",
            entityId: id,
            kind: OpKind.create,
            baseVersion: null,
            fields: { part_id: partId, dismissed: true },
          },
          {
            sql: `${below} AND NOT EXISTS (SELECT 1 FROM shopping_item
                                            WHERE team_id = $teamId AND part_id = $partId)`,
            params,
          },
        ),
      );
    } else {
      const guardRev = `EXISTS (SELECT 1 FROM shopping_item
                                 WHERE team_id = $teamId AND part_id = $partId AND local_rev = $rev)`;
      const hide = changedFields(
        { qty_centi: existing.qty_centi, dismissed: existing.dismissed === 1 },
        { qty_centi: null, dismissed: true },
      );
      if (Object.keys(hide).length > 0) {
        statements.push(
          outboxStatement(
            {
              teamId,
              entity: "garage.shopping_item",
              entityId: existing.id,
              kind: OpKind.patch,
              baseVersion: existing.version,
              fields: hide,
            },
            { sql: `${below} AND ${guardRev}`, params: { ...params, $rev: existing.local_rev } },
          ),
        );
      }
      statements.push(
        outboxStatement(
          {
            teamId,
            entity: "garage.shopping_item",
            entityId: existing.id,
            kind: OpKind.delete,
            baseVersion: existing.version,
            fields: {},
          },
          { sql: `${notBelow} AND ${guardRev}`, params: { ...params, $rev: existing.local_rev } },
        ),
      );
    }
    try {
      await this.writeTx([
        ...statements,
        {
          // Below the minimum the line is automatic: hide it until the stock recovers.
          sql: `INSERT INTO shopping_item (
                  id, team_id, part_id, qty_centi, dismissed, created_at, updated_at
                )
                SELECT $id, p.team_id, p.id, NULL, 1, $now, $now
                  FROM part p
                 WHERE p.team_id = $teamId AND p.id = $partId AND p.deleted_at IS NULL
                   AND ${stock} < p.min_stock_centi
                ON CONFLICT (team_id, part_id) DO UPDATE
                   SET qty_centi = NULL, dismissed = 1, updated_at = excluded.updated_at,
                       local_rev = local_rev + 1`,
          params: { $id: id, $teamId: teamId, $partId: partId, $now: now },
        },
        {
          sql: `DELETE FROM shopping_item
                 WHERE team_id = $teamId AND part_id = $partId
                   AND ${stock} >= (SELECT min_stock_centi FROM part
                                     WHERE team_id = $teamId AND id = $partId)`,
          params: { $teamId: teamId, $partId: partId },
        },
      ]);
    } catch (error) {
      this.rethrowWrite(error);
    }
  }

  private shoppingListSql(scope: ReadScope): string {
    return `
      SELECT p.id AS part_id, p.kind, p.description, p.brand, p.reference, p.unit,
             COALESCE(s.qty, 0) AS stock_centi, p.min_stock_centi, p.reorder_to_centi,
             CASE WHEN COALESCE(s.qty, 0) < p.min_stock_centi THEN 1 ELSE 0 END AS below_min,
             si.qty_centi AS item_qty_centi, si.note AS item_note${
               scope.costs ? ", p.unit_cost_cents" : ""
             }
        FROM part p
        LEFT JOIN (SELECT team_id, part_id, SUM(qty_delta_centi) AS qty
                     FROM stock_movement GROUP BY team_id, part_id) s
               ON s.team_id = p.team_id AND s.part_id = p.id
        LEFT JOIN shopping_item si ON si.team_id = p.team_id AND si.part_id = p.id
       WHERE p.team_id = $teamId AND p.deleted_at IS NULL
         AND COALESCE(si.dismissed, 0) = 0
         AND (COALESCE(s.qty, 0) < p.min_stock_centi OR si.id IS NOT NULL)
       ORDER BY COALESCE(p.brand, '') COLLATE NOCASE, p.description COLLATE NOCASE, p.id`;
  }

  private shoppingFrom(row: unknown): ShoppingLine {
    const r = parse(shoppingLineRowSchema, row);
    const suggested = Math.max(0, (r.reorder_to_centi ?? r.min_stock_centi) - r.stock_centi);
    const line: ShoppingLine = {
      partId: r.part_id,
      kind: r.kind,
      description: r.description,
      brand: r.brand,
      reference: r.reference,
      unit: r.unit,
      stockCenti: r.stock_centi,
      qtyCenti: r.item_qty_centi ?? suggested,
      suggestedQtyCenti: suggested,
      belowMin: r.below_min === 1,
      note: r.item_note,
    };
    if (r.unit_cost_cents !== undefined) line.unitCostCents = r.unit_cost_cents;
    return line;
  }

  private movementWithStock(teamId: string, partId: string, id: string): SqlStatement {
    return {
      sql: `SELECT ${MOVEMENT_COLUMNS},
                   (SELECT COALESCE(SUM(s.qty_delta_centi), 0) FROM stock_movement s
                     WHERE s.team_id = m.team_id AND s.part_id = m.part_id) AS part_qty_centi,
                   (SELECT min_stock_centi FROM part
                     WHERE team_id = m.team_id AND id = m.part_id) AS part_min_stock_centi
              FROM stock_movement m
             WHERE m.team_id = $teamId AND m.part_id = $partId AND m.id = $id`,
      params: { $teamId: teamId, $partId: partId, $id: id },
    };
  }

  /**
   * The row as the server model sees it (snake_case columns plus version), for diffing an edit.
   * `table` is always one of this file's own constants, never user input.
   */
  private async syncRow(
    table: "engine" | "tyre_set" | "part",
    teamId: string,
    id: string,
    columns: string,
  ): Promise<Record<string, unknown> & { version: number }> {
    const rows = await this.db.query<Record<string, unknown> & { version: number }>(
      `SELECT ${columns}, version FROM ${table}
        WHERE team_id = $teamId AND id = $id AND deleted_at IS NULL`,
      { $teamId: teamId, $id: id },
    );
    const row = rows[0];
    if (!row) throw new GarageNotFoundError(`${table} not found`);
    return row;
  }

  /** Queues an edit of `entity` with only what changed; nothing changed, nothing queued. */
  private patchQueue(
    entity: string,
    table: "engine" | "tyre_set" | "part",
    teamId: string,
    id: string,
    localRev: number,
    current: Record<string, unknown> & { version: number },
    next: Record<string, unknown>,
  ): SqlStatement[] {
    const fields = changedFields(current, next);
    if (Object.keys(fields).length === 0) return [];
    return [
      outboxStatement(
        { teamId, entity, entityId: id, kind: OpKind.patch, baseVersion: current.version, fields },
        {
          // The state the UPDATE after it is about to match.
          sql: `SELECT 1 FROM ${table}
                 WHERE team_id = $teamId AND id = $id AND local_rev = $applied AND deleted_at IS NULL`,
          params: { $teamId: teamId, $id: id, $applied: localRev },
        },
      ),
    ];
  }

  /**
   * The operation for a car edit carries only the fields that changed, so two devices that
   * touch different fields merge without a conflict (ADR 0025). Nothing changed, no operation.
   */
  private carPatchStatements(
    teamId: string,
    id: string,
    localRev: number,
    current: Car,
    input: CarInput,
  ): SqlStatement[] {
    const fields: Record<string, unknown> = {};
    if (current.make !== input.make) fields.make = input.make;
    if (current.model !== input.model) fields.model = input.model;
    if (current.modelYear !== input.modelYear) fields.model_year = input.modelYear;
    if (current.serialNumber !== input.serialNumber) fields.serial_number = input.serialNumber;
    if (current.nickname !== input.nickname) fields.nickname = input.nickname;
    if (current.transponderNumber !== input.transponderNumber) {
      fields.transponder_number = input.transponderNumber;
    }
    if (current.status !== input.status) fields.status = input.status;
    if (current.notes !== input.notes) fields.notes = input.notes;
    if (Object.keys(fields).length === 0) return [];
    return [
      outboxStatement(
        { teamId, entity: "garage.car", entityId: id, kind: OpKind.patch, baseVersion: current.version, fields },
        {
          sql: "SELECT 1 FROM car WHERE team_id = $teamId AND id = $id AND local_rev = $applied AND deleted_at IS NULL",
          params: { $teamId: teamId, $id: id, $applied: localRev },
        },
      ),
    ];
  }

  private writeTx<T = Record<string, unknown>>(
    statements: SqlStatement[],
    requireRows = false,
  ): Promise<T[]> {
    return this.db.transaction<T>(statements, { requireRows }).then((rows) => {
      // Wakes the sync engine; a no-op when nothing is queued.
      notifyLocalWrite();
      return rows;
    });
  }

  private rethrowWrite(error: unknown): never {
    if (error instanceof LocalConstraintError) {
      const message = error.message.toLowerCase();
      if (message.includes("transponder")) {
        throw new GarageConflictError("transponder", "duplicate transponder", { cause: error });
      }
      if (message.includes("tyre_set") && message.includes("label")) {
        throw new GarageConflictError("tyre_label", "duplicate tyre label", { cause: error });
      }
      throw new GarageConflictError("constraint", "local garage constraint", { cause: error });
    }
    if (error instanceof GarageConflictError || error instanceof GarageNotFoundError) {
      throw error;
    }
    throw error;
  }

  private rethrowStale(error: unknown, entity: string): never {
    if (
      error instanceof LocalConstraintError &&
      /required at least one row/i.test(error.message)
    ) {
      throw new GarageConflictError("stale", `${entity} changed in another tab`, {
        cause: error,
      });
    }
    this.rethrowWrite(error);
  }

  private async hasOpenCarMount(teamId: string, carId: string): Promise<boolean> {
    const rows = await this.db.query<{ id: string }>(
      `SELECT id FROM engine_mount
        WHERE team_id = $teamId AND car_id = $carId AND unmounted_at IS NULL
        LIMIT 1`,
      { $teamId: teamId, $carId: carId },
    );
    return rows.length > 0;
  }

  private async hasOpenEngineMount(teamId: string, engineId: string): Promise<boolean> {
    const rows = await this.db.query<{ id: string }>(
      `SELECT id FROM engine_mount
        WHERE team_id = $teamId AND engine_id = $engineId AND unmounted_at IS NULL
        LIMIT 1`,
      { $teamId: teamId, $engineId: engineId },
    );
    return rows.length > 0;
  }

  private async mountWrite(statements: SqlStatement[]): Promise<void> {
    try {
      await this.writeTx(statements, true);
    } catch (error) {
      if (error instanceof GarageConflictError) throw error;
      if (error instanceof LocalConstraintError) {
        throw new GarageConflictError("mount", "engine or car already mounted", {
          cause: error,
        });
      }
      throw error;
    }
  }

  private carFrom(row: unknown): Car {
    const value = parse(carRowSchema, row);
    return {
      id: value.id,
      teamId: value.team_id,
      make: value.make,
      model: value.model,
      modelYear: value.model_year,
      serialNumber: value.serial_number,
      nickname: value.nickname,
      transponderNumber: value.transponder_number,
      status: value.status,
      scale: value.scale,
      powertrain: value.powertrain,
      notes: value.notes,
      createdAt: value.created_at,
      updatedAt: value.updated_at,
      version: value.version,
      localRev: value.local_rev,
    };
  }

  private engineFrom(row: unknown): Engine {
    const value = parse(engineRowSchema, row);
    return {
      id: value.id,
      teamId: value.team_id,
      make: value.make,
      model: value.model,
      serialNumber: value.serial_number,
      purchasedOn: value.purchased_on,
      status: value.status,
      tanksTotal: value.tanks_total,
      runSecondsTotal: value.run_seconds_total,
      sessionsTotal: value.sessions_total,
      notes: value.notes,
      createdAt: value.created_at,
      updatedAt: value.updated_at,
      version: value.version,
      localRev: value.local_rev,
    };
  }

  private engineWithMountFrom(row: unknown): EngineWithMount {
    const value = parse(engineWithMountRowSchema, row);
    const {
      current_car_id,
      current_mounted_at,
      current_car_make,
      current_car_model,
      current_car_nickname,
      current_car_status,
      ...engineRow
    } = value;
    let currentCar: CarSummary | null = null;
    if (current_car_id !== null) {
      if (
        current_car_make === null ||
        current_car_model === null ||
        current_car_status === null
      ) {
        throw new CorruptLocalDataError("an engine mount row in the local database is invalid");
      }
      currentCar = {
        id: current_car_id,
        make: current_car_make,
        model: current_car_model,
        nickname: current_car_nickname,
        status: current_car_status,
      };
    }
    return {
      ...this.engineFrom(engineRow),
      currentCar,
      mountedAt: current_mounted_at,
    };
  }

  private mountFrom(row: unknown): EngineMount {
    const value = parse(engineMountRowSchema, row);
    return {
      id: value.id,
      engineId: value.engine_id,
      carId: value.car_id,
      car: {
        id: value.car_id,
        make: value.car_make,
        model: value.car_model,
        nickname: value.car_nickname,
        status: value.car_status,
      },
      mountedAt: value.mounted_at,
      unmountedAt: value.unmounted_at,
    };
  }

  private tyreFrom(row: unknown): TyreSet {
    const value = parse(tyreSetRowSchema, row);
    return {
      id: value.id,
      teamId: value.team_id,
      label: value.label,
      make: value.make,
      model: value.model,
      compound: value.compound,
      insertType: value.insert_type,
      gluedOn: value.glued_on,
      status: value.status,
      sessionsTotal: value.sessions_total,
      runSecondsTotal: value.run_seconds_total,
      wearNotes: value.wear_notes,
      createdAt: value.created_at,
      updatedAt: value.updated_at,
      version: value.version,
      localRev: value.local_rev,
    };
  }

  private installFrom(row: unknown): PartInstall {
    const r = parse(partInstallRowSchema, row);
    return {
      id: r.id,
      partId: r.part_id,
      partKind: r.part_kind,
      partDescription: r.part_description,
      partBrand: r.part_brand,
      partReference: r.part_reference,
      carId: r.car_id,
      engineId: r.engine_id,
      targetName: r.target_name,
      installedAt: r.installed_at,
      removedAt: r.removed_at,
      removalDestination: r.removal_destination,
      removalReason: r.removal_reason,
      runSeconds: r.run_seconds,
      sessions: r.sessions,
      note: r.note,
    };
  }

  private movementFrom(row: unknown): StockMovement {
    const value = parse(stockMovementRowSchema, row);
    return {
      id: value.id,
      partId: value.part_id,
      qtyDeltaCenti: value.qty_delta_centi,
      reason: value.reason,
      lossReason: value.loss_reason,
      note: value.note,
      store: value.store,
      ...(value.unit_cost_cents !== undefined ? { unitCostCents: value.unit_cost_cents } : {}),
      occurredAt: value.occurred_at,
      createdAt: value.created_at,
      createdBy: value.created_by,
      revertsMovementId: value.reverts_movement_id,
      undone: value.undone === 1,
    };
  }

  private changeFrom(row: unknown): StockChange {
    const value = parse(stockMovementRowSchema, row);
    if (value.part_qty_centi === undefined || value.part_min_stock_centi === undefined) {
      throw new CorruptLocalDataError("a stock movement row in the local database is invalid");
    }
    return {
      movementId: value.id,
      beforeCenti: value.part_qty_centi - value.qty_delta_centi,
      afterCenti: value.part_qty_centi,
      minStockCenti: value.part_min_stock_centi,
    };
  }

  private partFrom(row: unknown): PartWithStock {
    const value = parse(partRowSchema, row);
    return {
      id: value.id,
      teamId: value.team_id,
      kind: value.kind,
      brand: value.brand,
      reference: value.reference,
      description: value.description,
      unit: value.unit,
      specs: value.specs,
      minStockCenti: value.min_stock_centi,
      reorderToCenti: value.reorder_to_centi,
      ...(value.unit_cost_cents !== undefined ? { unitCostCents: value.unit_cost_cents } : {}),
      hasServiceLife: value.has_service_life === 1,
      createdAt: value.created_at,
      updatedAt: value.updated_at,
      version: value.version,
      localRev: value.local_rev,
      qtyCenti: value.qty_centi,
      belowMin: value.below_min === 1,
    };
  }
}
