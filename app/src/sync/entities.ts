import { z } from "zod";

import type { SqlStatement } from "@/db/protocol";

// What a Pull change does to the local database, per entity. Every payload is validated
// with Zod first: the server is trusted for authorization, not for the shape of its data.
// Entities the app does not know are ignored (the cursor still moves past them).

export type ChangeStatements = (
  teamId: string,
  entityId: string,
  data: unknown,
  now: string,
) => SqlStatement[];

const timestamp = z
  .string()
  .max(40)
  .transform((value, ctx) => {
    const parsed = Date.parse(value);
    if (!Number.isFinite(parsed)) {
      ctx.addIssue({ code: "custom", message: "invalid timestamp" });
      return z.NEVER;
    }
    return new Date(parsed).toISOString();
  });

const nullableString = z.string().nullable();

const carWire = z.object({
  make: z.string(),
  model: z.string(),
  model_year: z.number().int().nullable(),
  serial_number: nullableString,
  nickname: nullableString,
  transponder_number: nullableString,
  status: z.enum(["active", "spare", "retired"]),
  scale: z.string(),
  powertrain: z.string(),
  notes: nullableString,
  created_at: timestamp,
  version: z.number().int().min(1),
});

const engineWire = z.object({
  make: z.string(),
  model: z.string(),
  serial_number: nullableString,
  purchased_on: nullableString,
  status: z.enum(["active", "spare", "retired"]),
  notes: nullableString,
  created_at: timestamp,
  version: z.number().int().min(1),
});

const tyreWire = z.object({
  label: z.string(),
  make: z.string(),
  model: z.string(),
  compound: nullableString,
  insert_type: nullableString,
  glued_on: nullableString,
  status: z.enum(["new", "used", "worn", "retired"]),
  wear_notes: nullableString,
  created_at: timestamp,
  version: z.number().int().min(1),
});

const partWire = z.object({
  kind: z.string(),
  brand: nullableString,
  reference: nullableString,
  description: z.string(),
  unit: z.enum(["un", "ml", "l", "g"]),
  specs: z.record(z.string(), z.unknown()),
  min_stock_centi: z.number().int().min(0),
  // Absent for a reader who may not see costs: the local value is then left alone.
  unit_cost_cents: z.number().int().min(0).nullable().optional(),
  currency: z.string().length(3),
  has_service_life: z.boolean(),
  reorder_to_centi: z.number().int().min(0).nullable(),
  created_at: timestamp,
  version: z.number().int().min(1),
});

const setupWire = z.object({
  car_id: z.uuid(),
  name: z.string().min(1).max(120),
  current_version: z.number().int().min(1),
  notes: nullableString,
  created_at: timestamp,
  updated_at: timestamp,
  version: z.number().int().min(1),
});

const versionsWire = z.object({
  setup_id: z.uuid(),
  versions: z
    .array(
      z.object({
        version: z.number().int().min(1),
        schema_version: z.number().int().min(1),
        based_on_setup_id: z.uuid().nullable(),
        based_on_version: z.number().int().min(1).nullable(),
        change_summary: nullableString,
        created_at: timestamp,
        data: z.record(z.string(), z.unknown()),
      }),
    )
    .max(50),
});

function carUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const car = carWire.parse(data);
  return [
    {
      // A pulled row bumps local_rev so an edit form opened on the old content goes stale
      // instead of silently writing over it. The team guard stops an id from another team.
      sql: `INSERT INTO car (
              id, team_id, make, model, model_year, serial_number, nickname, transponder_number,
              status, scale, powertrain, notes, created_at, updated_at, version, local_rev
            ) VALUES (
              $id, $teamId, $make, $model, $modelYear, $serial, $nickname, $transponder,
              $status, $scale, $powertrain, $notes, $createdAt, $now, $version, 0
            )
            ON CONFLICT(id) DO UPDATE SET
              make = excluded.make, model = excluded.model, model_year = excluded.model_year,
              serial_number = excluded.serial_number, nickname = excluded.nickname,
              transponder_number = excluded.transponder_number, status = excluded.status,
              scale = excluded.scale, powertrain = excluded.powertrain, notes = excluded.notes,
              version = excluded.version, updated_at = excluded.updated_at,
              deleted_at = NULL, local_rev = car.local_rev + 1
            WHERE car.team_id = excluded.team_id`,
      params: {
        $id: id,
        $teamId: teamId,
        $make: car.make,
        $model: car.model,
        $modelYear: car.model_year,
        $serial: car.serial_number,
        $nickname: car.nickname,
        $transponder: car.transponder_number,
        $status: car.status,
        $scale: car.scale,
        $powertrain: car.powertrain,
        $notes: car.notes,
        $createdAt: car.created_at,
        $now: now,
        $version: car.version,
      },
    },
  ];
}

type SoftTable = "car" | "setup" | "engine" | "tyre_set" | "part";
type VersionedTable = SoftTable | "shopping_item" | "part_install";

function softDelete(table: SoftTable): ChangeStatements {
  return (teamId, id, _data, now) => {
    const statements: SqlStatement[] = [
      {
        sql: `UPDATE ${table} SET deleted_at = $now, updated_at = $now, local_rev = local_rev + 1
               WHERE team_id = $teamId AND id = $id AND deleted_at IS NULL`,
        params: { $teamId: teamId, $id: id, $now: now },
      },
    ];
    // An archived part leaves the shopping list (a soft delete never fires the cascade).
    if (table === "part") {
      statements.push({
        sql: "DELETE FROM shopping_item WHERE team_id = $teamId AND part_id = $id",
        params: { $teamId: teamId, $id: id },
      });
    }
    return statements;
  };
}

function adoptVersion(table: VersionedTable) {
  return (teamId: string, id: string, version: number): SqlStatement => ({
    sql: `UPDATE ${table} SET version = $version WHERE team_id = $teamId AND id = $id AND version < $version`,
    params: { $teamId: teamId, $id: id, $version: version },
  });
}

function engineUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const e = engineWire.parse(data);
  return [
    {
      sql: `INSERT INTO engine (
              id, team_id, make, model, serial_number, purchased_on, status, tanks_total,
              run_seconds_total, sessions_total, notes, created_at, updated_at, version, local_rev
            ) VALUES (
              $id, $teamId, $make, $model, $serial, $purchasedOn, $status, 0, 0, 0, $notes,
              $createdAt, $now, $version, 0
            )
            ON CONFLICT(id) DO UPDATE SET
              make = excluded.make, model = excluded.model, serial_number = excluded.serial_number,
              purchased_on = excluded.purchased_on, status = excluded.status, notes = excluded.notes,
              version = excluded.version, updated_at = excluded.updated_at,
              deleted_at = NULL, local_rev = engine.local_rev + 1
            WHERE engine.team_id = excluded.team_id`,
      params: {
        $id: id, $teamId: teamId, $make: e.make, $model: e.model, $serial: e.serial_number,
        $purchasedOn: e.purchased_on, $status: e.status, $notes: e.notes,
        $createdAt: e.created_at, $now: now, $version: e.version,
      },
    },
  ];
}

function tyreUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const t = tyreWire.parse(data);
  return [
    {
      sql: `INSERT INTO tyre_set (
              id, team_id, label, make, model, compound, insert_type, glued_on, status,
              sessions_total, run_seconds_total, wear_notes, created_at, updated_at, version, local_rev
            ) VALUES (
              $id, $teamId, $label, $make, $model, $compound, $insertType, $gluedOn, $status,
              0, 0, $wearNotes, $createdAt, $now, $version, 0
            )
            ON CONFLICT(id) DO UPDATE SET
              label = excluded.label, make = excluded.make, model = excluded.model,
              compound = excluded.compound, insert_type = excluded.insert_type,
              glued_on = excluded.glued_on, status = excluded.status,
              wear_notes = excluded.wear_notes, version = excluded.version,
              updated_at = excluded.updated_at, deleted_at = NULL,
              local_rev = tyre_set.local_rev + 1
            WHERE tyre_set.team_id = excluded.team_id`,
      params: {
        $id: id, $teamId: teamId, $label: t.label, $make: t.make, $model: t.model,
        $compound: t.compound, $insertType: t.insert_type, $gluedOn: t.glued_on,
        $status: t.status, $wearNotes: t.wear_notes, $createdAt: t.created_at, $now: now,
        $version: t.version,
      },
    },
  ];
}

function partUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const p = partWire.parse(data);
  const costKnown = p.unit_cost_cents !== undefined;
  return [
    {
      sql: `INSERT INTO part (
              id, team_id, kind, brand, reference, description, unit, specs, min_stock_centi,
              reorder_to_centi, unit_cost_cents, currency, has_service_life, created_at, updated_at,
              version, local_rev
            ) VALUES (
              $id, $teamId, $kind, $brand, $reference, $description, $unit, $specs, $min,
              $reorder, $cost, $currency, $service, $createdAt, $now, $version, 0
            )
            ON CONFLICT(id) DO UPDATE SET
              kind = excluded.kind, brand = excluded.brand, reference = excluded.reference,
              description = excluded.description, unit = excluded.unit, specs = excluded.specs,
              min_stock_centi = excluded.min_stock_centi, reorder_to_centi = excluded.reorder_to_centi,
              unit_cost_cents = CASE WHEN $costKnown = 1 THEN excluded.unit_cost_cents
                                     ELSE part.unit_cost_cents END,
              currency = excluded.currency, has_service_life = excluded.has_service_life,
              version = excluded.version, updated_at = excluded.updated_at,
              deleted_at = NULL, local_rev = part.local_rev + 1
            WHERE part.team_id = excluded.team_id`,
      params: {
        $id: id, $teamId: teamId, $kind: p.kind, $brand: p.brand, $reference: p.reference,
        $description: p.description, $unit: p.unit, $specs: JSON.stringify(p.specs),
        $min: p.min_stock_centi, $reorder: p.reorder_to_centi, $cost: p.unit_cost_cents ?? null,
        $costKnown: costKnown ? 1 : 0, $currency: p.currency, $service: p.has_service_life ? 1 : 0,
        $createdAt: p.created_at, $now: now, $version: p.version,
      },
    },
  ];
}

const movementWire = z.object({
  part_id: z.uuid(),
  qty_delta_centi: z.number().int().refine((n) => n !== 0, "a movement moves something"),
  reason: z.enum(["purchase", "consumed", "installed", "adjustment", "lost", "returned"]),
  note: nullableString,
  occurred_at: timestamp,
  // Absent for a reader who may not see costs.
  unit_cost_cents: z.number().int().min(0).nullable().optional(),
  loss_reason: z.enum(["broken", "worn", "lost", "expired"]).nullable(),
});

const shoppingWire = z.object({
  part_id: z.uuid(),
  qty_centi: z.number().int().positive().nullable(),
  dismissed: z.boolean(),
  note: nullableString,
  version: z.number().int().min(1),
});

/**
 * Movements are append-only: one the device already has is left alone (it is usually our own
 * echo). The part must be here; a movement of an unknown part is skipped by the SELECT, not
 * forced in (the foreign key would fail the whole page).
 */
function movementUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const m = movementWire.parse(data);
  return [
    {
      sql: `INSERT OR IGNORE INTO stock_movement (
              id, team_id, part_id, qty_delta_centi, reason, loss_reason, note, unit_cost_cents,
              occurred_at, created_at
            )
            SELECT $id, p.team_id, p.id, $qty, $reason, $lossReason, $note, $cost, $occurredAt, $now
              FROM part p
             WHERE p.team_id = $teamId AND p.id = $partId`,
      params: {
        $id: id,
        $teamId: teamId,
        $partId: m.part_id,
        $qty: m.qty_delta_centi,
        $reason: m.reason,
        $lossReason: m.loss_reason,
        $note: m.note,
        $cost: m.unit_cost_cents ?? null,
        $occurredAt: m.occurred_at,
        $now: now,
      },
    },
  ];
}

function shoppingUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const item = shoppingWire.parse(data);
  const params = {
    $id: id,
    $teamId: teamId,
    $partId: item.part_id,
    $qty: item.qty_centi,
    $dismissed: item.dismissed ? 1 : 0,
    $note: item.note,
    $now: now,
    $version: item.version,
  };
  return [
    {
      // One line per part: the server's line wins over a local one with another id (made
      // offline on two devices). The local one's operation is then rejected by the server.
      sql: "DELETE FROM shopping_item WHERE team_id = $teamId AND part_id = $partId AND id <> $id",
      params: { $teamId: teamId, $partId: item.part_id, $id: id },
    },
    {
      sql: `INSERT INTO shopping_item (
              id, team_id, part_id, qty_centi, dismissed, note, created_at, updated_at, version, local_rev
            )
            SELECT $id, p.team_id, p.id, $qty, $dismissed, $note, $now, $now, $version, 0
              FROM part p
             WHERE p.team_id = $teamId AND p.id = $partId
            ON CONFLICT(id) DO UPDATE SET
              qty_centi = excluded.qty_centi, dismissed = excluded.dismissed, note = excluded.note,
              version = excluded.version, updated_at = excluded.updated_at,
              local_rev = shopping_item.local_rev + 1
            WHERE shopping_item.team_id = excluded.team_id`,
      params,
    },
  ];
}

const mountWire = z.object({
  engine_id: z.uuid(),
  car_id: z.uuid(),
  mounted_at: timestamp,
  unmounted_at: timestamp.nullable(),
});

const installWire = z.object({
  part_id: z.uuid(),
  car_id: z.uuid().nullable(),
  engine_id: z.uuid().nullable(),
  installed_at: timestamp,
  removed_at: timestamp.nullable(),
  removal_destination: z.enum(["stock", "scrap", "spare"]).nullable(),
  removal_reason: z.enum(["broken", "worn", "lost"]).nullable(),
  run_seconds: z.number().int().min(0),
  sessions: z.number().int().min(0),
  version: z.number().int().min(1),
});

function mountUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const m = mountWire.parse(data);
  return [
    ...(m.unmounted_at === null
      ? [
          {
            // At most one open mount per engine and per car: the server's wins over a local one
            // with another id (made offline on two devices); that one's operation is rejected.
            sql: `DELETE FROM engine_mount
                   WHERE team_id = $teamId AND id <> $id AND unmounted_at IS NULL
                     AND (engine_id = $engineId OR car_id = $carId)`,
            params: { $teamId: teamId, $id: id, $engineId: m.engine_id, $carId: m.car_id },
          },
        ]
      : []),
    {
      sql: `INSERT INTO engine_mount (id, team_id, engine_id, car_id, mounted_at, unmounted_at, created_at)
            SELECT $id, e.team_id, e.id, c.id, $mountedAt, $unmountedAt, $now
              FROM engine e
              JOIN car c ON c.team_id = e.team_id AND c.id = $carId
             WHERE e.team_id = $teamId AND e.id = $engineId
            ON CONFLICT(id) DO UPDATE SET unmounted_at = excluded.unmounted_at
              WHERE engine_mount.team_id = excluded.team_id`,
      params: {
        $id: id,
        $teamId: teamId,
        $engineId: m.engine_id,
        $carId: m.car_id,
        $mountedAt: m.mounted_at,
        $unmountedAt: m.unmounted_at,
        $now: now,
      },
    },
  ];
}

function installUpsert(teamId: string, id: string, data: unknown, now: string): SqlStatement[] {
  const i = installWire.parse(data);
  return [
    {
      // The note is local only (ADR 0023) and stays as the device has it.
      sql: `INSERT INTO part_install (
              id, team_id, part_id, car_id, engine_id, installed_at, removed_at, removal_destination,
              removal_reason, run_seconds, sessions, created_at, updated_at, version
            )
            SELECT $id, p.team_id, p.id, $carId, $engineId, $installedAt, $removedAt, $destination,
                   $reason, $runSeconds, $sessions, $now, $now, $version
              FROM part p
             WHERE p.team_id = $teamId AND p.id = $partId
               AND (($carId IS NOT NULL AND EXISTS (SELECT 1 FROM car WHERE team_id = $teamId AND id = $carId))
                 OR ($engineId IS NOT NULL AND EXISTS (SELECT 1 FROM engine WHERE team_id = $teamId AND id = $engineId)))
            ON CONFLICT(id) DO UPDATE SET
              removed_at = excluded.removed_at, removal_destination = excluded.removal_destination,
              removal_reason = excluded.removal_reason, run_seconds = excluded.run_seconds,
              sessions = excluded.sessions, version = excluded.version, updated_at = excluded.updated_at
              WHERE part_install.team_id = excluded.team_id`,
      params: {
        $id: id,
        $teamId: teamId,
        $partId: i.part_id,
        $carId: i.car_id,
        $engineId: i.engine_id,
        $installedAt: i.installed_at,
        $removedAt: i.removed_at,
        $destination: i.removal_destination,
        $reason: i.removal_reason,
        $runSeconds: i.run_seconds,
        $sessions: i.sessions,
        $now: now,
        $version: i.version,
      },
    },
  ];
}

const feedbackScale = z.number().int().min(1).max(5).nullable();
const feedbackWire = z.object({
  setup_id: z.uuid(),
  version: z.number().int().min(1),
  author_id: z.uuid(),
  steering_entry: feedbackScale,
  steering_mid: feedbackScale,
  steering_exit: feedbackScale,
  traction: feedbackScale,
  jump_stability: feedbackScale,
  braking: feedbackScale,
  engine_low_end: feedbackScale,
  engine_top_end: feedbackScale,
  track_zone: nullableString,
  note: nullableString,
  created_at: timestamp,
});

/**
 * Feedback is append-only: one the device already has is left alone (usually our own echo).
 * It needs its setup version here; one that is not (a version older than the 20 a Pull
 * carries) is skipped by the SELECT instead of failing the whole page.
 */
function feedbackUpsert(teamId: string, id: string, data: unknown): SqlStatement[] {
  const f = feedbackWire.parse(data);
  return [
    {
      sql: `INSERT OR IGNORE INTO setup_feedback (
              id, team_id, setup_id, version, author_id, steering_entry, steering_mid, steering_exit,
              traction, jump_stability, braking, engine_low_end, engine_top_end, track_zone, note, created_at
            )
            SELECT $id, v.team_id, v.setup_id, v.version, $authorId, $steeringEntry, $steeringMid,
                   $steeringExit, $traction, $jumpStability, $braking, $engineLowEnd, $engineTopEnd,
                   $zone, $note, $createdAt
              FROM setup_version v
             WHERE v.team_id = $teamId AND v.setup_id = $setupId AND v.version = $version`,
      params: {
        $id: id,
        $teamId: teamId,
        $setupId: f.setup_id,
        $version: f.version,
        $authorId: f.author_id,
        $steeringEntry: f.steering_entry,
        $steeringMid: f.steering_mid,
        $steeringExit: f.steering_exit,
        $traction: f.traction,
        $jumpStability: f.jump_stability,
        $braking: f.braking,
        $engineLowEnd: f.engine_low_end,
        $engineTopEnd: f.engine_top_end,
        $zone: f.track_zone,
        $note: f.note,
        $createdAt: f.created_at,
      },
    },
  ];
}

function setupUpsert(teamId: string, id: string, data: unknown): SqlStatement[] {
  const setup = setupWire.parse(data);
  return [
    {
      // name and notes stay as the device has them: renaming is local only for now (the
      // server API has no rename), so the server value would erase a local edit.
      sql: `INSERT INTO setup (
              id, team_id, car_id, name, current_version, notes, created_at, updated_at,
              version, local_rev
            ) VALUES (
              $id, $teamId, $carId, $name, $current, $notes, $createdAt, $updatedAt, $version, 0
            )
            ON CONFLICT(id) DO UPDATE SET
              current_version = max(setup.current_version, excluded.current_version),
              version = excluded.version, updated_at = excluded.updated_at,
              deleted_at = NULL, local_rev = setup.local_rev + 1
            WHERE setup.team_id = excluded.team_id`,
      params: {
        $id: id,
        $teamId: teamId,
        $carId: setup.car_id,
        $name: setup.name,
        $current: setup.current_version,
        $notes: setup.notes,
        $createdAt: setup.created_at,
        $updatedAt: setup.updated_at,
        $version: setup.version,
      },
    },
  ];
}

function versionsUpsert(teamId: string, id: string, data: unknown): SqlStatement[] {
  const parsed = versionsWire.parse(data);
  if (parsed.setup_id !== id) throw new Error("setup version payload names another setup");
  return parsed.versions.map((v) => ({
    // Versions are immutable: a version the device already has is left alone.
    sql: `INSERT OR IGNORE INTO setup_version (
            team_id, setup_id, version, data, schema_version, based_on_setup_id,
            based_on_version, change_summary, created_at
          ) VALUES ($teamId, $setupId, $version, $data, $schemaVersion, $basedOnSetup,
                    $basedOnVersion, $summary, $createdAt)`,
    params: {
      $teamId: teamId,
      $setupId: id,
      $version: v.version,
      $data: JSON.stringify(v.data),
      $schemaVersion: v.schema_version,
      $basedOnSetup: v.based_on_setup_id,
      $basedOnVersion: v.based_on_version,
      $summary: v.change_summary,
      $createdAt: v.created_at,
    },
  }));
}

export interface SyncEntity {
  upsert: ChangeStatements;
  remove: ChangeStatements;
  /**
   * After the server accepted an operation: the server version this entity now has, so the
   * next local edit is based on it (otherwise it would conflict with our own earlier edit).
   */
  adopt?: (teamId: string, entityId: string, version: number) => SqlStatement;
}

const noop: ChangeStatements = () => [];

export const syncEntities: Readonly<Record<string, SyncEntity>> = {
  "garage.car": {
    upsert: (teamId, id, data, now) => carUpsert(teamId, id, data, now),
    remove: softDelete("car"),
    adopt: adoptVersion("car"),
  },
  "garage.engine": {
    upsert: engineUpsert,
    remove: softDelete("engine"),
    adopt: adoptVersion("engine"),
  },
  "garage.tyre_set": {
    upsert: tyreUpsert,
    remove: softDelete("tyre_set"),
    adopt: adoptVersion("tyre_set"),
  },
  "garage.part": {
    upsert: partUpsert,
    remove: softDelete("part"),
    adopt: adoptVersion("part"),
  },
  // Order matters when held changes are replayed: these need their car, engine or part first.
  "garage.engine_mount": { upsert: mountUpsert, remove: noop },
  "garage.part_install": { upsert: installUpsert, remove: noop, adopt: adoptVersion("part_install") },
  "garage.stock_movement": {
    upsert: movementUpsert,
    // Movements are never deleted (an archived part keeps its history).
    remove: noop,
  },
  "garage.shopping_item": {
    upsert: shoppingUpsert,
    remove: (teamId, id) => [
      {
        sql: "DELETE FROM shopping_item WHERE team_id = $teamId AND id = $id",
        params: { $teamId: teamId, $id: id },
      },
    ],
    adopt: adoptVersion("shopping_item"),
  },
  "setup.setup": {
    upsert: (teamId, id, data) => setupUpsert(teamId, id, data),
    remove: softDelete("setup"),
  },
  // Versions are never deleted; the setup tombstone hides them.
  "setup.setup_version": {
    upsert: (teamId, id, data) => versionsUpsert(teamId, id, data),
    remove: noop,
  },
  // After the versions it hangs from.
  "setup.setup_feedback": {
    upsert: (teamId, id, data) => feedbackUpsert(teamId, id, data),
    remove: noop,
  },
};

const catalogWire = z.object({
  scales: z
    .array(
      z.object({
        code: z.string().min(1).max(20),
        name: z.string(),
        active: z.boolean(),
        sort_order: z.number().int(),
      }),
    )
    .max(200),
  powertrains: z
    .array(
      z.object({
        code: z.string().min(1).max(20),
        name: z.string(),
        family: z.string(),
        active: z.boolean(),
        sort_order: z.number().int(),
      }),
    )
    .max(200),
});

/** Replaces the catalogues. A value the server dropped is switched off, never deleted (cars use it). */
export function catalogStatements(snapshot: unknown): SqlStatement[] {
  const catalog = catalogWire.parse(snapshot);
  return [
    { sql: "UPDATE scale SET active = 0" },
    { sql: "UPDATE powertrain SET active = 0" },
    ...catalog.scales.map((s) => ({
      sql: `INSERT INTO scale (code, name, active, sort_order) VALUES ($code, $name, $active, $sort)
            ON CONFLICT(code) DO UPDATE SET name = excluded.name, active = excluded.active,
              sort_order = excluded.sort_order`,
      params: { $code: s.code, $name: s.name, $active: s.active ? 1 : 0, $sort: s.sort_order },
    })),
    ...catalog.powertrains.map((p) => ({
      sql: `INSERT INTO powertrain (code, family, name, active, sort_order)
            VALUES ($code, $family, $name, $active, $sort)
            ON CONFLICT(code) DO UPDATE SET family = excluded.family, name = excluded.name,
              active = excluded.active, sort_order = excluded.sort_order`,
      params: {
        $code: p.code,
        $family: p.family,
        $name: p.name,
        $active: p.active ? 1 : 0,
        $sort: p.sort_order,
      },
    })),
  ];
}
