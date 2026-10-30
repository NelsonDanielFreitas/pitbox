// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { databasePragmas } from "@/db/pragmas";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { NodeSqliteExecutor } from "@/test/sqlite";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const live = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e04";
const archived = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e05";
const now = "2026-10-30T12:00:00Z";

describe("0010 sync inventory backfill", () => {
  it("queues the stock movements and shopping lines of live parts, in order, once", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      for (const pragma of databasePragmas) await db.exec(pragma);
      await runMigrations(db, migrations.filter((m) => m.version <= 9));
      await db.transaction(
        teamBootstrapStatements([{ id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" }]),
      );
      for (const id of [live, archived]) {
        await db.exec(
          `INSERT INTO part (id, team_id, kind, description, unit, specs, min_stock_centi, created_at, updated_at)
           VALUES ($id, $t, 'glow_plug', 'Vela', 'un', '{}', 200, $n, $n)`,
          { $id: id, $t: teamId, $n: now },
        );
      }
      await db.exec("UPDATE part SET deleted_at = $n WHERE id = $id", { $n: now, $id: archived });
      const movement = (id: string, part: string, qty: number, reason: string, at: string, extra = "NULL, NULL, NULL") =>
        db.exec(
          `INSERT INTO stock_movement (id, team_id, part_id, qty_delta_centi, reason, occurred_at, created_at,
                                       note, store, unit_cost_cents)
           VALUES ($id, $t, $part, $qty, $reason, $at, $at, ${extra})`,
          { $id: id, $t: teamId, $part: part, $qty: qty, $reason: reason, $at: at },
        );
      await movement("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f02", live, -100, "consumed", "2026-10-02T10:00:00.000Z");
      await movement("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f01", live, 500, "purchase", "2026-10-01T10:00:00.000Z", "'Boa', 'Hobby', 350");
      await movement("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4f03", archived, 100, "purchase", "2026-10-01T10:00:00.000Z");
      await db.exec(
        `INSERT INTO shopping_item (id, team_id, part_id, qty_centi, dismissed, created_at, updated_at)
         VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a01', $t, $p, 300, 0, $n, $n)`,
        { $t: teamId, $p: live, $n: now },
      );
      const before = (await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox"))[0]?.n ?? -1;

      await runMigrations(db, migrations);

      const ops = await db.query<{ entity: string; entity_id: string; kind: number; base_version: number | null; fields_json: string; op_id: string }>(
        `SELECT entity, entity_id, kind, base_version, fields_json, op_id FROM sync_outbox
          WHERE rowid > (SELECT COALESCE(MAX(rowid), 0) - 3 FROM sync_outbox) ORDER BY rowid`,
      );
      expect(ops.map((o) => [o.entity, o.entity_id.slice(-2)])).toEqual([
        ["garage.stock_movement", "01"],
        ["garage.stock_movement", "02"],
        ["garage.shopping_item", "01"],
      ]);
      expect(ops.map((o) => o.kind)).toEqual([3, 3, 1]);
      for (const op of ops) {
        expect(op.base_version).toBeNull();
        expect(op.op_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
      }
      expect(JSON.parse(ops[0]?.fields_json ?? "{}")).toEqual({
        part_id: live, qty_delta_centi: 500, reason: "purchase", note: "Loja: Hobby · Boa",
        occurred_at: "2026-10-01T10:00:00.000Z", unit_cost_cents: 350, loss_reason: null,
      });
      expect(JSON.parse(ops[2]?.fields_json ?? "{}")).toEqual({
        part_id: live, qty_centi: 300, dismissed: false, note: null,
      });
      const after = (await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox"))[0]?.n ?? -1;
      expect(after - before).toBe(3);

      await runMigrations(db, migrations);
      expect((await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox"))[0]?.n).toBe(after);
    } finally {
      await db.close();
    }
  });
});
