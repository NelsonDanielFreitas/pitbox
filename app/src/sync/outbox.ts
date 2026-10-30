import type { SqlParams, SqlStatement } from "@/db/protocol";
import { newId } from "@/shared/lib/id";

import { OpKind, type OpKindValue } from "./signing";

/**
 * One local write as an operation for the server. The statement returned by
 * outboxStatement goes into the SAME transaction as the data write (atomic: no change
 * without its operation, no operation without its change).
 */
export interface OutboxOp {
  teamId: string;
  entity: string;
  entityId: string;
  kind: OpKindValue;
  /** JSON-serialisable object; null clears an optional field, absence leaves it alone. */
  fields: Record<string, unknown>;
  /** Server version the writer saw. Required for patch and delete. */
  baseVersion: number | null;
}

/** Fired after a local write is committed; the engine listens to push soon after. */
type Listener = () => void;
const writeListeners = new Set<Listener>();

export function onLocalWrite(listener: Listener): () => void {
  writeListeners.add(listener);
  return () => writeListeners.delete(listener);
}

/** Repositories call this once the transaction has committed. */
export function notifyLocalWrite(): void {
  for (const listener of writeListeners) listener();
}

/** Only queues the operation when this SELECT finds a row (the write above it took effect). */
export interface OutboxGuard {
  sql: string;
  params: SqlParams;
}

export function outboxStatement(
  op: OutboxOp,
  guard?: OutboxGuard,
  now: () => Date = () => new Date(),
  opId: string = newId(),
): SqlStatement {
  if ((op.kind === OpKind.patch || op.kind === OpKind.delete) && op.baseVersion === null) {
    throw new Error("patch and delete operations need a base version");
  }
  const at = now();
  return {
    sql: `INSERT INTO sync_outbox (
            op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at
          ) SELECT $obOpId, $obTeamId, $obEntity, $obEntityId, $obKind, $obFields, $obBase, $obTs, $obNow
            WHERE ${guard ? `EXISTS (${guard.sql})` : "1 = 1"}`,
    params: {
      $obOpId: opId,
      $obTeamId: op.teamId,
      $obEntity: op.entity,
      $obEntityId: op.entityId,
      $obKind: op.kind,
      $obFields: JSON.stringify(op.fields),
      $obBase: op.baseVersion,
      $obTs: at.getTime(),
      $obNow: at.toISOString(),
      ...guard?.params,
    },
  };
}

/**
 * The append operation of a stock movement that a statement ABOVE this one inserted: the
 * fields are read from the stored row, so movements whose quantity or part is computed in
 * SQL (an undo, a count adjustment, an install) are sent exactly as they were stored. It
 * queues nothing when the movement was not inserted (an archived part, for example).
 *
 * The store (where it was bought) is local only (ADR 0023): it travels folded into the note
 * ("Loja: X · note"). reverts_movement_id never travels: the undo is just an adjustment.
 */
export function movementOutboxStatement(
  teamId: string,
  movementId: string,
  now: () => Date = () => new Date(),
  opId: string = newId(),
): SqlStatement {
  const at = now();
  return {
    sql: `INSERT INTO sync_outbox (
            op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at
          ) SELECT $obOpId, m.team_id, 'garage.stock_movement', m.id, ${OpKind.append},
                   json_object(
                     'part_id', m.part_id,
                     'qty_delta_centi', m.qty_delta_centi,
                     'reason', m.reason,
                     'note', substr(CASE
                       WHEN m.store IS NULL THEN m.note
                       WHEN m.note IS NULL THEN 'Loja: ' || m.store
                       ELSE 'Loja: ' || m.store || ' · ' || m.note END, 1, 500),
                     'occurred_at', m.occurred_at,
                     'unit_cost_cents', m.unit_cost_cents,
                     'loss_reason', m.loss_reason
                   ),
                   NULL, $obTs, $obNow
              FROM stock_movement m
             WHERE m.team_id = $obTeamId AND m.id = $obMovementId`,
    params: {
      $obOpId: opId,
      $obTeamId: teamId,
      $obMovementId: movementId,
      $obTs: at.getTime(),
      $obNow: at.toISOString(),
    },
  };
}

/**
 * An operation whose fields come from a stored row (SQL), for changes whose values are
 * computed in the statement that makes them (a close time, a mount made by a swap). The
 * row decides: when the SELECT finds nothing (the write above did not happen, or the
 * pre-state no longer holds) nothing is queued.
 */
export interface RowOutbox {
  entity: string;
  kind: OpKindValue;
  /** "table alias WHERE ..." */
  from: string;
  teamId: string;
  entityId: string;
  /** SQL expression for the JSON object text. */
  fields: string;
  /** SQL expression: the version the writer saw, or NULL. */
  baseVersion: string;
  params: SqlParams;
}

export function rowOutboxStatement(
  spec: RowOutbox,
  now: () => Date = () => new Date(),
  opId: string = newId(),
): SqlStatement {
  const at = now();
  return {
    sql: `INSERT INTO sync_outbox (
            op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at
          ) SELECT $obOpId, ${spec.teamId}, '${spec.entity}', ${spec.entityId}, ${spec.kind},
                   ${spec.fields}, ${spec.baseVersion}, $obTs, $obNow
              FROM ${spec.from}`,
    params: { ...spec.params, $obOpId: opId, $obTs: at.getTime(), $obNow: at.toISOString() },
  };
}

/** Create of the install row a statement ABOVE inserted. The stock movement travels on its own. */
export function installCreatedOutbox(teamId: string, installId: string): SqlStatement {
  return rowOutboxStatement({
    entity: "garage.part_install",
    kind: OpKind.create,
    from: "part_install i WHERE i.team_id = $obTeamId AND i.id = $obId",
    teamId: "i.team_id",
    entityId: "i.id",
    fields: `json_object('part_id', i.part_id, 'car_id', i.car_id, 'engine_id', i.engine_id,
                         'installed_at', i.installed_at)`,
    baseVersion: "NULL",
    params: { $obTeamId: teamId, $obId: installId },
  });
}

/**
 * Close of an install, queued BEFORE the UPDATE that closes it (it only fires while the install is
 * still open). The close time is the same expression the UPDATE uses.
 */
export function installClosedOutbox(
  teamId: string,
  installId: string,
  removal: { at: string; destination: string; reason: string | null },
): SqlStatement {
  return rowOutboxStatement({
    entity: "garage.part_install",
    kind: OpKind.patch,
    from: "part_install i WHERE i.team_id = $obTeamId AND i.id = $obId AND i.removed_at IS NULL",
    teamId: "i.team_id",
    entityId: "i.id",
    fields: `json_object(
               'removed_at', CASE WHEN $obAt > i.installed_at THEN $obAt
                             ELSE strftime('%Y-%m-%dT%H:%M:%fZ', i.installed_at, '+0.001 seconds') END,
               'removal_destination', $obDestination,
               'removal_reason', $obReason)`,
    baseVersion: "i.version",
    params: {
      $obTeamId: teamId,
      $obId: installId,
      $obAt: removal.at,
      $obDestination: removal.destination,
      $obReason: removal.reason,
    },
  });
}

/** Create of the mount row a statement ABOVE inserted. */
export function mountCreatedOutbox(teamId: string, mountId: string): SqlStatement {
  return rowOutboxStatement({
    entity: "garage.engine_mount",
    kind: OpKind.create,
    from: "engine_mount m WHERE m.team_id = $obTeamId AND m.id = $obId",
    teamId: "m.team_id",
    entityId: "m.id",
    fields: "json_object('engine_id', m.engine_id, 'car_id', m.car_id, 'mounted_at', m.mounted_at)",
    baseVersion: "NULL",
    params: { $obTeamId: teamId, $obId: mountId },
  });
}

/**
 * Close of the mount a statement ABOVE closed at `at`. A mount has no version on the server
 * (closing is safe to repeat), so the base version is always 1.
 */
export function mountClosedOutbox(teamId: string, mountId: string, at: string): SqlStatement {
  return rowOutboxStatement({
    entity: "garage.engine_mount",
    kind: OpKind.patch,
    from: "engine_mount m WHERE m.team_id = $obTeamId AND m.id = $obId AND m.unmounted_at = $obAt",
    teamId: "m.team_id",
    entityId: "m.id",
    fields: "json_object('unmounted_at', m.unmounted_at)",
    baseVersion: "1",
    params: { $obTeamId: teamId, $obId: mountId, $obAt: at },
  });
}

function sameValue(a: unknown, b: unknown): boolean {
  const x = a === undefined ? null : a;
  const y = b === undefined ? null : b;
  if (typeof x === "object" && x !== null && typeof y === "object" && y !== null) {
    return stable(x) === stable(y);
  }
  return x === y;
}

/** JSON with sorted keys, so two objects with the same content compare equal. */
function stable(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stable).join(",")}]`;
  if (typeof value === "object" && value !== null) {
    const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) => a.localeCompare(b));
    return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${stable(v)}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

/**
 * The fields of `next` whose value differs from `current`. An edit sends only these, so two
 * devices that touch different fields merge without a conflict (ADR 0025).
 */
export function changedFields(
  current: Record<string, unknown>,
  next: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(next)) {
    if (!sameValue(current[key], value)) out[key] = value;
  }
  return out;
}
