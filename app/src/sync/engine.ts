import { Code, ConnectError } from "@connectrpc/connect";

import { LocalConstraintError } from "@/db/errors";
import type { SqlStatement } from "@/db/protocol";
import { newId } from "@/shared/lib/id";

import { defaultSyncDatabase, type SyncDatabase } from "./database";
import {
  forgetDeviceKey,
  exportPublicKey,
  loadOrCreateDeviceKey,
  signBytes,
  type KeyStore,
} from "./device-key";
import { catalogStatements, syncEntities } from "./entities";
import {
  signingMessage,
  registrationMessage,
  type OpKindValue,
} from "./signing";
import type { PulledChange, PushOp, PushResult, Resolution, SyncApi } from "./sync-api";

// The sync engine (F1-13b, ADR 0003/0025). Rules it keeps:
//  - the UI never waits for it: it runs in the background in small batches and short local
//    transactions, and nothing in final mode depends on it;
//  - the network is "available" only after a real request worked (navigator.onLine lies);
//  - it syncs whenever it can: after a local write, on `online`, when the tab becomes
//    visible, at start, and every pollMs; failures back off exponentially (with jitter);
//  - a 401 stops it and asks for a new login instead of looping;
//  - one syncer per user across tabs (Web Locks);
//  - the Pull never overwrites an entity that still has operations waiting to be sent.

export type SyncPhase = "idle" | "syncing" | "offline" | "error" | "auth" | "blocked";

export interface SyncSnapshot {
  phase: SyncPhase;
  /** Operations waiting to be sent. */
  pending: number;
  /** Operations the server rejected (kept so the mechanic can see and dismiss them). */
  failed: number;
  conflicts: number;
  lastSyncAt: string | null;
  /** Stable code of the last problem, never a server message. */
  problem: SyncProblem | null;
}

export type SyncProblem =
  | "network"
  | "service"
  | "server"
  | "device_revoked"
  | "pass_unavailable"
  | "local_data"
  | "contract";

const batchSize = 50;
const maxBatchesPerCycle = 20;
const maxPullPagesPerCycle = 20;
const pullLimit = 200;
const passRenewBeforeMs = 24 * 3_600_000;
const maxBackoffMs = 5 * 60_000;

export interface EngineOptions {
  db?: SyncDatabase;
  api: SyncApi;
  userId: string;
  userHash: string;
  keyStore: KeyStore;
  deviceName?: string;
  now?: () => Date;
  random?: () => number;
  pollMs?: number;
  debounceMs?: number;
  /** Runs fn if this tab may sync; resolves false when another tab holds the lock. */
  exclusive?: (fn: () => Promise<void>) => Promise<boolean>;
  onDataChanged?: () => void;
  onUnauthenticated?: () => void;
}

type Failure = "auth" | "transient" | "team" | "local";

function classify(error: unknown): Failure {
  if (error instanceof ConnectError) {
    if (error.code === Code.Unauthenticated) return "auth";
    if (error.code === Code.PermissionDenied || error.code === Code.NotFound) return "team";
    return "transient";
  }
  if (error instanceof BlockedError) return "team";
  if (error instanceof LocalConstraintError || error instanceof Error) {
    // Not an RPC error: fetch failures surface as ConnectError, so this is local or a bug.
    return "local";
  }
  return "transient";
}

/**
 * Why a transient failure happened. Only a request that never got an answer is "network":
 * connect-es leaves a failed fetch as Code.Unknown with the TypeError as cause. An answer that
 * says the service is missing or broken (404/Unimplemented, 5xx) is the SERVER's problem, and
 * telling the mechanic "offline" for it would hide a fault that more waiting cannot fix.
 */
function problemOf(error: unknown): SyncProblem {
  if (!(error instanceof ConnectError)) return "network";
  switch (error.code) {
    case Code.DeadlineExceeded:
    case Code.Canceled:
      return "network";
    case Code.Unknown:
      return error.cause instanceof TypeError ? "network" : "service";
    case Code.Unimplemented:
    case Code.Unavailable:
    case Code.Internal:
    case Code.DataLoss:
      return "service";
    default:
      return "server";
  }
}

class BlockedError extends Error {
  constructor(readonly problem: SyncProblem) {
    super(problem);
  }
}

interface OutboxRow {
  seq: number;
  op_id: string;
  entity: string;
  entity_id: string;
  kind: number;
  fields_json: string;
  base_version: number | null;
  client_ts: number;
}

interface HeldRow {
  entity: string;
  entity_id: string;
  kind: number;
  data: string | null;
}

export class SyncEngine {
  private readonly db: SyncDatabase;
  private readonly api: SyncApi;
  private readonly now: () => Date;
  private readonly random: () => number;
  private readonly pollMs: number;
  private readonly debounceMs: number;
  private readonly listeners = new Set<() => void>();
  private snapshot: SyncSnapshot = {
    phase: "idle",
    pending: 0,
    failed: 0,
    conflicts: 0,
    lastSyncAt: null,
    problem: null,
  };
  private timer: ReturnType<typeof setTimeout> | undefined;
  private debounce: ReturnType<typeof setTimeout> | undefined;
  private running: Promise<void> | undefined;
  private again = false;
  private started = false;
  private failures = 0;
  private blockedTeams = new Map<string, SyncProblem>();

  constructor(private readonly options: EngineOptions) {
    this.db = options.db ?? defaultSyncDatabase;
    this.api = options.api;
    this.now = options.now ?? (() => new Date());
    this.random = options.random ?? Math.random;
    this.pollMs = options.pollMs ?? 15_000;
    this.debounceMs = options.debounceMs ?? 300;
  }

  // --- observable state (useSyncExternalStore) ------------------------------------------

  getSnapshot = (): SyncSnapshot => this.snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private set(patch: Partial<SyncSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const listener of this.listeners) listener();
  }

  // --- life cycle -----------------------------------------------------------------------

  start(): void {
    if (this.started) return;
    this.started = true;
    void this.refreshCounts();
    this.requestSync(true);
  }

  stop(): void {
    this.started = false;
    clearTimeout(this.timer);
    clearTimeout(this.debounce);
    this.timer = undefined;
    this.debounce = undefined;
  }

  /** A local write, `online`, tab visible: sync soon. `immediate` skips the debounce and backoff. */
  requestSync(immediate = false): void {
    if (!this.started) return;
    if (immediate) {
      this.failures = 0;
      clearTimeout(this.debounce);
      void this.syncNow();
      return;
    }
    clearTimeout(this.debounce);
    this.debounce = setTimeout(() => void this.syncNow(), this.debounceMs);
    void this.refreshCounts();
  }

  /**
   * Single flight: a request that arrives during a cycle makes one more cycle after it, and the
   * returned promise settles only when no cycle is left (so awaiting it means "quiet").
   */
  syncNow(): Promise<void> {
    if (!this.started) return Promise.resolve();
    if (this.running) {
      this.again = true;
      return this.running;
    }
    this.running = (async () => {
      do {
        this.again = false;
        await this.cycle();
      } while (this.again && this.started);
    })().finally(() => {
      this.running = undefined;
    });
    return this.running;
  }

  /** Stops the timers and waits for the cycle in flight (used before closing the database). */
  async close(): Promise<void> {
    this.stop();
    await this.running?.catch(() => undefined);
  }

  private scheduleNext(): void {
    clearTimeout(this.timer);
    if (!this.started || this.snapshot.phase === "auth") return;
    const base =
      this.failures === 0
        ? this.pollMs
        : Math.min(maxBackoffMs, this.pollMs * 2 ** Math.min(this.failures, 10));
    // Jitter 0.5x..1.5x so many devices do not retry in step.
    const delay = Math.round(base * (0.5 + this.random()));
    this.timer = setTimeout(() => void this.syncNow(), delay);
  }

  // --- one cycle ------------------------------------------------------------------------

  private async cycle(): Promise<void> {
    clearTimeout(this.timer);
    const ran = await (this.options.exclusive ?? ((fn) => fn().then(() => true)))(() => this.work());
    if (!ran) {
      // Another tab syncs; it will tell us about data changes. Check back later.
      this.failures = 0;
    }
    this.scheduleNext();
  }

  private async work(): Promise<void> {
    this.set({ phase: "syncing" });
    let outcome: Failure | null = null;
    let problem: SyncProblem | null = null;
    try {
      const teams = await this.db.query<{ id: string }>(
        "SELECT id FROM team WHERE deleted_at IS NULL ORDER BY id",
      );
      for (const team of teams) {
        if (this.blockedTeams.has(team.id)) continue;
        try {
          await this.syncTeam(team.id);
        } catch (error) {
          const kind = classify(error);
          if (kind === "auth") throw error;
          if (error instanceof BlockedError) {
            this.blockedTeams.set(team.id, error.problem);
            problem = error.problem;
            outcome = "team";
          } else if (kind === "team") {
            problem = "server";
            outcome = "team";
          } else if (kind === "local") {
            problem = error instanceof ContractError ? "contract" : "local_data";
            outcome = "local";
          } else {
            problem = problemOf(error);
            outcome ??= "transient";
            break; // the rest would fail the same way
          }
        }
      }
    } catch (error) {
      const kind = classify(error);
      outcome = kind;
      problem = kind === "auth" ? null : kind === "transient" ? problemOf(error) : "local_data";
    }

    await this.refreshCounts();
    if (outcome === null) {
      this.failures = 0;
      this.set({ phase: "idle", problem: null, lastSyncAt: this.now().toISOString() });
      return;
    }
    if (outcome === "auth") {
      this.set({ phase: "auth", problem: null });
      this.options.onUnauthenticated?.();
      return;
    }
    this.failures += 1;
    const blocked = problem === "device_revoked" || problem === "pass_unavailable";
    this.set({
      phase: blocked ? "blocked" : problem === "network" ? "offline" : "error",
      problem,
    });
  }

  private async syncTeam(teamId: string): Promise<void> {
    const deviceId = await this.ensureDevice(teamId);
    const pass = await this.ensurePass(teamId, deviceId);
    await this.push(teamId, deviceId, pass);
    await this.pull(teamId);
    await this.drainHeld(teamId);
    await this.refreshConflicts(teamId);
  }

  // --- device and pass ------------------------------------------------------------------

  private async meta(key: string): Promise<string | null> {
    const rows = await this.db.query<{ value: string }>(
      "SELECT value FROM app_meta WHERE key = $key",
      { $key: key },
    );
    return rows[0]?.value ?? null;
  }

  private setMeta(key: string, value: string): SqlStatement {
    return {
      sql: "INSERT INTO app_meta (key, value) VALUES ($key, $value) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
      params: { $key: key, $value: value },
    };
  }

  private async ensureDevice(teamId: string): Promise<string> {
    const key = `sync.device.${teamId}`;
    const stored = await this.meta(key);
    // A device id without its key (the browser cleared IndexedDB) cannot sign: start over.
    const pair = await loadOrCreateDeviceKey(this.options.keyStore, this.options.userHash);
    if (stored) {
      const known = await this.meta(`sync.device-key.${teamId}`);
      const spki = toBase64(await exportPublicKey(pair));
      if (known === spki) return stored;
    }
    const publicKey = await exportPublicKey(pair);
    const proof = await signBytes(pair, registrationMessage(teamId, this.options.userId, publicKey));
    const { deviceId } = await this.api.registerDevice({
      teamId,
      requestId: newId(),
      deviceName: this.options.deviceName ?? "Browser",
      platform: "web",
      publicKey,
      proof,
    });
    await this.db.transaction([
      this.setMeta(key, deviceId),
      this.setMeta(`sync.device-key.${teamId}`, toBase64(publicKey)),
    ]);
    return deviceId;
  }

  private async ensurePass(teamId: string, deviceId: string, force = false): Promise<string> {
    const key = `sync.pass.${teamId}`;
    const raw = await this.meta(key);
    const stored = parsePass(raw);
    const fresh = stored && Date.parse(stored.expiresAt) - this.now().getTime() > passRenewBeforeMs;
    if (stored && fresh && !force) return stored.token;
    try {
      const issued = await this.api.issueOfflinePass({ teamId, deviceId });
      await this.db.transaction([
        this.setMeta(key, JSON.stringify({ token: issued.token, expiresAt: issued.expiresAt.toISOString() })),
      ]);
      return issued.token;
    } catch (error) {
      // An older pass still covers operations made before it expired (ADR 0025).
      if (stored && classify(error) === "transient") return stored.token;
      throw error;
    }
  }

  /** The owner revoked this device, or the user did: stop until they register it again. */
  async reregisterDevice(teamId: string): Promise<void> {
    await forgetDeviceKey(this.options.keyStore, this.options.userHash);
    await this.db.transaction([
      { sql: "DELETE FROM app_meta WHERE key IN ($a, $b, $c)", params: {
        $a: `sync.device.${teamId}`, $b: `sync.device-key.${teamId}`, $c: `sync.pass.${teamId}` } },
    ]);
    this.blockedTeams.delete(teamId);
    this.requestSync(true);
  }

  // --- push -----------------------------------------------------------------------------

  private async push(teamId: string, deviceId: string, initialPass: string): Promise<void> {
    let pass = initialPass;
    const pair = await loadOrCreateDeviceKey(this.options.keyStore, this.options.userHash);
    let renewed = false;
    for (let batch = 0; batch < maxBatchesPerCycle; batch += 1) {
      const rows = await this.db.query<OutboxRow>(
        `SELECT seq, op_id, entity, entity_id, kind, fields_json, base_version, client_ts
           FROM sync_outbox WHERE team_id = $teamId AND state = 'pending'
          ORDER BY seq LIMIT ${batchSize}`,
        { $teamId: teamId },
      );
      if (rows.length === 0) return;

      // One edit per entity per batch: the second one must be based on the version the first
      // produces, which is only known from the answer. It waits for the next batch.
      const seen = new Set<string>();
      const sendable: OutboxRow[] = [];
      for (const row of rows) {
        const key = `${row.entity}/${row.entity_id}`;
        if (row.base_version !== null && seen.has(key)) break;
        seen.add(key);
        sendable.push(row);
      }
      rows.length = sendable.length;

      const operations: PushOp[] = [];
      for (const row of rows) {
        const op = {
          opId: row.op_id,
          entity: row.entity,
          entityId: row.entity_id,
          kind: row.kind as OpKindValue,
          fieldsJson: new TextEncoder().encode(row.fields_json),
          baseVersion: row.base_version,
          clientTsMs: row.client_ts,
        };
        const signature = await signBytes(pair, await signingMessage(teamId, deviceId, op));
        operations.push({ ...op, signature });
      }

      const results = await this.api.push({ teamId, deviceId, offlinePass: pass, operations });
      const byOp = new Map(results.map((r) => [r.opId, r]));

      if (results.some((r) => r.reason === "device_revoked")) throw new BlockedError("device_revoked");
      if (results.some((r) => r.reason === "pass_invalid")) {
        if (renewed) throw new BlockedError("pass_unavailable");
        renewed = true;
        pass = await this.ensurePass(teamId, deviceId, true);
        await this.db.transaction(this.resultStatements(teamId, rows, byOp, true));
        continue;
      }
      await this.db.transaction(this.resultStatements(teamId, rows, byOp, false));
      await this.refreshCounts();
    }
  }

  private resultStatements(
    teamId: string,
    rows: OutboxRow[],
    results: Map<string, PushResult>,
    retryOnly: boolean,
  ): SqlStatement[] {
    const out: SqlStatement[] = [];
    for (const row of rows) {
      const result = results.get(row.op_id);
      const bump = { sql: "UPDATE sync_outbox SET attempts = attempts + 1 WHERE op_id = $op", params: { $op: row.op_id } };
      if (!result || retryOnly || result.reason === "pass_invalid") {
        out.push(bump);
        continue;
      }
      switch (result.status) {
        case "applied":
        case "merged": {
          out.push({ sql: "DELETE FROM sync_outbox WHERE op_id = $op", params: { $op: row.op_id } });
          if (result.version !== null) {
            const adopt = syncEntities[row.entity]?.adopt;
            if (adopt) out.push(adopt(teamId, row.entity_id, result.version));
            if (result.status === "applied") {
              // The next edit of the same entity was based on the old version: rebase it onto
              // what our own operation produced, or it would conflict with itself.
              out.push({
                sql: `UPDATE sync_outbox SET base_version = $version
                       WHERE team_id = $teamId AND entity = $entity AND entity_id = $entityId
                         AND state = 'pending' AND seq > $seq
                         AND base_version IS NOT NULL AND base_version < $version`,
                params: { $version: result.version, $teamId: teamId, $entity: row.entity, $entityId: row.entity_id, $seq: row.seq },
              });
            }
          }
          break;
        }
        case "conflict":
          out.push({
            sql: "UPDATE sync_outbox SET state = 'conflict', attempts = attempts + 1 WHERE op_id = $op",
            params: { $op: row.op_id },
          });
          break;
        case "rejected":
          out.push({
            sql: "UPDATE sync_outbox SET state = 'rejected', reason = $reason, attempts = attempts + 1 WHERE op_id = $op",
            params: { $op: row.op_id, $reason: (result.reason ?? "rejected").slice(0, 64) },
          });
          break;
        default:
          out.push(bump);
      }
    }
    return out;
  }

  // --- pull -----------------------------------------------------------------------------

  private async pull(teamId: string): Promise<void> {
    const cursorKey = `sync.cursor.${teamId}`;
    let cursor = (await this.meta(cursorKey)) ?? "";
    for (let page = 0; page < maxPullPagesPerCycle; page += 1) {
      const catalogVersion = (await this.meta("sync.catalog")) ?? "";
      const response = await this.api.pull({ teamId, cursor, limit: pullLimit, catalogVersion });

      const pending = new Set(
        (
          await this.db.query<{ entity_id: string }>(
            "SELECT DISTINCT entity_id FROM sync_outbox WHERE team_id = $teamId AND state = 'pending'",
            { $teamId: teamId },
          )
        ).map((r) => r.entity_id),
      );

      const prelude: SqlStatement[] = [];
      if (response.catalogSnapshot) {
        prelude.push(
          ...catalogStatements(response.catalogSnapshot),
          this.setMeta("sync.catalog", response.catalogVersion),
        );
      }
      const cursorStatement = this.setMeta(cursorKey, response.nextCursor);
      const changes = response.changes.filter((c) => c.entity in syncEntities);
      const statements = changes.flatMap((change) => this.changeStatements(teamId, change, pending));

      try {
        await this.db.transaction([...prelude, ...statements, cursorStatement]);
      } catch (error) {
        if (!(error instanceof LocalConstraintError)) throw error;
        // One row does not fit (e.g. a unique transponder): hold it and apply the others.
        await this.db.transaction(prelude.length > 0 ? prelude : [cursorStatement]);
        for (const change of changes) {
          try {
            await this.db.transaction(this.changeStatements(teamId, change, pending));
          } catch (inner) {
            if (!(inner instanceof LocalConstraintError)) throw inner;
            await this.db.transaction([this.holdStatement(teamId, change)]);
          }
        }
        await this.db.transaction([cursorStatement]);
      }

      if (changes.length > 0) this.options.onDataChanged?.();
      cursor = response.nextCursor;
      if (!response.hasMore) return;
    }
  }

  private holdStatement(teamId: string, change: PulledChange): SqlStatement {
    return {
      sql: `INSERT INTO sync_held (team_id, entity, entity_id, kind, data)
            VALUES ($teamId, $entity, $entityId, $kind, $data)
            ON CONFLICT(team_id, entity, entity_id) DO UPDATE SET kind = excluded.kind, data = excluded.data`,
      params: {
        $teamId: teamId,
        $entity: change.entity,
        $entityId: change.entityId,
        $kind: change.deleted ? 2 : 1,
        $data: change.deleted ? null : JSON.stringify(change.data),
      },
    };
  }

  /** Statements that apply one pulled change, or hold it while the entity has pending operations. */
  private changeStatements(teamId: string, change: PulledChange, pending: Set<string>): SqlStatement[] {
    const entity = syncEntities[change.entity];
    if (!entity) return [];
    if (pending.has(change.entityId)) return [this.holdStatement(teamId, change)];
    const now = this.now().toISOString();
    let applied: SqlStatement[];
    try {
      applied = change.deleted
        ? entity.remove(teamId, change.entityId, null, now)
        : entity.upsert(teamId, change.entityId, change.data, now);
    } catch (error) {
      // Data that does not match the contract is held, never applied half-way.
      if (error instanceof Error) throw new ContractError(error.message, { cause: error });
      throw error;
    }
    return [
      ...applied,
      {
        sql: "DELETE FROM sync_held WHERE team_id = $teamId AND entity = $entity AND entity_id = $entityId",
        params: { $teamId: teamId, $entity: change.entity, $entityId: change.entityId },
      },
    ];
  }

  /** Applies held changes whose entity is free now (or whose missing parent has arrived). */
  private async drainHeld(teamId: string): Promise<void> {
    const order = Object.keys(syncEntities);
    const held = await this.db.query<HeldRow>(
      `SELECT entity, entity_id, kind, data FROM sync_held h
        WHERE team_id = $teamId
          AND NOT EXISTS (SELECT 1 FROM sync_outbox o WHERE o.team_id = h.team_id
                            AND o.entity_id = h.entity_id AND o.state = 'pending')`,
      { $teamId: teamId },
    );
    held.sort((a, b) => order.indexOf(a.entity) - order.indexOf(b.entity));
    let changed = false;
    for (const row of held) {
      try {
        const data: unknown = row.data === null ? null : JSON.parse(row.data);
        const change: PulledChange = {
          entity: row.entity,
          entityId: row.entity_id,
          deleted: row.kind === 2,
          data: data as PulledChange["data"],
        };
        await this.db.transaction(this.changeStatements(teamId, change, new Set()));
        changed = true;
      } catch (error) {
        // Still does not fit (parent missing, constraint): stays held and is tried again.
        if (!(error instanceof LocalConstraintError) && !(error instanceof ContractError)) throw error;
      }
    }
    if (changed) this.options.onDataChanged?.();
  }

  // --- conflicts ------------------------------------------------------------------------

  private async refreshConflicts(teamId: string): Promise<void> {
    const conflicts = await this.api.listConflicts(teamId);
    const statements: SqlStatement[] = [
      { sql: "DELETE FROM sync_conflict WHERE team_id = $teamId", params: { $teamId: teamId } },
      ...conflicts.map((c) => ({
        sql: `INSERT INTO sync_conflict (id, team_id, op_id, entity, entity_id, field, server_value, client_value, created_at)
              VALUES ($id, $teamId, $opId, $entity, $entityId, $field, $server, $client, $createdAt)`,
        params: {
          $id: c.id,
          $teamId: teamId,
          $opId: c.opId,
          $entity: c.entity,
          $entityId: c.entityId,
          $field: c.field,
          $server: JSON.stringify(c.serverValue),
          $client: JSON.stringify(c.clientValue),
          $createdAt: c.createdAt,
        },
      })),
      // A conflict that is no longer open was resolved: its operation has nothing left to show.
      {
        sql: `DELETE FROM sync_outbox WHERE team_id = $teamId AND state = 'conflict'
                AND op_id NOT IN (SELECT op_id FROM sync_conflict WHERE team_id = $teamId)`,
        params: { $teamId: teamId },
      },
    ];
    await this.db.transaction(statements);
    await this.refreshCounts();
  }

  /** Sends the mechanic's choice. Needs the network: resolving is a decision, not an offline write. */
  async resolveConflict(
    teamId: string,
    conflictId: string,
    resolution: Resolution,
    value?: Parameters<SyncApi["resolveConflict"]>[0]["value"],
  ): Promise<void> {
    await this.api.resolveConflict({ teamId, requestId: newId(), conflictId, resolution, value });
    await this.refreshConflicts(teamId);
    this.requestSync(true);
  }

  /** Removes a rejected operation from the list after the mechanic has seen it. */
  async dismissFailed(opId: string): Promise<void> {
    await this.db.transaction([
      { sql: "DELETE FROM sync_outbox WHERE op_id = $op AND state = 'rejected'", params: { $op: opId } },
    ]);
    await this.refreshCounts();
  }

  async refreshCounts(): Promise<void> {
    const rows = await this.db.query<{ state: string; n: number }>(
      "SELECT state, count(*) AS n FROM sync_outbox GROUP BY state",
    );
    const count = (state: string) => rows.find((r) => r.state === state)?.n ?? 0;
    const conflicts = await this.db.query<{ n: number }>("SELECT count(*) AS n FROM sync_conflict");
    this.set({
      pending: count("pending"),
      failed: count("rejected"),
      conflicts: conflicts[0]?.n ?? 0,
    });
  }
}

class ContractError extends Error {}

function parsePass(raw: string | null): { token: string; expiresAt: string } | null {
  if (!raw) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (
      typeof value === "object" && value !== null &&
      "token" in value && typeof value.token === "string" &&
      "expiresAt" in value && typeof value.expiresAt === "string"
    ) {
      return { token: value.token, expiresAt: value.expiresAt };
    }
  } catch {
    // Treated as no pass: a new one is requested.
  }
  return null;
}

function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}
