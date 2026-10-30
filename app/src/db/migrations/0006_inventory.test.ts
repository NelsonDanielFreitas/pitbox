// @vitest-environment node

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { type NodeSqliteExecutor, openTestDatabase } from "@/test/sqlite";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const partId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70";
const otherPartId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const now = "2026-10-30T12:00:00Z";

const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

let db: NodeSqliteExecutor;

beforeEach(async () => {
  db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  for (const [id, team_] of [
    [partId, teamId],
    [otherPartId, otherTeamId],
  ] as const) {
    await db.exec(
      `INSERT INTO part (id, team_id, kind, description, unit, min_stock_centi, created_at, updated_at)
       VALUES ($id, $team, 'glow_plug', 'Vela', 'un', 200, $now, $now)`,
      { $id: id, $team: team_, $now: now },
    );
  }
});

afterEach(async () => {
  await db.close();
});

let seq = 0;
function movement(columns: Record<string, string | number | null>) {
  const row = {
    id: `0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b5${String(++seq).padStart(3, "0")}`,
    team_id: teamId,
    part_id: partId,
    qty_delta_centi: 100,
    reason: "purchase",
    occurred_at: now,
    created_at: now,
    ...columns,
  };
  const names = Object.keys(row);
  return db.exec(
    `INSERT INTO stock_movement (${names.join(", ")}) VALUES (${names.map((n) => `$${n}`).join(", ")})`,
    Object.fromEntries(names.map((n) => [`$${n}`, row[n as keyof typeof row]])),
  );
}

describe("0006_inventory: stock_movement", () => {
  it("accepts a priced purchase with store and author, and a lost movement with a reason", async () => {
    await movement({ unit_cost_cents: 950, store: "Loja RC Porto", created_by: userId });
    await movement({ reason: "lost", qty_delta_centi: -100, loss_reason: "expired" });
    const rows = await db.query<{ n: number }>("SELECT count(*) AS n FROM stock_movement");
    expect(rows[0]?.n).toBe(2);
  });

  it.each([
    ["a price outside a purchase", { reason: "consumed", qty_delta_centi: -100, unit_cost_cents: 950 }],
    ["a negative price", { unit_cost_cents: -1 }],
    ["a store outside a purchase", { reason: "consumed", qty_delta_centi: -100, store: "Loja" }],
    ["a blank store", { store: "   " }],
    ["a store longer than 80", { store: "x".repeat(81) }],
    ["a loss reason outside a loss", { loss_reason: "broken" }],
    ["an unknown loss reason", { reason: "lost", qty_delta_centi: -100, loss_reason: "stolen" }],
    ["a lost movement without a reason", { reason: "lost", qty_delta_centi: -100 }],
    ["an author that is not an id", { created_by: "Nelson" }],
  ])("refuses %s", async (_name, columns) => {
    await expect(movement(columns)).rejects.toThrow();
  });

  it("allows one undo per movement and only as an adjustment", async () => {
    const original = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b5f00";
    await movement({ id: original, unit_cost_cents: 950 });
    await movement({ reason: "adjustment", qty_delta_centi: -100, reverts_movement_id: original });
    await expect(
      movement({ reason: "adjustment", qty_delta_centi: -100, reverts_movement_id: original }),
    ).rejects.toThrow(/constraint failed/);
    await expect(movement({ reverts_movement_id: original })).rejects.toThrow();
  });

  it("is append-only: updates are refused", async () => {
    const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b5f01";
    await movement({ id });
    await expect(db.exec("UPDATE stock_movement SET note = 'x' WHERE id = $id", { $id: id })).rejects.toThrow(
      /immutable/,
    );
  });

  it("cascades with the part, including a movement that reverts another", async () => {
    const original = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b5f02";
    await movement({ id: original });
    await movement({ reason: "adjustment", qty_delta_centi: -100, reverts_movement_id: original });
    await db.exec("DELETE FROM part WHERE id = $id", { $id: partId });
    const rows = await db.query<{ n: number }>("SELECT count(*) AS n FROM stock_movement");
    expect(rows[0]?.n).toBe(0);
  });
});

describe("0006_inventory: part and part_install", () => {
  it("keeps reorder_to at or above the minimum", async () => {
    await db.exec("UPDATE part SET reorder_to_centi = 800 WHERE id = $id", { $id: partId });
    await expect(
      db.exec("UPDATE part SET reorder_to_centi = 100 WHERE id = $id", { $id: partId }),
    ).rejects.toThrow(/constraint failed/);
  });

  it("validates how a dismounted unit leaves", async () => {
    await db.exec(
      `INSERT INTO engine (id, team_id, make, model, status, created_at, updated_at)
       VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d90', $team, 'REDS', 'R7', 'active', $now, $now)`,
      { $team: teamId, $now: now },
    );
    const install = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d91";
    await db.exec(
      `INSERT INTO part_install (id, team_id, part_id, engine_id, installed_at, created_at, updated_at)
       VALUES ($id, $team, $part, '0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d90', '2026-10-01T10:00:00Z', $now, $now)`,
      { $id: install, $team: teamId, $part: partId, $now: now },
    );
    const close = (destination: string | null, reason: string | null) =>
      db.exec(
        `UPDATE part_install SET removed_at = '2026-10-20T10:00:00Z', removal_destination = $d, removal_reason = $r
         WHERE id = $id`,
        { $id: install, $d: destination, $r: reason },
      );
    await expect(close(null, null)).rejects.toThrow();
    await expect(close("scrap", null)).rejects.toThrow();
    await expect(close("scrap", "sold")).rejects.toThrow();
    await expect(close("stock", "worn")).rejects.toThrow();
    await expect(close("trash", null)).rejects.toThrow();
    await close("scrap", "worn");
  });
});

describe("0006_inventory: shopping_item and inventory count", () => {
  it("keeps one shopping line per part and refuses a part of another team", async () => {
    const insert = (team_: string, part: string, qty: number | null) =>
      db.exec(
        `INSERT INTO shopping_item (id, team_id, part_id, qty_centi, created_at, updated_at)
         VALUES ($id, $team, $part, $qty, $now, $now)`,
        { $id: crypto.randomUUID(), $team: team_, $part: part, $qty: qty, $now: now },
      );
    await insert(teamId, partId, 400);
    await expect(insert(teamId, partId, 100)).rejects.toThrow(/constraint failed/);
    await expect(insert(teamId, otherPartId, 100)).rejects.toThrow(/constraint failed/);
    await expect(insert(otherTeamId, otherPartId, 0)).rejects.toThrow(/constraint failed/);
  });

  it("keeps one count draft per team, with non-negative counted values of its own parts", async () => {
    await db.exec(
      "INSERT INTO inventory_count (team_id, scope_kind, started_at, base_rowid) VALUES ($t, NULL, $now, 0)",
      { $t: teamId, $now: now },
    );
    await expect(
      db.exec("INSERT INTO inventory_count (team_id, started_at, base_rowid) VALUES ($t, $now, 0)", {
        $t: teamId,
        $now: now,
      }),
    ).rejects.toThrow(/constraint failed/);
    const line = (part: string, counted: number) =>
      db.exec(
        "INSERT INTO inventory_count_line (team_id, part_id, counted_centi) VALUES ($t, $p, $c)",
        { $t: teamId, $p: part, $c: counted },
      );
    await line(partId, 300);
    await expect(line(partId, 400)).rejects.toThrow(/constraint failed/);
    await expect(line(otherPartId, 400)).rejects.toThrow(/constraint failed/);
    await db.exec("DELETE FROM inventory_count WHERE team_id = $t", { $t: teamId });
    const rows = await db.query<{ n: number }>("SELECT count(*) AS n FROM inventory_count_line");
    expect(rows[0]?.n).toBe(0);
  });
});
