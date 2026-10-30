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
const apply = { occurredOn: "2026-10-26", note: "Ajuste · Contagem de 26 out", createdBy: userId };

const part = (description: string, qty: number, over: Partial<PartCreateInput> = {}): PartCreateInput => ({
  kind: "bearing",
  brand: null,
  reference: null,
  description,
  unit: "un",
  specs: {},
  minStockCenti: 200,
  reorderToCenti: null,
  initialQtyCenti: qty,
  hasServiceLife: false,
  createdBy: userId,
  ...over,
});

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  const repository = new LocalGarageRepository(db, () => "2026-10-26T12:00:00.000Z");
  return { db, repository };
}

describe("inventory count", () => {
  it("keeps one draft per team, with the counted lines", async () => {
    const { db, repository } = await setup();
    try {
      expect(await repository.getCountDraft(teamId)).toBeNull();
      const a = await repository.createPart(teamId, part("A", 500));
      const draft = await repository.startCount(teamId, "bearing");
      expect(draft).toMatchObject({ scopeKind: "bearing", lines: {}, movementsSince: 0 });
      // Starting again keeps the open draft (and its scope).
      expect(await repository.startCount(teamId, null)).toMatchObject({ scopeKind: "bearing" });

      await repository.setCountLine(teamId, a.id, 300);
      await repository.setCountLine(teamId, a.id, 400);
      expect((await repository.getCountDraft(teamId))?.lines).toEqual({ [a.id]: 400 });
      await repository.setCountLine(teamId, a.id, null);
      expect((await repository.getCountDraft(teamId))?.lines).toEqual({});
      expect(await repository.getCountDraft(otherTeamId)).toBeNull();

      await repository.discardCount(teamId);
      expect(await repository.getCountDraft(teamId)).toBeNull();
    } finally {
      await db.close();
    }
  });

  it("refuses a line without a draft or for a part of another team", async () => {
    const { db, repository } = await setup();
    try {
      const a = await repository.createPart(teamId, part("A", 500));
      await expect(repository.setCountLine(teamId, a.id, 100)).rejects.toMatchObject({
        code: "not_serviceable",
      });
      await repository.startCount(otherTeamId, null);
      await expect(repository.setCountLine(otherTeamId, a.id, 100)).rejects.toMatchObject({
        code: "not_serviceable",
      });
    } finally {
      await db.close();
    }
  });

  it("applies only the parts that changed, using the current stock", async () => {
    const { db, repository } = await setup();
    try {
      const a = await repository.createPart(teamId, part("A", 500));
      const b = await repository.createPart(teamId, part("B", 300));
      const c = await repository.createPart(teamId, part("C", 100));
      await repository.startCount(teamId, null);
      await repository.setCountLine(teamId, a.id, 400); // expected 500 -> -100
      await repository.setCountLine(teamId, b.id, 300); // matches -> no movement
      await repository.setCountLine(teamId, c.id, 400); // counted 4 after the stock changed
      // Someone uses the only unit of C while counting: the adjustment is 0 -> 400, not 100 -> 400.
      await repository.addStockMovement(teamId, {
        partId: c.id,
        reason: "consumed",
        qtyDeltaCenti: -100,
        lossReason: null,
        unitCostCents: null,
        store: null,
        occurredOn: "2026-10-26",
        note: null,
        createdBy: userId,
      });
      expect((await repository.getCountDraft(teamId))?.movementsSince).toBe(1);

      const applied = await repository.applyCount(teamId, apply);
      expect(applied).toBe(2);
      expect((await repository.getPart(teamId, a.id))?.qtyCenti).toBe(400);
      expect((await repository.getPart(teamId, b.id))?.qtyCenti).toBe(300);
      expect((await repository.getPart(teamId, c.id))?.qtyCenti).toBe(400);
      const [adjustC] = await repository.listStockMovements(teamId, c.id);
      expect(adjustC).toMatchObject({ reason: "adjustment", qtyDeltaCenti: 400, note: apply.note, createdBy: userId });
      expect(await repository.listStockMovements(teamId, b.id)).toHaveLength(1);
      expect(await repository.getCountDraft(teamId)).toBeNull();
    } finally {
      await db.close();
    }
  });

  it("is atomic: nothing changes without an open draft, and a second apply is refused", async () => {
    const { db, repository } = await setup();
    try {
      const a = await repository.createPart(teamId, part("A", 500));
      await expect(repository.applyCount(teamId, apply)).rejects.toMatchObject({ code: "not_serviceable" });
      await repository.startCount(teamId, null);
      await repository.setCountLine(teamId, a.id, 0);
      expect(await repository.applyCount(teamId, apply)).toBe(1);
      await expect(repository.applyCount(teamId, apply)).rejects.toMatchObject({ code: "not_serviceable" });
      expect((await repository.getPart(teamId, a.id))?.qtyCenti).toBe(0);
    } finally {
      await db.close();
    }
  });

  it("brings a hidden shopping line back once the count lifts the stock to the minimum", async () => {
    const { db, repository } = await setup();
    try {
      const a = await repository.createPart(teamId, part("A", 100));
      await db.exec(
        `INSERT INTO shopping_item (id, team_id, part_id, dismissed, created_at, updated_at)
         VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e00', $t, $p, 1, 'x', 'x')`,
        { $t: teamId, $p: a.id },
      );
      await repository.startCount(teamId, null);
      await repository.setCountLine(teamId, a.id, 600);
      await repository.applyCount(teamId, apply);
      const rows = await db.query<{ n: number }>("SELECT count(*) AS n FROM shopping_item");
      expect(rows[0]?.n).toBe(0);
    } finally {
      await db.close();
    }
  });
});
