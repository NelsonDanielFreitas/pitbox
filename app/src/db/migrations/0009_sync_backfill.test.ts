// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { databasePragmas } from "@/db/pragmas";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { NodeSqliteExecutor } from "@/test/sqlite";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const now = "2026-10-30T12:00:00Z";

describe("0009 sync backfill", () => {
  it("queues the engines, tyre sets and parts that only exist locally, once", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      for (const pragma of databasePragmas) await db.exec(pragma);
      await runMigrations(db, migrations.filter((m) => m.version <= 8));
      await db.transaction(
        teamBootstrapStatements([{ id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" }]),
      );
      await db.exec(
        `INSERT INTO engine (id, team_id, make, model, status, created_at, updated_at, notes)
         VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e01', $t, 'REDS', 'R7', 'active', $n, $n, NULL),
                ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e02', $t, 'Gone', 'X', 'active', $n, $n, NULL)`,
        { $t: teamId, $n: now },
      );
      await db.exec("UPDATE engine SET deleted_at = $n WHERE make = 'Gone'", { $n: now });
      await db.exec(
        `INSERT INTO tyre_set (id, team_id, label, make, model, status, created_at, updated_at)
         VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e03', $t, 'J2', 'Pro-Line', 'Badlands', 'new', $n, $n)`,
        { $t: teamId, $n: now },
      );
      await db.exec(
        `INSERT INTO part (id, team_id, kind, description, unit, specs, min_stock_centi,
                           unit_cost_cents, has_service_life, created_at, updated_at)
         VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e04', $t, 'glow_plug', 'Vela', 'un',
                 '{"heat":"medium"}', 200, 950, 1, $n, $n)`,
        { $t: teamId, $n: now },
      );

      await runMigrations(db, migrations);

      const ops = await db.query<{ entity: string; entity_id: string; kind: number; base_version: number | null; fields_json: string; op_id: string; state: string }>(
        "SELECT entity, entity_id, kind, base_version, fields_json, op_id, state FROM sync_outbox ORDER BY entity",
      );
      expect(ops.map((o) => o.entity)).toEqual(["garage.engine", "garage.part", "garage.tyre_set"]);
      for (const op of ops) {
        expect(op).toMatchObject({ kind: 1, base_version: null, state: "pending" });
        expect(op.op_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
      }
      expect(JSON.parse(ops[0]?.fields_json ?? "{}")).toEqual({
        make: "REDS", model: "R7", serial_number: null, purchased_on: null, status: "active", notes: null,
      });
      expect(JSON.parse(ops[1]?.fields_json ?? "{}")).toEqual({
        kind: "glow_plug", brand: null, reference: null, description: "Vela", unit: "un",
        specs: { heat: "medium" }, min_stock_centi: 200, reorder_to_centi: null, unit_cost_cents: 950,
        has_service_life: true,
      });
      expect(new Set(ops.map((o) => o.op_id)).size).toBe(3);

      // Running the migrations again does not queue anything twice.
      await runMigrations(db, migrations);
      const [{ n }] = await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox") as [{ n: number }];
      expect(n).toBe(3);
    } finally {
      await db.close();
    }
  });
});
