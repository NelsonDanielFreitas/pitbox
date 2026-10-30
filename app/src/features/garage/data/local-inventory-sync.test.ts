// @vitest-environment node

import { describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { syncEntities } from "@/sync/entities";
import { OpKind } from "@/sync/signing";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { openTestDatabase } from "@/test/sqlite";

import type { PartCreateInput, StockMovementInput } from "../domain/models";
import { LocalGarageRepository } from "./local-garage-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };
const NOW = "2026-10-26T12:00:00.000Z";

const part = (description: string, qty: number, over: Partial<PartCreateInput> = {}): PartCreateInput => ({
  kind: "bearing",
  brand: null,
  reference: null,
  description,
  unit: "un",
  specs: {},
  minStockCenti: 300,
  reorderToCenti: null,
  initialQtyCenti: qty,
  unitCostCents: 250,
  hasServiceLife: false,
  createdBy: userId,
  ...over,
});

const movement = (partId: string, over: Partial<StockMovementInput>): StockMovementInput => ({
  partId,
  reason: "consumed",
  qtyDeltaCenti: -100,
  lossReason: null,
  unitCostCents: null,
  store: null,
  occurredOn: "2026-10-26",
  note: null,
  createdBy: userId,
  ...over,
});

interface QueuedOp {
  entity: string;
  entity_id: string;
  kind: number;
  base_version: number | null;
  fields_json: string;
}

/** `advancing` makes every reading of the clock one second later (a mount cannot close at its own instant). */
async function setup(advancing = false) {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team]));
  let ticks = 0;
  const clock = () => (advancing ? new Date(Date.parse(NOW) + ++ticks * 1000).toISOString() : NOW);
  const repository = new LocalGarageRepository(db, clock);
  const ops = async (entity?: string) =>
    (
      await db.query<QueuedOp>(
        `SELECT entity, entity_id, kind, base_version, fields_json FROM sync_outbox
          ${entity ? "WHERE entity = $entity" : ""} ORDER BY rowid`,
        entity ? { $entity: entity } : {},
      )
    ).map((o) => ({ ...o, fields: JSON.parse(o.fields_json) as Record<string, unknown> }));
  return { db, repository, ops };
}

describe("stock movements are queued", () => {
  it("queues a part and its initial purchase, in that order", async () => {
    const { db, repository, ops } = await setup();
    try {
      const created = await repository.createPart(teamId, part("Rolamento", 500));
      const queued = await ops();
      expect(queued.map((o) => [o.entity, o.kind])).toEqual([
        ["garage.part", OpKind.create],
        ["garage.stock_movement", OpKind.append],
      ]);
      expect(queued[1]).toMatchObject({ base_version: null });
      expect(queued[1]?.fields).toEqual({
        part_id: created.id,
        qty_delta_centi: 500,
        reason: "purchase",
        note: null,
        occurred_at: NOW,
        unit_cost_cents: 250,
        loss_reason: null,
      });
    } finally {
      await db.close();
    }
  });

  it("queues no movement for a part created without stock", async () => {
    const { db, repository, ops } = await setup();
    try {
      await repository.createPart(teamId, part("Vazio", 0));
      expect((await ops("garage.stock_movement")).length).toBe(0);
    } finally {
      await db.close();
    }
  });

  it("folds the store into the note and keeps the loss reason", async () => {
    const { db, repository, ops } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Vela", 1000));
      await repository.addStockMovement(
        teamId,
        movement(p.id, { reason: "purchase", qtyDeltaCenti: 200, unitCostCents: 400, store: "Hobby", note: "Boa" }),
      );
      await repository.addStockMovement(teamId, movement(p.id, { reason: "lost", qtyDeltaCenti: -100, lossReason: "broken" }));
      const queued = await ops("garage.stock_movement");
      expect(queued.map((o) => o.fields["note"])).toEqual([null, "Loja: Hobby · Boa", null]);
      expect(queued[2]?.fields).toMatchObject({ reason: "lost", loss_reason: "broken", qty_delta_centi: -100 });
      expect(new Set(queued.map((o) => o.entity_id)).size).toBe(3);
    } finally {
      await db.close();
    }
  });

  it("queues an undo as a plain adjustment, without the link to the undone movement", async () => {
    const { db, repository, ops } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Filtro", 1000));
      const done = await repository.addStockMovement(teamId, movement(p.id, { qtyDeltaCenti: -300 }));
      await repository.undoMovement(teamId, done.movementId, { occurredOn: "2026-10-26", note: "Desfeito", createdBy: userId });
      const last = (await ops("garage.stock_movement")).at(-1);
      expect(last?.fields).toMatchObject({ reason: "adjustment", qty_delta_centi: 300, note: "Desfeito" });
      expect(last?.fields).not.toHaveProperty("reverts_movement_id");
    } finally {
      await db.close();
    }
  });

  it("queues one adjustment per counted line that differs, none for the others", async () => {
    const { db, repository, ops } = await setup();
    try {
      const a = await repository.createPart(teamId, part("A", 500));
      const b = await repository.createPart(teamId, part("B", 500));
      await repository.startCount(teamId, null);
      await repository.setCountLine(teamId, a.id, 450);
      await repository.setCountLine(teamId, b.id, 500);
      await repository.applyCount(teamId, { occurredOn: "2026-10-26", note: "Contagem", createdBy: userId });
      const adjustments = (await ops("garage.stock_movement")).filter((o) => o.fields["reason"] === "adjustment");
      expect(adjustments).toHaveLength(1);
      expect(adjustments[0]?.fields).toMatchObject({ part_id: a.id, qty_delta_centi: -50 });
    } finally {
      await db.close();
    }
  });

  it("queues nothing, and writes nothing, for an archived part", async () => {
    const { db, repository, ops } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Velho", 500));
      await repository.archivePart(teamId, p.id, p.localRev);
      const before = (await ops()).length;
      await expect(repository.addStockMovement(teamId, movement(p.id, {}))).rejects.toThrow();
      expect((await ops()).length).toBe(before);
    } finally {
      await db.close();
    }
  });
});

describe("shopping lines are queued", () => {
  it("creates, adjusts and deletes a manual line", async () => {
    const { db, repository, ops } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Ok", 1000));
      await repository.setShoppingItem(teamId, p.id, 400);
      let lines = await ops("garage.shopping_item");
      expect(lines).toHaveLength(1);
      expect(lines[0]).toMatchObject({ kind: OpKind.create, base_version: null });
      expect(lines[0]?.fields).toEqual({ part_id: p.id, qty_centi: 400, dismissed: false });
      const lineId = lines[0]?.entity_id;

      await repository.setShoppingItem(teamId, p.id, 600);
      await repository.setShoppingItem(teamId, p.id, 600); // no change: nothing to send
      lines = await ops("garage.shopping_item");
      expect(lines).toHaveLength(2);
      expect(lines[1]).toMatchObject({ kind: OpKind.patch, base_version: 1, entity_id: lineId });
      expect(lines[1]?.fields).toEqual({ qty_centi: 600 });

      await repository.removeShoppingItem(teamId, p.id);
      lines = await ops("garage.shopping_item");
      expect(lines).toHaveLength(3);
      expect(lines[2]).toMatchObject({ kind: OpKind.delete, base_version: 1, entity_id: lineId });
    } finally {
      await db.close();
    }
  });

  it("hides an automatic line by creating a dismissed one", async () => {
    const { db, repository, ops } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Baixo", 100));
      await repository.removeShoppingItem(teamId, p.id);
      const lines = await ops("garage.shopping_item");
      expect(lines).toHaveLength(1);
      expect(lines[0]).toMatchObject({ kind: OpKind.create });
      expect(lines[0]?.fields).toEqual({ part_id: p.id, dismissed: true });
    } finally {
      await db.close();
    }
  });

  it("refuses a line without a positive quantity and queues nothing", async () => {
    const { db, repository, ops } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Ok", 1000));
      await expect(repository.setShoppingItem(teamId, p.id, null)).rejects.toThrow();
      expect(await ops("garage.shopping_item")).toHaveLength(0);
    } finally {
      await db.close();
    }
  });
});

describe("pulling stock", () => {
  it("adds a movement from another device once, and leaves our own alone", async () => {
    const { db, repository } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Ok", 500));
      const entity = syncEntities["garage.stock_movement"];
      if (!entity) throw new Error("stock movement entity missing");
      const wire = {
        part_id: p.id,
        qty_delta_centi: -200,
        reason: "consumed",
        note: null,
        occurred_at: NOW,
        loss_reason: null,
      };
      const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f77";
      await db.transaction(entity.upsert(teamId, id, wire, NOW));
      await db.transaction(entity.upsert(teamId, id, { ...wire, qty_delta_centi: -999 }, NOW));
      const stored = await repository.getPart(teamId, p.id, { costs: true });
      expect(stored?.qtyCenti).toBe(300);
      // A movement of a part this device does not have is skipped, not forced in.
      await db.transaction(
        entity.upsert(teamId, "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f78", { ...wire, part_id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f99" }, NOW),
      );
      expect((await db.query("SELECT 1 FROM stock_movement WHERE id LIKE '%4f78'")).length).toBe(0);
    } finally {
      await db.close();
    }
  });

  it("keeps one line per part: the server's replaces a local one with another id", async () => {
    const { db, repository } = await setup();
    try {
      const p = await repository.createPart(teamId, part("Ok", 1000));
      await repository.setShoppingItem(teamId, p.id, 400);
      const entity = syncEntities["garage.shopping_item"];
      if (!entity) throw new Error("shopping entity missing");
      const serverId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a77";
      await db.transaction(
        entity.upsert(teamId, serverId, { part_id: p.id, qty_centi: 900, dismissed: false, note: null, version: 3 }, NOW),
      );
      const rows = await db.query<{ id: string; qty_centi: number; version: number }>(
        "SELECT id, qty_centi, version FROM shopping_item WHERE part_id = $p",
        { $p: p.id },
      );
      expect(rows).toEqual([{ id: serverId, qty_centi: 900, version: 3 }]);
      await db.transaction(entity.remove(teamId, serverId, null, NOW));
      expect(await db.query("SELECT 1 FROM shopping_item")).toHaveLength(0);
    } finally {
      await db.close();
    }
  });
});

const carInput = (make: string) => ({
  make,
  model: "X",
  modelYear: null,
  serialNumber: null,
  nickname: null,
  transponderNumber: null,
  status: "active" as const,
  scale: "1/8",
  powertrain: "nitro",
  notes: null,
});
const engineInput = (make: string) => ({
  make,
  model: "R7",
  serialNumber: null,
  purchasedOn: null,
  status: "active" as const,
  notes: null,
});

describe("mounts are queued", () => {
  it("queues a mount, a move (close then create) and an unmount, with the ids the server will see", async () => {
    const { db, repository, ops } = await setup(true);
    try {
      const car1 = await repository.createCar(teamId, carInput("A"));
      const car2 = await repository.createCar(teamId, carInput("B"));
      const engine = await repository.createEngine(teamId, engineInput("REDS"));
      await repository.mountEngine(teamId, engine.id, car1.id);
      await repository.moveEngine(teamId, engine.id, car2.id);
      await repository.unmountEngine(teamId, engine.id);

      const queued = await ops("garage.engine_mount");
      expect(queued.map((o) => o.kind)).toEqual([OpKind.create, OpKind.patch, OpKind.create, OpKind.patch]);
      expect(queued[0]?.fields).toMatchObject({ engine_id: engine.id, car_id: car1.id });
      expect(queued[1]).toMatchObject({ base_version: 1, entity_id: queued[0]?.entity_id });
      expect(queued[1]?.fields["unmounted_at"]).toBe(queued[2]?.fields["mounted_at"]);
      expect(String(queued[1]?.fields["unmounted_at"]) > String(queued[0]?.fields["mounted_at"])).toBe(true);
      expect(queued[2]?.fields).toMatchObject({ car_id: car2.id });
      expect(queued[3]).toMatchObject({ entity_id: queued[2]?.entity_id });
      const open = await db.query("SELECT 1 FROM engine_mount WHERE unmounted_at IS NULL");
      expect(open).toHaveLength(0);
    } finally {
      await db.close();
    }
  });

  it("queues the close of the old engine and the mount of the new one when swapping", async () => {
    const { db, repository, ops } = await setup(true);
    try {
      const car = await repository.createCar(teamId, carInput("A"));
      const first = await repository.createEngine(teamId, engineInput("One"));
      const second = await repository.createEngine(teamId, engineInput("Two"));
      await repository.mountEngine(teamId, first.id, car.id);
      await repository.swapCarEngine(teamId, car.id, second.id);
      const queued = await ops("garage.engine_mount");
      expect(queued.map((o) => o.kind)).toEqual([OpKind.create, OpKind.patch, OpKind.create]);
      expect(queued[2]?.fields).toMatchObject({ engine_id: second.id, car_id: car.id });
    } finally {
      await db.close();
    }
  });

  it("queues nothing when the mount is refused", async () => {
    const { db, repository, ops } = await setup(true);
    try {
      const car = await repository.createCar(teamId, carInput("A"));
      const other = await repository.createCar(teamId, carInput("B"));
      const engine = await repository.createEngine(teamId, engineInput("REDS"));
      await repository.mountEngine(teamId, engine.id, car.id);
      const before = (await ops("garage.engine_mount")).length;
      await expect(repository.mountEngine(teamId, engine.id, other.id)).rejects.toThrow();
      await expect(repository.unmountEngine(teamId, "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4999")).rejects.toThrow();
      expect((await ops("garage.engine_mount")).length).toBe(before);
    } finally {
      await db.close();
    }
  });
});

describe("installs are queued", () => {
  it("queues the install and its removal, and the stock movement on its own", async () => {
    const { db, repository, ops } = await setup(true);
    try {
      const car = await repository.createCar(teamId, carInput("A"));
      const plug = await repository.createPart(teamId, part("Vela", 500, { hasServiceLife: true, kind: "glow_plug" }));
      const change = await repository.installPart(teamId, {
        partId: plug.id,
        target: { carId: car.id },
        occurredOn: "2026-10-26",
        note: "nova",
        createdBy: userId,
      });
      const installs = await repository.listPartInstalls(teamId, { carId: car.id }, false);
      const install = installs[0];
      if (!install) throw new Error("install missing");
      await repository.removePart(teamId, install.id, {
        destination: "scrap",
        reason: "worn",
        occurredOn: "2026-10-26",
        note: null,
        createdBy: userId,
      });

      const queued = await ops("garage.part_install");
      expect(queued.map((o) => o.kind)).toEqual([OpKind.create, OpKind.patch]);
      expect(queued[0]?.fields).toMatchObject({ part_id: plug.id, car_id: car.id, engine_id: null });
      expect(String(queued[0]?.fields["installed_at"])).toMatch(/^2026-10-26T12:00:0\d\.000Z$/);
      expect(queued[1]).toMatchObject({ base_version: 1, entity_id: install.id });
      expect(queued[1]?.fields).toMatchObject({ removal_destination: "scrap", removal_reason: "worn" });
      expect(String(queued[1]?.fields["removed_at"]) > String(queued[0]?.fields["installed_at"])).toBe(true);
      // The unit leaves the stock with a movement of its own.
      const movements = await ops("garage.stock_movement");
      expect(movements.at(-1)?.fields).toMatchObject({ reason: "installed", qty_delta_centi: -100 });
      expect(change.movementId).toBe(movements.at(-1)?.entity_id);
    } finally {
      await db.close();
    }
  });

  it("queues no close for a removal that does nothing", async () => {
    const { db, repository, ops } = await setup(true);
    try {
      const car = await repository.createCar(teamId, carInput("A"));
      const plug = await repository.createPart(teamId, part("Vela", 500, { hasServiceLife: true, kind: "glow_plug" }));
      await repository.installPart(teamId, {
        partId: plug.id,
        target: { carId: car.id },
        occurredOn: "2026-10-26",
        note: null,
        createdBy: userId,
      });
      const [install] = await repository.listPartInstalls(teamId, { carId: car.id }, false);
      if (!install) throw new Error("install missing");
      const removal = {
        destination: "stock" as const,
        reason: null,
        occurredOn: "2026-10-26",
        note: null,
        createdBy: userId,
      };
      await repository.removePart(teamId, install.id, removal);
      const before = (await ops()).length;
      await expect(repository.removePart(teamId, install.id, removal)).rejects.toThrow();
      expect((await ops()).length).toBe(before);
    } finally {
      await db.close();
    }
  });
});

describe("pulling mounts and installs", () => {
  it("applies a mount and its close, and lets the server's open mount replace a local ghost", async () => {
    const { db, repository } = await setup();
    try {
      const car = await repository.createCar(teamId, carInput("A"));
      const engine = await repository.createEngine(teamId, engineInput("REDS"));
      await repository.mountEngine(teamId, engine.id, car.id); // a local mount with its own id
      const entity = syncEntities["garage.engine_mount"];
      if (!entity) throw new Error("mount entity missing");
      const serverId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4b01";
      const wire = { engine_id: engine.id, car_id: car.id, mounted_at: "2026-10-25T10:00:00.000Z", unmounted_at: null };
      await db.transaction(entity.upsert(teamId, serverId, wire, NOW));
      let rows = await db.query<{ id: string; unmounted_at: string | null }>("SELECT id, unmounted_at FROM engine_mount");
      expect(rows).toEqual([{ id: serverId, unmounted_at: null }]);
      await db.transaction(
        entity.upsert(teamId, serverId, { ...wire, unmounted_at: "2026-10-26T09:00:00.000Z" }, NOW),
      );
      rows = await db.query("SELECT id, unmounted_at FROM engine_mount");
      expect(rows).toEqual([{ id: serverId, unmounted_at: "2026-10-26T09:00:00.000Z" }]);
    } finally {
      await db.close();
    }
  });

  it("applies an install and its removal, keeping the local note", async () => {
    const { db, repository } = await setup();
    try {
      const car = await repository.createCar(teamId, carInput("A"));
      const plug = await repository.createPart(teamId, part("Vela", 500, { hasServiceLife: true, kind: "glow_plug" }));
      const entity = syncEntities["garage.part_install"];
      if (!entity) throw new Error("install entity missing");
      const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4c01";
      const wire = {
        part_id: plug.id,
        car_id: car.id,
        engine_id: null,
        installed_at: "2026-10-25T10:00:00.000Z",
        removed_at: null,
        removal_destination: null,
        removal_reason: null,
        run_seconds: 0,
        sessions: 0,
        version: 1,
      };
      await db.transaction(entity.upsert(teamId, id, wire, NOW));
      await db.exec("UPDATE part_install SET note = 'minha' WHERE id = $id", { $id: id });
      await db.transaction(
        entity.upsert(
          teamId,
          id,
          { ...wire, removed_at: "2026-10-26T09:00:00.000Z", removal_destination: "scrap", removal_reason: "worn", version: 2 },
          NOW,
        ),
      );
      const [row] = await db.query<{ removal_destination: string; note: string; version: number }>(
        "SELECT removal_destination, note, version FROM part_install WHERE id = $id",
        { $id: id },
      );
      expect(row).toEqual({ removal_destination: "scrap", note: "minha", version: 2 });
      // An install whose car this device does not have is skipped, not forced in.
      await db.transaction(
        entity.upsert(teamId, "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4c02", { ...wire, car_id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4c99" }, NOW),
      );
      expect(await db.query("SELECT 1 FROM part_install WHERE id LIKE '%4c02'")).toHaveLength(0);
    } finally {
      await db.close();
    }
  });
});
