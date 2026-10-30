// @vitest-environment node

import { describe, expect, it } from "vitest";

import { openTestDatabase } from "@/test/sqlite";
import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";

import type { PartCreateInput, PartInput, StockMovementInput } from "../domain/models";
import { LocalGarageRepository } from "./local-garage-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

function partInput(overrides: Partial<PartCreateInput> = {}): PartCreateInput {
  return {
    kind: "fuel",
    brand: "Runner Time",
    reference: "RT25",
    description: "Combustível 25%",
    unit: "l",
    specs: { nitro_pct: 25, oil_pct: 9 },
    minStockCenti: 200,
    reorderToCenti: 600,
    initialQtyCenti: 500,
    unitCostCents: 1400,
    createdBy: userId,
    hasServiceLife: false,
    ...overrides,
  };
}

function movement(partId: string, overrides: Partial<StockMovementInput> = {}): StockMovementInput {
  return {
    partId,
    reason: "consumed",
    qtyDeltaCenti: -100,
    lossReason: null,
    unitCostCents: null,
    store: null,
    occurredOn: "2026-10-26",
    note: null,
    createdBy: userId,
    ...overrides,
  };
}

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  const repository = new LocalGarageRepository(db, () => "2026-10-26T12:00:00.000Z");
  return { db, repository };
}

describe("parts: specs, restock target and costs", () => {
  it("round-trips specs and the restock target, and hides costs without permission", async () => {
    const { db, repository } = await setup();
    try {
      const created = await repository.createPart(teamId, partInput());
      expect(created.specs).toEqual({ nitro_pct: 25, oil_pct: 9 });
      expect(created.reorderToCenti).toBe(600);
      expect(created.unitCostCents).toBe(1400);

      const withCosts = await repository.getPart(teamId, created.id, { costs: true });
      expect(withCosts?.unitCostCents).toBe(1400);
      // A reader that may not see costs never gets the key at all.
      const without = await repository.getPart(teamId, created.id);
      expect(without && "unitCostCents" in without).toBe(false);
      const list = await repository.listParts(teamId, { costs: false });
      expect(list.every((p) => !("unitCostCents" in p))).toBe(true);
      const moves = await repository.listStockMovements(teamId, created.id, { costs: false });
      expect(moves.every((m) => !("unitCostCents" in m))).toBe(true);
      const movesWithCosts = await repository.listStockMovements(teamId, created.id, { costs: true });
      expect(movesWithCosts[0]?.unitCostCents).toBe(1400);
    } finally {
      await db.close();
    }
  });

  it("keeps the stored cost when an update does not set one, and refuses reorder below the minimum", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput());
      const base: PartInput = {
        kind: "fuel",
        brand: "Runner Time",
        reference: "RT25",
        description: "Combustível 25%",
        unit: "l",
        specs: { nitro_pct: 25, oil_pct: 9 },
        minStockCenti: 200,
        reorderToCenti: 600,
        hasServiceLife: false,
      };
      const updated = await repository.updatePart(teamId, part.id, part.localRev, {
        ...base,
        description: "Combustível 20%",
      });
      expect(updated.description).toBe("Combustível 20%");
      expect((await repository.getPart(teamId, part.id, { costs: true }))?.unitCostCents).toBe(1400);
      await expect(
        repository.updatePart(teamId, part.id, updated.localRev, { ...base, reorderToCenti: 100 }),
      ).rejects.toMatchObject({ code: "constraint" });
    } finally {
      await db.close();
    }
  });
});

describe("stock movements", () => {
  it("records a priced purchase with a store, a structured loss and the author", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput({ initialQtyCenti: 0 }));
      const bought = await repository.addStockMovement(
        teamId,
        movement(part.id, {
          reason: "purchase",
          qtyDeltaCenti: 400,
          unitCostCents: 1450,
          store: "RC Mania",
          note: "caixa nova",
        }),
      );
      expect(bought).toMatchObject({ beforeCenti: 0, afterCenti: 400, minStockCenti: 200 });
      const lost = await repository.addStockMovement(
        teamId,
        movement(part.id, { reason: "lost", qtyDeltaCenti: -100, lossReason: "expired" }),
      );
      expect(lost).toMatchObject({ beforeCenti: 400, afterCenti: 300 });

      const [first, second] = await repository.listStockMovements(teamId, part.id, { costs: true });
      expect(second).toMatchObject({
        reason: "purchase",
        unitCostCents: 1450,
        store: "RC Mania",
        createdBy: userId,
        undone: false,
      });
      expect(first).toMatchObject({ reason: "lost", lossReason: "expired" });
    } finally {
      await db.close();
    }
  });

  it.each([
    ["a price outside a purchase", { unitCostCents: 100 }],
    ["a loss without a reason", { reason: "lost" as const }],
    ["a reason on a consumption", { lossReason: "broken" as const }],
    ["a negative purchase", { reason: "purchase" as const, qtyDeltaCenti: -100 }],
    ["a zero quantity", { qtyDeltaCenti: 0 }],
  ])("refuses %s", async (_name, overrides) => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput());
      await expect(
        repository.addStockMovement(teamId, movement(part.id, overrides)),
      ).rejects.toMatchObject({ code: "constraint" });
      expect((await repository.getPart(teamId, part.id))?.qtyCenti).toBe(500);
    } finally {
      await db.close();
    }
  });

  it("does not touch a part of another team or an archived part", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput());
      await expect(
        repository.addStockMovement(otherTeamId, movement(part.id)),
      ).rejects.toMatchObject({ code: "constraint" });
      await repository.archivePart(teamId, part.id, part.localRev);
      await expect(repository.addStockMovement(teamId, movement(part.id))).rejects.toMatchObject({
        code: "constraint",
      });
      expect(await repository.listParts(teamId)).toHaveLength(0);
    } finally {
      await db.close();
    }
  });

  it("a purchase removes the shopping line; other changes bring a hidden line back at the minimum", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput());
      const insertLine = (dismissed: number) =>
        db.exec(
          `INSERT INTO shopping_item (id, team_id, part_id, dismissed, created_at, updated_at)
           VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e00', $t, $p, $d, 'x', 'x')`,
          { $t: teamId, $p: part.id, $d: dismissed },
        );
      const lines = async () =>
        (await db.query<{ n: number }>("SELECT count(*) AS n FROM shopping_item"))[0]?.n;

      await insertLine(0);
      await repository.addStockMovement(
        teamId,
        movement(part.id, { reason: "purchase", qtyDeltaCenti: 100 }),
      );
      expect(await lines()).toBe(0);

      // Hidden line, stock still below the minimum: it stays hidden.
      await repository.addStockMovement(teamId, movement(part.id, { qtyDeltaCenti: -500 }));
      await insertLine(1);
      await repository.addStockMovement(teamId, movement(part.id, { qtyDeltaCenti: -50 }));
      expect(await lines()).toBe(1);
      // Back at the minimum: the line is cleared so the alert works next time.
      await repository.addStockMovement(
        teamId,
        movement(part.id, { reason: "adjustment", qtyDeltaCenti: 600 }),
      );
      expect(await lines()).toBe(0);
    } finally {
      await db.close();
    }
  });
});

describe("undoing a movement", () => {
  const undo = { occurredOn: "2026-10-26", note: "Anula o movimento de 26 out", createdBy: userId };

  it("appends the inverse as an adjustment and keeps the original", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput());
      await repository.addStockMovement(teamId, movement(part.id, { qtyDeltaCenti: -100 }));
      const [consumed] = await repository.listStockMovements(teamId, part.id);
      const change = await repository.undoMovement(teamId, consumed!.id, undo);
      expect(change).toMatchObject({ beforeCenti: 400, afterCenti: 500 });

      const moves = await repository.listStockMovements(teamId, part.id);
      expect(moves[0]).toMatchObject({
        reason: "adjustment",
        qtyDeltaCenti: 100,
        note: undo.note,
        revertsMovementId: consumed!.id,
        undone: false,
      });
      expect(moves[1]).toMatchObject({ id: consumed!.id, qtyDeltaCenti: -100, undone: true });
    } finally {
      await db.close();
    }
  });

  it("undoes a priced purchase as a negative adjustment", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput({ initialQtyCenti: 0 }));
      await repository.addStockMovement(
        teamId,
        movement(part.id, { reason: "purchase", qtyDeltaCenti: 400, unitCostCents: 1450 }),
      );
      const [purchase] = await repository.listStockMovements(teamId, part.id);
      await repository.undoMovement(teamId, purchase!.id, undo);
      expect((await repository.getPart(teamId, part.id))?.qtyCenti).toBe(0);
    } finally {
      await db.close();
    }
  });

  it("refuses a second undo, an undo of an undo, an unknown movement and another team", async () => {
    const { db, repository } = await setup();
    try {
      const part = await repository.createPart(teamId, partInput());
      await repository.addStockMovement(teamId, movement(part.id));
      const [consumed] = await repository.listStockMovements(teamId, part.id);
      await repository.undoMovement(teamId, consumed!.id, undo);
      await expect(repository.undoMovement(teamId, consumed!.id, undo)).rejects.toMatchObject({
        code: "undone",
      });
      const [inverse] = await repository.listStockMovements(teamId, part.id);
      await expect(repository.undoMovement(teamId, inverse!.id, undo)).rejects.toMatchObject({
        code: "not_serviceable",
      });
      await expect(
        repository.undoMovement(teamId, "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4eff", undo),
      ).rejects.toMatchObject({ code: "not_serviceable" });
      await expect(repository.undoMovement(otherTeamId, consumed!.id, undo)).rejects.toMatchObject({
        code: "not_serviceable",
      });
      expect((await repository.getPart(teamId, part.id))?.qtyCenti).toBe(500);
    } finally {
      await db.close();
    }
  });
});
