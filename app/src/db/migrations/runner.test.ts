// @vitest-environment node

import { describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { NodeSqliteExecutor } from "@/test/sqlite";
import { teamBootstrapStatements } from "@/sync/team-statements";

import { migrations } from "./index";
import { runMigrations } from "./runner";

describe("runMigrations", () => {
  it("applies v1, preserves its rows, and applies v2", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      await runMigrations(db, [migrations[0]!]);
      await db.exec(
        "INSERT INTO team (id, name, category, country_code, role, updated_at) VALUES ($id, $name, $category, $country, $role, $updatedAt)",
        {
          $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
          $name: "Equipa Alpha",
          $category: "1/8 TT nitro",
          $country: "PT",
          $role: "owner",
          $updatedAt: "2026-10-25T12:00:00Z",
        },
      );

      await runMigrations(db, [migrations[1]!, migrations[2]!]);
      const teams = await db.query<{ name: string }>("SELECT name FROM team");
      expect(teams).toEqual([{ name: "Equipa Alpha" }]);
      const applied = await db.query<{ version: number }>(
        "SELECT version FROM schema_migrations ORDER BY version",
      );
      expect(applied.map((row) => row.version)).toEqual([1, 2, 3]);
    } finally {
      await db.close();
    }
  });

  it("keeps local garage rows when the team list is refreshed", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      await runMigrations(db, migrations);
      const team: Team = {
        id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
        name: "Equipa Alpha",
        category: "1/8 TT nitro",
        countryCode: "PT",
        role: "owner",
      };
      await db.transaction(teamBootstrapStatements([team]));
      await db.exec(
        `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
         VALUES ($id, $team, $make, $model, 'active', '1/8', 'nitro', $now, $now)`,
        {
          $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
          $team: team.id,
          $make: "Mugen",
          $model: "MBX8",
          $now: "2026-10-25T12:00:00Z",
        },
      );

      await db.transaction(teamBootstrapStatements([{ ...team, name: "Equipa Alpha 2" }]));
      const cars = await db.query<{ count: number }>("SELECT count(*) AS count FROM car");
      expect(cars[0]?.count).toBe(1);
      const names = await db.query<{ name: string }>("SELECT name FROM team");
      expect(names[0]?.name).toBe("Equipa Alpha 2");
    } finally {
      await db.close();
    }
  });

  it("is idempotent and rejects a changed migration", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      await runMigrations(db, [migrations[0]!]);
      await expect(runMigrations(db, [migrations[0]!])).resolves.toBeUndefined();
      await expect(
        runMigrations(db, [{ ...migrations[0]!, sql: `${migrations[0]!.sql}\n-- changed` }]),
      ).rejects.toThrow(/modified/);
    } finally {
      await db.close();
    }
  });
});
