import { newId } from "@/shared/lib/id";
import { LocalConstraintError, CorruptLocalDataError } from "@/db/errors";

import type {
  Car,
  CarInput,
  CarSummary,
  Engine,
  EngineInput,
  EngineMount,
  EngineWithMount,
  PartCreateInput,
  PartInput,
  PartWithStock,
  TyreSet,
  TyreSetInput,
} from "../domain/models";
import { GarageConflictError, GarageNotFoundError } from "../domain/errors";
import {
  carRowSchema,
  engineMountRowSchema,
  engineRowSchema,
  engineWithMountRowSchema,
  partRowSchema,
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

const PART_SELECT = `
  SELECT
    p.id, p.team_id, p.kind, p.brand, p.reference, p.description, p.unit,
    p.min_stock_centi, p.has_service_life, p.created_at, p.updated_at, p.version,
    p.local_rev,
    COALESCE(SUM(m.qty_delta_centi), 0) AS qty_centi,
    CASE WHEN COALESCE(SUM(m.qty_delta_centi), 0) < p.min_stock_centi THEN 1 ELSE 0 END AS below_min
  FROM part p
  LEFT JOIN stock_movement m
    ON m.team_id = p.team_id AND m.part_id = p.id
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
    try {
      const rows = await this.writeTx(
        [
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
    try {
      await this.writeTx(
        [
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

  async mountEngine(teamId: string, engineId: string, carId: string): Promise<void> {
    await this.mountWrite([
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
                 )
              RETURNING id`,
        params: {
          $id: newId(),
          $teamId: teamId,
          $engineId: engineId,
          $carId: carId,
          $mountedAt: this.now(),
          $createdAt: this.now(),
        },
      },
    ]);
  }

  async unmountEngine(teamId: string, engineId: string): Promise<void> {
    await this.mountWrite([
      {
        sql: `UPDATE engine_mount
                 SET unmounted_at = $unmountedAt
               WHERE team_id = $teamId AND engine_id = $engineId AND unmounted_at IS NULL
              RETURNING id`,
        params: {
          $teamId: teamId,
          $engineId: engineId,
          $unmountedAt: this.now(),
        },
      },
    ]);
  }

  async moveEngine(teamId: string, engineId: string, toCarId: string): Promise<void> {
    const current = await this.db.query<{ car_id: string }>(
      `SELECT car_id FROM engine_mount
        WHERE team_id = $teamId AND engine_id = $engineId AND unmounted_at IS NULL`,
      { $teamId: teamId, $engineId: engineId },
    );
    if (current[0]?.car_id === toCarId) return;

    await this.mountWrite([
      {
        sql: `UPDATE engine_mount
                 SET unmounted_at = $unmountedAt
               WHERE team_id = $teamId AND engine_id = $engineId AND unmounted_at IS NULL`,
        params: { $teamId: teamId, $engineId: engineId, $unmountedAt: this.now() },
      },
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
                 )
              RETURNING id`,
        params: {
          $id: newId(),
          $teamId: teamId,
          $engineId: engineId,
          $carId: toCarId,
          $mountedAt: this.now(),
          $createdAt: this.now(),
        },
      },
    ]);
  }

  async swapCarEngine(teamId: string, carId: string, engineId: string): Promise<void> {
    const current = await this.db.query<{ engine_id: string }>(
      `SELECT engine_id FROM engine_mount
        WHERE team_id = $teamId AND car_id = $carId AND unmounted_at IS NULL`,
      { $teamId: teamId, $carId: carId },
    );
    if (current[0]?.engine_id === engineId) return;

    await this.mountWrite([
      {
        sql: `UPDATE engine_mount
                 SET unmounted_at = $unmountedAt
               WHERE team_id = $teamId AND car_id = $carId AND unmounted_at IS NULL`,
        params: { $teamId: teamId, $carId: carId, $unmountedAt: this.now() },
      },
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
                 )
              RETURNING id`,
        params: {
          $id: newId(),
          $teamId: teamId,
          $carId: carId,
          $engineId: engineId,
          $mountedAt: this.now(),
          $createdAt: this.now(),
        },
      },
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
    try {
      const rows = await this.writeTx(
        [
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

  async listParts(teamId: string): Promise<PartWithStock[]> {
    const rows = await this.db.query(
      `${PART_SELECT}
       WHERE p.team_id = $teamId AND p.deleted_at IS NULL
       GROUP BY p.id
       ORDER BY below_min DESC, p.description COLLATE NOCASE, p.id`,
      { $teamId: teamId },
    );
    return rows.map((row) => this.partFrom(row));
  }

  async getPart(teamId: string, id: string): Promise<PartWithStock | undefined> {
    const rows = await this.db.query(
      `${PART_SELECT}
       WHERE p.team_id = $teamId AND p.id = $id AND p.deleted_at IS NULL
       GROUP BY p.id`,
      { $teamId: teamId, $id: id },
    );
    return rows[0] ? this.partFrom(rows[0]) : undefined;
  }

  async createPart(teamId: string, input: PartCreateInput): Promise<PartWithStock> {
    const id = newId();
    const now = this.now();
    const statements: SqlStatement[] = [
      {
        sql: `INSERT INTO part (
                id, team_id, kind, brand, reference, description, unit, specs,
                min_stock_centi, unit_cost_cents, currency, has_service_life,
                created_at, updated_at, version, local_rev
              ) VALUES (
                $id, $teamId, $kind, $brand, $reference, $description, $unit, '{}',
                $minStockCenti, NULL, 'EUR', $hasServiceLife,
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
          $minStockCenti: input.minStockCenti,
          $hasServiceLife: input.hasServiceLife ? 1 : 0,
          $createdAt: now,
          $updatedAt: now,
        },
      },
      ...(input.initialQtyCenti > 0
        ? [
            {
              sql: `INSERT INTO stock_movement (
                      id, team_id, part_id, qty_delta_centi, reason, note, occurred_at,
                      created_at
                    ) VALUES ($movementId, $teamId, $partId, $qty, 'purchase', NULL, $now, $now)`,
              params: {
                $movementId: newId(),
                $teamId: teamId,
                $partId: id,
                $qty: input.initialQtyCenti,
                $now: now,
              },
            },
          ]
        : []),
      {
        sql: `${PART_SELECT}
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
    try {
      await this.writeTx(
        [
          {
            sql: `UPDATE part
                     SET kind = $kind, brand = $brand, reference = $reference,
                         description = $description, unit = $unit,
                         min_stock_centi = $minStockCenti,
                         has_service_life = $hasServiceLife, updated_at = $updatedAt,
                         local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL
                  RETURNING id`,
            params: {
              $teamId: teamId,
              $id: id,
              $localRev: localRev,
              $kind: input.kind,
              $brand: input.brand,
              $reference: input.reference,
              $description: input.description,
              $unit: input.unit,
              $minStockCenti: input.minStockCenti,
              $hasServiceLife: input.hasServiceLife ? 1 : 0,
              $updatedAt: this.now(),
            },
          },
        ],
        true,
      );
      const part = await this.getPart(teamId, id);
      if (!part) throw new GarageNotFoundError("part not found after update");
      return part;
    } catch (error) {
      this.rethrowStale(error, "part");
    }
  }

  private writeTx<T = Record<string, unknown>>(
    statements: SqlStatement[],
    requireRows = false,
  ): Promise<T[]> {
    return this.db.transaction<T>(statements, { requireRows });
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
      minStockCenti: value.min_stock_centi,
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
