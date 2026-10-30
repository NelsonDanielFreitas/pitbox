// @vitest-environment node

import { describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { openTestDatabase } from "@/test/sqlite";

import type { PartCreateInput } from "../domain/models";
import { LocalGarageRepository } from "./local-garage-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

const part = (description: string, qty: number, over: Partial<PartCreateInput> = {}): PartCreateInput => ({
  kind: "bearing",
  brand: "Mugen",
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

const consume = (partId: string, qty: number) => ({
  partId,
  reason: "consumed" as const,
  qtyDeltaCenti: -qty,
  lossReason: null,
  unitCostCents: null,
  store: null,
  occurredOn: "2026-10-26",
  note: null,
  createdBy: userId,
});

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  const repository = new LocalGarageRepository(db, () => "2026-10-26T12:00:00.000Z");
  return { db, repository };
}

describe("shopping list", () => {
  it("lists parts below the minimum with the suggested quantity, and hides costs", async () => {
    const { db, repository } = await setup();
    try {
      await repository.createPart(teamId, part("Ok", 500));
      const low = await repository.createPart(teamId, part("Baixo", 100));
      const targeted = await repository.createPart(teamId, part("Alvo", 0, { reorderToCenti: 1000 }));

      const lines = await repository.listShoppingList(teamId, { costs: true });
      expect(lines.map((l) => l.description)).toEqual(["Alvo", "Baixo"]);
      expect(lines.find((l) => l.partId === low.id)).toMatchObject({
        belowMin: true,
        qtyCenti: 200,
        suggestedQtyCenti: 200,
        unitCostCents: 250,
      });
      expect(lines.find((l) => l.partId === targeted.id)?.qtyCenti).toBe(1000);

      const hidden = await repository.listShoppingList(teamId);
      expect(hidden.every((l) => !("unitCostCents" in l))).toBe(true);
      expect(await repository.listShoppingList(otherTeamId)).toEqual([]);
    } finally {
      await db.close();
    }
  });

  it("adds a manual line, adjusts a quantity and refuses a zero suggestion", async () => {
    const { db, repository } = await setup();
    try {
      const ok = await repository.createPart(teamId, part("Ok", 500));
      await expect(repository.setShoppingItem(teamId, ok.id, null)).rejects.toMatchObject({
        code: "not_serviceable",
      });
      await repository.setShoppingItem(teamId, ok.id, 200);
      expect(await repository.listShoppingList(teamId)).toEqual([
        expect.objectContaining({ partId: ok.id, belowMin: false, qtyCenti: 200 }),
      ]);

      const low = await repository.createPart(teamId, part("Baixo", 100));
      await repository.setShoppingItem(teamId, low.id, 500);
      await repository.setShoppingItem(teamId, low.id, null); // back to the suggestion
      expect((await repository.listShoppingList(teamId)).find((l) => l.partId === low.id)?.qtyCenti).toBe(200);

      await expect(repository.setShoppingItem(otherTeamId, ok.id, 100)).rejects.toMatchObject({
        code: "not_serviceable",
      });
    } finally {
      await db.close();
    }
  });

  it("removes a manual line, and hides an automatic one until the stock recovers", async () => {
    const { db, repository } = await setup();
    try {
      const ok = await repository.createPart(teamId, part("Ok", 500));
      await repository.setShoppingItem(teamId, ok.id, 100);
      await repository.removeShoppingItem(teamId, ok.id);
      expect(await repository.listShoppingList(teamId)).toEqual([]);
      await expect(repository.removeShoppingItem(teamId, ok.id)).rejects.toMatchObject({
        name: "GarageNotFoundError",
      });

      const low = await repository.createPart(teamId, part("Baixo", 100));
      await repository.removeShoppingItem(teamId, low.id);
      expect(await repository.listShoppingList(teamId)).toEqual([]);

      // Still low: the part stays hidden. Back at the minimum: the hidden line is cleared,
      // so it reappears the next time the stock drops.
      await repository.addStockMovement(teamId, { ...consume(low.id, 0), qtyDeltaCenti: -50 });
      expect(await repository.listShoppingList(teamId)).toEqual([]);
      await repository.addStockMovement(teamId, {
        ...consume(low.id, 0),
        reason: "adjustment",
        qtyDeltaCenti: 500,
      });
      await repository.addStockMovement(teamId, consume(low.id, 400));
      expect(await repository.listShoppingList(teamId)).toHaveLength(1);
    } finally {
      await db.close();
    }
  });

  it("a purchase takes the line off the list", async () => {
    const { db, repository } = await setup();
    try {
      const low = await repository.createPart(teamId, part("Baixo", 100));
      await repository.addStockMovement(teamId, {
        ...consume(low.id, 0),
        reason: "purchase",
        qtyDeltaCenti: 200,
        unitCostCents: 250,
      });
      expect(await repository.listShoppingList(teamId)).toEqual([]);
      const ok = await repository.createPart(teamId, part("Ok", 500));
      await repository.setShoppingItem(teamId, ok.id, 100);
      await repository.addStockMovement(teamId, {
        ...consume(ok.id, 0),
        reason: "purchase",
        qtyDeltaCenti: 100,
      });
      expect(await repository.listShoppingList(teamId)).toEqual([]);
    } finally {
      await db.close();
    }
  });
});
