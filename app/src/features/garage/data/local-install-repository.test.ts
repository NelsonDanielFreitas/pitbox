// @vitest-environment node

import { describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { openTestDatabase } from "@/test/sqlite";

import type { CarInput, EngineInput, PartCreateInput } from "../domain/models";
import { LocalGarageRepository } from "./local-garage-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

const car: CarInput = {
  make: "Mugen",
  model: "MBX8",
  modelYear: 2024,
  serialNumber: null,
  nickname: "Carro A",
  transponderNumber: null,
  status: "active",
  notes: null,
};
const engine: EngineInput = {
  make: "Novarossi",
  model: "Plus 21",
  serialNumber: null,
  purchasedOn: null,
  status: "active",
  notes: null,
};
const plug = (over: Partial<PartCreateInput> = {}): PartCreateInput => ({
  kind: "glow_plug",
  brand: "OS",
  reference: "P3",
  description: "Vela OS P3",
  unit: "un",
  specs: {},
  minStockCenti: 200,
  reorderToCenti: null,
  initialQtyCenti: 300,
  hasServiceLife: true,
  createdBy: userId,
  ...over,
});
const when = { occurredOn: "2026-10-26", note: null, createdBy: userId };

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  const repository = new LocalGarageRepository(db, () => "2026-10-26T12:00:00.000Z");
  return { db, repository };
}

describe("mounting parts", () => {
  it("mounts one unit and writes the installed movement in the same transaction", async () => {
    const { db, repository } = await setup();
    try {
      const c = await repository.createCar(teamId, car);
      const p = await repository.createPart(teamId, plug());
      const change = await repository.installPart(teamId, { partId: p.id, target: { carId: c.id }, ...when });
      expect(change).toMatchObject({ beforeCenti: 300, afterCenti: 200, minStockCenti: 200 });

      const active = await repository.listPartInstalls(teamId, { carId: c.id });
      expect(active).toHaveLength(1);
      expect(active[0]).toMatchObject({ partId: p.id, targetName: "Carro A", removedAt: null });
      expect((await repository.listStockMovements(teamId, p.id))[0]).toMatchObject({
        reason: "installed",
        qtyDeltaCenti: -100,
        createdBy: userId,
      });
      // The same unit shows up from the part and from the car, but not from another car.
      expect(await repository.listPartInstalls(teamId, { partId: p.id })).toHaveLength(1);
    } finally {
      await db.close();
    }
  });

  it("refuses parts without service life, retired or missing targets, and other teams", async () => {
    const { db, repository } = await setup();
    try {
      const c = await repository.createCar(teamId, car);
      const retired = await repository.createEngine(teamId, { ...engine, status: "retired" });
      const plain = await repository.createPart(teamId, plug({ hasServiceLife: false }));
      const p = await repository.createPart(teamId, plug());

      const attempts = [
        { partId: plain.id, target: { carId: c.id } },
        { partId: p.id, target: { engineId: retired.id } },
        { partId: p.id, target: { carId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4eff" } },
      ];
      for (const attempt of attempts) {
        await expect(repository.installPart(teamId, { ...attempt, ...when })).rejects.toMatchObject({
          code: "not_serviceable",
        });
      }
      await expect(
        repository.installPart(otherTeamId, { partId: p.id, target: { carId: c.id }, ...when }),
      ).rejects.toMatchObject({ code: "not_serviceable" });
      // Nothing leaked: no install, stock untouched.
      expect(await repository.listPartInstalls(teamId, { partId: p.id }, true)).toHaveLength(0);
      expect((await repository.getPart(teamId, p.id))?.qtyCenti).toBe(300);
    } finally {
      await db.close();
    }
  });
});

describe("dismounting parts", () => {
  async function mounted() {
    const ctx = await setup();
    const e = await ctx.repository.createEngine(teamId, engine);
    const p = await ctx.repository.createPart(teamId, plug());
    await ctx.repository.installPart(teamId, { partId: p.id, target: { engineId: e.id }, ...when });
    const [install] = await ctx.repository.listPartInstalls(teamId, { engineId: e.id });
    return { ...ctx, e, p, installId: install!.id };
  }

  it("returns the unit to the stock", async () => {
    const { db, repository, p, installId } = await mounted();
    try {
      await repository.removePart(teamId, installId, {
        destination: "stock",
        reason: null,
        ...when,
      });
      expect((await repository.getPart(teamId, p.id))?.qtyCenti).toBe(300);
      expect((await repository.listStockMovements(teamId, p.id))[0]).toMatchObject({
        reason: "returned",
        qtyDeltaCenti: 100,
      });
      expect(await repository.listPartInstalls(teamId, { partId: p.id })).toHaveLength(0);
      const history = await repository.listPartInstalls(teamId, { partId: p.id }, true);
      expect(history[0]).toMatchObject({ removalDestination: "stock", removalReason: null });
    } finally {
      await db.close();
    }
  });

  it("scrap needs a reason and writes no movement; spare writes none either", async () => {
    const { db, repository, p, installId } = await mounted();
    try {
      await expect(
        repository.removePart(teamId, installId, { destination: "scrap", reason: null, ...when }),
      ).rejects.toMatchObject({ code: "constraint" });
      expect(await repository.listPartInstalls(teamId, { partId: p.id })).toHaveLength(1);

      await repository.removePart(teamId, installId, { destination: "scrap", reason: "worn", ...when });
      expect((await repository.getPart(teamId, p.id))?.qtyCenti).toBe(200);
      const [closed] = await repository.listPartInstalls(teamId, { partId: p.id }, true);
      expect(closed).toMatchObject({ removalDestination: "scrap", removalReason: "worn" });
    } finally {
      await db.close();
    }
  });

  it("ignores a reason for spare and refuses to close an install twice", async () => {
    const { db, repository, p, installId } = await mounted();
    try {
      await repository.removePart(teamId, installId, { destination: "spare", reason: "broken", ...when });
      const [closed] = await repository.listPartInstalls(teamId, { partId: p.id }, true);
      expect(closed).toMatchObject({ removalDestination: "spare", removalReason: null });
      expect((await repository.getPart(teamId, p.id))?.qtyCenti).toBe(200);
      await expect(
        repository.removePart(teamId, installId, { destination: "stock", reason: null, ...when }),
      ).rejects.toMatchObject({ code: "install_closed" });
      expect((await repository.getPart(teamId, p.id))?.qtyCenti).toBe(200);
    } finally {
      await db.close();
    }
  });

  it("does not close an install of another team", async () => {
    const { db, repository, installId } = await mounted();
    try {
      await expect(
        repository.removePart(otherTeamId, installId, { destination: "stock", reason: null, ...when }),
      ).rejects.toMatchObject({ code: "install_closed" });
    } finally {
      await db.close();
    }
  });
});
