// @vitest-environment node

import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { CarInput } from "@/features/garage/domain/models";
import { LocalGarageRepository } from "@/features/garage/data/local-garage-repository";
import { LocalSetupRepository } from "@/features/setups/data/local-setup-repository";
import type { SqlParams, SqlStatement } from "@/db/protocol";
import type { SyncDatabase } from "./database";
import { openTestDatabase, type NodeSqliteExecutor } from "@/test/sqlite";
import { teamBootstrapStatements } from "@/sync/team-statements";

import { MemoryKeyStore } from "./device-key";
import { SyncEngine } from "./engine";
import { signingMessage } from "./signing";
import type {
  PullPage,
  PushOp,
  PushResult,
  RemoteConflict,
  SyncApi,
} from "./sync-api";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d99";

const team = { id: teamId, name: "Alpha", category: "1/8 TT nitro", countryCode: "PT", role: "owner" } as const;

function carInput(overrides: Partial<CarInput> = {}): CarInput {
  return {
    make: "Mugen",
    model: "MBX8",
    modelYear: 2024,
    serialNumber: null,
    nickname: "A",
    transponderNumber: null,
    status: "active",
    notes: null,
    ...overrides,
  };
}

/** A tiny server: verifies signatures, is idempotent on op_id and answers pulls from a list. */
/** What connect-es throws when fetch itself fails: no answer at all. */
const lostConnection = () =>
  new ConnectError("Failed to fetch", Code.Unknown, undefined, undefined, new TypeError("Failed to fetch"));

class FakeServer implements SyncApi {
  keys = new Map<string, CryptoKey>();
  registered = 0;
  seen = new Map<string, PushResult>();
  received: PushOp[] = [];
  version = new Map<string, number>();
  changes: PullPage["changes"] = [];
  conflicts: RemoteConflict[] = [];
  nextStatus: PushResult["status"] = "applied";
  nextReason: string | null = null;
  failPushAfterApply = 0;
  failWith: ConnectError | null = null;
  pulls = 0;

  async registerDevice(input: Parameters<SyncApi["registerDevice"]>[0]) {
    if (this.failWith) throw this.failWith;
    this.registered += 1;
    const deviceId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d11";
    this.keys.set(
      deviceId,
      await crypto.subtle.importKey("spki", input.publicKey as BufferSource, { name: "ECDSA", namedCurve: "P-256" }, false, ["verify"]),
    );
    return { deviceId };
  }

  issueOfflinePass() {
    if (this.failWith) return Promise.reject(this.failWith);
    return Promise.resolve({ token: "pass", expiresAt: new Date(Date.now() + 72 * 3_600_000) });
  }

  async push(input: Parameters<SyncApi["push"]>[0]): Promise<PushResult[]> {
    if (this.failWith) throw this.failWith;
    const key = this.keys.get(input.deviceId);
    const results: PushResult[] = [];
    for (const op of input.operations) {
      const stored = this.seen.get(op.opId);
      if (stored) {
        results.push(stored);
        continue;
      }
      const message = await signingMessage(input.teamId, input.deviceId, op);
      const ok = key
        ? await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, key, op.signature as BufferSource, message as BufferSource)
        : false;
      let result: PushResult;
      if (!ok) {
        result = { opId: op.opId, status: "rejected", reason: "bad_signature", version: null };
      } else if (this.nextReason) {
        result = { opId: op.opId, status: "rejected", reason: this.nextReason, version: null };
      } else {
        const version = (this.version.get(op.entityId) ?? 0) + 1;
        this.version.set(op.entityId, version);
        this.received.push(op);
        if (this.nextStatus === "conflict") {
          this.conflicts.push({
            id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f77", opId: op.opId, entity: op.entity,
            entityId: op.entityId, field: "nickname", serverValue: "S", clientValue: "C",
            createdAt: "2026-10-30T12:00:00.000Z",
          });
        }
        result = { opId: op.opId, status: this.nextStatus, reason: null, version };
      }
      if (!this.nextReason) this.seen.set(op.opId, result);
      results.push(result);
    }
    if (this.failPushAfterApply > 0) {
      this.failPushAfterApply -= 1;
      throw lostConnection();
    }
    return results;
  }

  pull(input: Parameters<SyncApi["pull"]>[0]): Promise<PullPage> {
    if (this.failWith) return Promise.reject(this.failWith);
    this.pulls += 1;
    const from = input.cursor === "" ? 0 : Number(input.cursor);
    const page = this.changes.slice(from, from + input.limit);
    const next = from + page.length;
    return Promise.resolve({
      changes: page,
      nextCursor: String(next),
      hasMore: next < this.changes.length,
      catalogVersion: "c1",
      catalogSnapshot: input.catalogVersion === "c1" ? null : {
        scales: [{ code: "1/8", name: "1/8", active: true, sort_order: 1 }],
        powertrains: [{ code: "nitro", name: "Nitro", family: "ice", active: true, sort_order: 1 }],
      },
    });
  }

  conflictCalls = 0;
  /** The real server only lists conflicts for the owner and the mechanic. */
  conflictsDenied = false;

  listConflicts() {
    this.conflictCalls++;
    if (this.conflictsDenied) {
      return Promise.reject(new ConnectError("mechanic role", Code.PermissionDenied));
    }
    return Promise.resolve(this.conflicts);
  }

  resolveConflict() {
    this.conflicts = [];
    return Promise.resolve();
  }
}

/** The real worker runs one request at a time; the test executor does not, so do the same. */
function serial(inner: NodeSqliteExecutor): SyncDatabase {
  let tail: Promise<unknown> = Promise.resolve();
  const run = <T,>(fn: () => Promise<T>): Promise<T> => {
    const next = tail.then(fn, fn);
    tail = next.catch(() => undefined);
    return next;
  };
  return {
    query: <T,>(sql: string, params?: SqlParams) => run(() => inner.query<T>(sql, params)),
    transaction: <T,>(statements: SqlStatement[], options?: { requireRows?: boolean }) =>
      run(() => inner.transaction<T>(statements, options)),
  };
}

let db: NodeSqliteExecutor;
let server: FakeServer;
let engine: SyncEngine;
let cars: LocalGarageRepository;

beforeEach(async () => {
  db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team]));
  server = new FakeServer();
  cars = new LocalGarageRepository(db);
  engine = new SyncEngine({
    db: serial(db),
    api: server,
    userId,
    userHash: "hash",
    keyStore: new MemoryKeyStore(),
    pollMs: 2_000,
    random: () => 0.5,
  });
  engine.start();
  await engine.syncNow();
  server.received = [];
});

afterEach(async () => {
  vi.useRealTimers();
  await engine.close();
  await db.close();
});

const engine_sync = () => engine.syncNow();

const pending = async () =>
  (await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox"))[0]?.n ?? 0;

describe("outbox", () => {
  it("queues the operation in the same transaction as the write", async () => {
    const car = await cars.createCar(teamId, carInput());
    const rows = await db.query<{ entity: string; entity_id: string; kind: number; base_version: number | null; fields_json: string }>(
      "SELECT entity, entity_id, kind, base_version, fields_json FROM sync_outbox",
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ entity: "garage.car", entity_id: car.id, kind: 1, base_version: null });
    expect(JSON.parse(rows[0]?.fields_json ?? "{}")).toMatchObject({ make: "Mugen", scale: "1/8" });
  });

  it("leaves nothing in the queue when the write fails", async () => {
    const car = await cars.createCar(teamId, carInput());
    await db.transaction([{ sql: "DELETE FROM sync_outbox" }]);
    await expect(cars.updateCar(teamId, car.id, car.localRev + 5, carInput({ nickname: "B" }))).rejects.toThrow();
    expect(await pending()).toBe(0);
  });

  it("sends only the fields that changed", async () => {
    const car = await cars.createCar(teamId, carInput());
    await db.transaction([{ sql: "DELETE FROM sync_outbox" }]);
    await cars.updateCar(teamId, car.id, car.localRev, carInput({ nickname: "B", notes: "x" }));
    const row = (await db.query<{ fields_json: string; base_version: number }>("SELECT fields_json, base_version FROM sync_outbox"))[0];
    expect(JSON.parse(row?.fields_json ?? "{}")).toEqual({ nickname: "B", notes: "x" });
    expect(row?.base_version).toBe(1);
  });

  it("queues nothing when an edit changes nothing", async () => {
    const car = await cars.createCar(teamId, carInput());
    await db.transaction([{ sql: "DELETE FROM sync_outbox" }]);
    await cars.updateCar(teamId, car.id, car.localRev, carInput());
    expect(await pending()).toBe(0);
  });

  it("queues a setup and its version with the base version", async () => {
    const car = await cars.createCar(teamId, carInput());
    const setups = new LocalSetupRepository(db);
    const { setup } = await setups.createSetup(teamId, { carId: car.id, name: "Seco", notes: null, source: null, createdBy: userId });
    const rows = await db.query<{ entity: string; kind: number }>("SELECT entity, kind FROM sync_outbox ORDER BY seq");
    expect(rows.map((r) => r.entity)).toEqual(["garage.car", "setup.setup"]);
    expect(setup.currentVersion).toBe(1);
  });
});

describe("engines, tyre sets and parts", () => {
  const engine = { make: "REDS", model: "R7", serialNumber: null, purchasedOn: null, status: "active", notes: null } as const;
  const tyre = { label: "J2", make: "Pro-Line", model: "Badlands", compound: null, insertType: null, gluedOn: null, status: "new", wearNotes: null } as const;
  const part = {
    kind: "glow_plug", brand: null, reference: null, description: "Vela", unit: "un", specs: {},
    minStockCenti: 200, reorderToCenti: null, hasServiceLife: true, initialQtyCenti: 0, unitCostCents: 950,
  } as const;

  it("sends creates and edits with only the changed fields, and clears the queue", async () => {
    const e = await cars.createEngine(teamId, engine);
    await cars.updateEngine(teamId, e.id, e.localRev, { ...engine, notes: "rodado" });
    const t = await cars.createTyreSet(teamId, tyre);
    await cars.updateTyreSet(teamId, t.id, t.localRev, { ...tyre, status: "used" });
    const p = await cars.createPart(teamId, part);
    await cars.updatePart(teamId, p.id, p.localRev, { ...part, minStockCenti: 300 });
    await engine_sync();
    const byEntity = (name: string) => server.received.filter((op) => op.entity === name);
    const text = (op: PushOp | undefined) => new TextDecoder().decode(op?.fieldsJson);
    expect(byEntity("garage.engine").map((op) => op.kind)).toEqual([1, 2]);
    expect(JSON.parse(text(byEntity("garage.engine")[1]))).toEqual({ notes: "rodado" });
    expect(JSON.parse(text(byEntity("garage.tyre_set")[1]))).toEqual({ status: "used" });
    expect(JSON.parse(text(byEntity("garage.part")[0]))).toMatchObject({ kind: "glow_plug", unit_cost_cents: 950, has_service_life: true });
    expect(JSON.parse(text(byEntity("garage.part")[1]))).toEqual({ min_stock_centi: 300 });
    expect(await pending()).toBe(0);
  });

  it("archiving a part queues a delete based on its version", async () => {
    const p = await cars.createPart(teamId, part);
    await engine_sync();
    server.received = [];
    await cars.archivePart(teamId, p.id, p.localRev);
    await engine_sync();
    expect(server.received.map((op) => [op.entity, op.kind, op.baseVersion])).toEqual([["garage.part", 4, 1]]);
  });

  it("pulls them in, hides nothing it did not receive and applies a tombstone", async () => {
    const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e55";
    const partChange = (cost?: number) => ({
      entity: "garage.part", entityId: id, deleted: false,
      data: {
        id, kind: "glow_plug", brand: null, reference: null, description: "Vela remota", unit: "un", specs: {},
        min_stock_centi: 100, currency: "EUR", has_service_life: false, reorder_to_centi: null,
        created_at: "2026-10-30T12:00:00Z", version: 2, ...(cost === undefined ? {} : { unit_cost_cents: cost }),
      },
    });
    server.changes = [partChange(700)];
    await engine_sync();
    const read = async () => (await db.query<{ unit_cost_cents: number | null; description: string; version: number }>(
      "SELECT unit_cost_cents, description, version FROM part WHERE id = $id", { $id: id }))[0];
    expect(await read()).toMatchObject({ unit_cost_cents: 700, description: "Vela remota", version: 2 });

    // A pilot-shaped payload has no price: the local one is not erased.
    server.changes.push(partChange());
    await engine_sync();
    expect((await read())?.unit_cost_cents).toBe(700);

    server.changes.push({ entity: "garage.part", entityId: id, deleted: true, data: null });
    await engine_sync();
    expect(await cars.getPart(teamId, id)).toBeUndefined();
  });
});

describe("push", () => {
  it("registers once, signs with the device key and clears the queue", async () => {
    await cars.createCar(teamId, carInput());
    await engine.syncNow();
    expect(server.registered).toBe(1);
    expect(server.received).toHaveLength(1);
    expect(server.received[0]?.entity).toBe("garage.car");
    expect(await pending()).toBe(0);
    await cars.createCar(teamId, carInput({ nickname: "C" }));
    await engine.syncNow();
    expect(server.registered).toBe(1);
  });

  it("resending after a lost response does not duplicate", async () => {
    await cars.createCar(teamId, carInput());
    server.failPushAfterApply = 1;
    await engine.syncNow();
    expect(await pending()).toBe(1);
    expect(engine.getSnapshot().phase).toBe("offline");
    await engine.syncNow();
    expect(await pending()).toBe(0);
    expect(server.received).toHaveLength(1);
  });

  it("rebases the next edit onto the version our own operation produced", async () => {
    const car = await cars.createCar(teamId, carInput());
    const second = await cars.updateCar(teamId, car.id, car.localRev, carInput({ nickname: "B" }));
    await cars.updateCar(teamId, car.id, second.localRev, carInput({ nickname: "C" }));
    await engine.syncNow();
    expect(server.received.map((op) => op.baseVersion)).toEqual([null, 1, 2]);
    expect(await pending()).toBe(0);
  });

  it("keeps a conflicting operation visible and does not retry it", async () => {
    await cars.createCar(teamId, carInput());
    server.nextStatus = "conflict";
    await engine.syncNow();
    const rows = await db.query<{ state: string }>("SELECT state FROM sync_outbox");
    expect(rows[0]?.state).toBe("conflict");
    server.received = [];
    await engine.syncNow();
    expect(server.received).toHaveLength(0);
  });

  it("keeps a rejected operation and counts it", async () => {
    await cars.createCar(teamId, carInput());
    server.nextReason = "invalid_fields";
    await engine.syncNow();
    expect(engine.getSnapshot().failed).toBe(1);
    await engine.dismissFailed((await db.query<{ op_id: string }>("SELECT op_id FROM sync_outbox"))[0]?.op_id ?? "");
    expect(engine.getSnapshot().failed).toBe(0);
  });

  it("stops on a revoked device instead of retrying", async () => {
    await cars.createCar(teamId, carInput());
    server.nextReason = "device_revoked";
    await engine.syncNow();
    expect(engine.getSnapshot()).toMatchObject({ phase: "blocked", problem: "device_revoked", pending: 1 });
    const before = server.registered;
    await engine.syncNow();
    expect(server.registered).toBe(before);
  });
});

describe("pull", () => {
  const carChange = (id: string, nickname: string) => ({
    entity: "garage.car",
    entityId: id,
    deleted: false,
    data: {
      id, make: "Mugen", model: "MBX8", model_year: 2024, serial_number: null, nickname,
      transponder_number: null, status: "active", scale: "1/8", powertrain: "nitro", notes: null,
      created_at: "2026-10-30T12:00:00.123456Z", version: 4,
    },
  });
  const otherCar = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e01";

  it("applies changes and a tombstone, and keeps the cursor", async () => {
    server.changes = [carChange(otherCar, "Remoto")];
    await engine.syncNow();
    expect((await cars.getCar(teamId, otherCar))?.nickname).toBe("Remoto");
    expect((await cars.getCar(teamId, otherCar))?.version).toBe(4);
    server.changes.push({ entity: "garage.car", entityId: otherCar, deleted: true, data: null });
    await engine.syncNow();
    expect(await cars.getCar(teamId, otherCar)).toBeUndefined();
    expect((await db.query<{ value: string }>("SELECT value FROM app_meta WHERE key = $k", { $k: `sync.cursor.${teamId}` }))[0]?.value).toBe("2");
  });

  it("never overwrites an entity with pending operations, and applies it afterwards", async () => {
    const car = await cars.createCar(teamId, carInput());
    server.failWith = null;
    server.changes = [carChange(car.id, "Do servidor")];
    // Make the push fail so the operation stays pending while the pull page arrives.
    server.failPushAfterApply = 1;
    await engine.syncNow();
    expect((await cars.getCar(teamId, car.id))?.nickname).toBe("A");
    // Next cycle: the push goes through, the held change is applied.
    await engine.syncNow();
    expect((await cars.getCar(teamId, car.id))?.nickname).toBe("Do servidor");
    expect(await db.query("SELECT 1 FROM sync_held")).toHaveLength(0);
  });

  it("rejects a payload that does not match the contract", async () => {
    server.changes = [{ ...carChange(otherCar, "x"), data: { id: otherCar, make: 1 } }];
    await engine.syncNow();
    expect(engine.getSnapshot().phase).toBe("error");
    expect(await cars.getCar(teamId, otherCar)).toBeUndefined();
  });

  it("stores the open conflicts for the screen", async () => {
    server.conflicts = [{
      id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f01", opId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f02",
      entity: "garage.car", entityId: otherCar, field: "nickname", serverValue: "S", clientValue: "C",
      createdAt: "2026-10-30T12:00:00.000Z",
    }];
    await engine.syncNow();
    expect(engine.getSnapshot().conflicts).toBe(1);
    await engine.resolveConflict(teamId, server.conflicts[0]?.id ?? "", "server");
    expect(engine.getSnapshot().conflicts).toBe(0);
  });

  it("does not ask a pilot for conflicts: only the mechanic can resolve them", async () => {
    await db.query("UPDATE team SET role = 'pilot' WHERE id = $id", { $id: teamId });
    server.conflictsDenied = true;
    server.conflictCalls = 0;
    await engine.syncNow();
    expect(server.conflictCalls).toBe(0);
    expect(engine.getSnapshot().phase).toBe("idle");
    expect(engine.getSnapshot().problem).toBeNull();
  });
});

describe("scheduling", () => {
  it("backs off on failures with jitter and resets after a success", async () => {
    vi.useFakeTimers();
    const spy = vi.spyOn(globalThis, "setTimeout");
    server.failWith = lostConnection();
    await engine.syncNow();
    expect(engine.getSnapshot()).toMatchObject({ phase: "offline", problem: "network" });
    const delays = () => spy.mock.calls.map((c) => c[1]).filter((d): d is number => typeof d === "number");
    const first = delays().at(-1);
    await engine.syncNow();
    const second = delays().at(-1);
    expect(second).toBeGreaterThan(first ?? 0);
    server.failWith = null;
    await engine.syncNow();
    expect(engine.getSnapshot().phase).toBe("idle");
  });

  it("calls a missing service a server problem, not a lost connection", async () => {
    server.failWith = new ConnectError("not found", Code.Unimplemented);
    await engine.syncNow();
    expect(engine.getSnapshot()).toMatchObject({ phase: "error", problem: "service" });
  });

  it("calls a failed fetch a lost connection", async () => {
    server.failWith = new ConnectError("Failed to fetch", Code.Unknown, undefined, undefined, new TypeError("Failed to fetch"));
    await engine.syncNow();
    expect(engine.getSnapshot()).toMatchObject({ phase: "offline", problem: "network" });
  });

  it("stops and asks for a login on a 401", async () => {
    const onUnauthenticated = vi.fn();
    await engine.close();
    server.failWith = new ConnectError("no", Code.Unauthenticated);
    engine = new SyncEngine({
      db: serial(db), api: server, userId, userHash: "hash", keyStore: new MemoryKeyStore(),
      pollMs: 1_000_000, onUnauthenticated,
    });
    engine.start();
    await engine.syncNow();
    expect(engine.getSnapshot().phase).toBe("auth");
    expect(onUnauthenticated).toHaveBeenCalled();
  });

  it("runs one cycle at a time", async () => {
    await cars.createCar(teamId, carInput());
    const before = server.pulls;
    await Promise.all([engine.syncNow(), engine.syncNow(), engine.syncNow()]);
    // One running cycle plus one follow-up for the requests that arrived meanwhile.
    expect(server.pulls - before).toBeLessThanOrEqual(2);
  });
});
