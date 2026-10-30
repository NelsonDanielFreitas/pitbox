// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { databasePragmas } from "@/db/pragmas";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { NodeSqliteExecutor } from "@/test/sqlite";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const id = (n: string) => `0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4${n}`;
const now = "2026-10-30T12:00:00.000Z";

describe("0011 sync mounts backfill", () => {
  it("queues mounts and installs so that a close always comes before the next mount, once", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      for (const pragma of databasePragmas) await db.exec(pragma);
      await runMigrations(db, migrations.filter((m) => m.version <= 10));
      await db.transaction(
        teamBootstrapStatements([{ id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" }]),
      );
      const run = (sql: string, params: Record<string, string | number | null> = {}) =>
        db.exec(sql, { $t: teamId, $n: now, ...params });
      for (const [carId, make] of [["c01", "A"], ["c02", "B"], ["c03", "Gone"]] as const) {
        await run(
          `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
           VALUES ($id, $t, $make, 'X', 'active', '1/8', 'nitro', $n, $n)`,
          { $id: id(carId), $make: make },
        );
      }
      await run("UPDATE car SET deleted_at = $n WHERE make = 'Gone'");
      await run(
        `INSERT INTO engine (id, team_id, make, model, status, created_at, updated_at)
         VALUES ($id, $t, 'REDS', 'R7', 'active', $n, $n)`,
        { $id: id("e01") },
      );
      // History: E on car A (closed), then on car B (open). A mount on the archived car is skipped.
      const mount = (mid: string, car: string, from: string, to: string | null) =>
        run(
          `INSERT INTO engine_mount (id, team_id, engine_id, car_id, mounted_at, unmounted_at, created_at)
           VALUES ($id, $t, $e, $c, $from, $to, $n)`,
          { $id: id(mid), $e: id("e01"), $c: id(car), $from: from, $to: to },
        );
      await mount("m02", "c02", "2026-10-02T10:00:00.000Z", null);
      await mount("m01", "c01", "2026-10-01T10:00:00.000Z", "2026-10-02T10:00:00.000Z");
      await mount("m03", "c03", "2026-09-01T10:00:00.000Z", "2026-09-02T10:00:00.000Z");
      await run(
        `INSERT INTO part (id, team_id, kind, description, unit, specs, min_stock_centi, has_service_life, created_at, updated_at)
         VALUES ($id, $t, 'glow_plug', 'Vela', 'un', '{}', 0, 1, $n, $n)`,
        { $id: id("p01") },
      );
      await run(
        `INSERT INTO part_install (id, team_id, part_id, car_id, installed_at, removed_at, removal_destination,
                                   removal_reason, created_at, updated_at)
         VALUES ($id, $t, $p, $c, '2026-10-03T10:00:00.000Z', '2026-10-04T10:00:00.000Z', 'scrap', 'worn', $n, $n)`,
        { $id: id("i01"), $p: id("p01"), $c: id("c01") },
      );
      const before = (await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox"))[0]?.n ?? -1;

      await runMigrations(db, migrations);

      const ops = await db.query<{ entity: string; entity_id: string; kind: number; base_version: number | null; fields_json: string; op_id: string }>(
        "SELECT entity, entity_id, kind, base_version, fields_json, op_id FROM sync_outbox WHERE rowid > $skip ORDER BY rowid",
        { $skip: before },
      );
      const mounts = ops.filter((o) => o.entity === "garage.engine_mount");
      expect(mounts.map((o) => [o.entity_id.slice(-3), o.kind])).toEqual([
        ["m01", 1],
        ["m01", 2],
        ["m02", 1],
      ]);
      expect(mounts[1]).toMatchObject({ base_version: 1 });
      expect(JSON.parse(mounts[0]?.fields_json ?? "{}")).toEqual({
        engine_id: id("e01"), car_id: id("c01"), mounted_at: "2026-10-01T10:00:00.000Z",
      });
      expect(JSON.parse(mounts[1]?.fields_json ?? "{}")).toEqual({ unmounted_at: "2026-10-02T10:00:00.000Z" });

      const installs = ops.filter((o) => o.entity === "garage.part_install");
      expect(installs.map((o) => o.kind)).toEqual([1, 2]);
      expect(JSON.parse(installs[0]?.fields_json ?? "{}")).toEqual({
        part_id: id("p01"), car_id: id("c01"), engine_id: null, installed_at: "2026-10-03T10:00:00.000Z",
      });
      expect(JSON.parse(installs[1]?.fields_json ?? "{}")).toEqual({
        removed_at: "2026-10-04T10:00:00.000Z", removal_destination: "scrap", removal_reason: "worn",
      });
      expect(installs[1]).toMatchObject({ base_version: 1 });
      expect(new Set(ops.map((o) => o.op_id)).size).toBe(ops.length);
      expect(ops).toHaveLength(5);

      await runMigrations(db, migrations);
      expect((await db.query<{ n: number }>("SELECT count(*) AS n FROM sync_outbox"))[0]?.n).toBe(before + 5);
    } finally {
      await db.close();
    }
  });
});
