// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { databasePragmas } from "@/db/pragmas";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { NodeSqliteExecutor } from "@/test/sqlite";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const carId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e01";
const tplId = "01a1990b-b4dd-777f-9c5b-e31aaf01355f";
const now = "2026-10-30T12:00:00Z";

async function before() {
  const db = await NodeSqliteExecutor.create();
  for (const pragma of databasePragmas) await db.exec(pragma);
  await runMigrations(db, migrations.filter((m) => m.version <= 13));
  await db.transaction(
    teamBootstrapStatements([{ id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" }]),
  );
  await db.exec(
    `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
     VALUES ($c, $t, 'Mugen', 'MBX8R', 'active', '1/8', 'nitro', $n, $n)`,
    { $c: carId, $t: teamId, $n: now },
  );
  return db;
}

describe("0014 sheet templates", () => {
  it("does not change existing rows and leaves the car on the generic sheet", async () => {
    const db = await before();
    try {
      const rowsBefore = await db.query("SELECT * FROM car");
      await runMigrations(db, migrations);
      const rowsAfter = await db.query<Record<string, unknown>>("SELECT * FROM car");
      expect(rowsAfter).toHaveLength(1);
      const { sheet_template_id: link, ...rest } = rowsAfter[0] ?? {};
      expect(link).toBeNull();
      expect(rest).toEqual(rowsBefore[0]);
    } finally {
      await db.close();
    }
  });

  it("keeps template versions immutable", async () => {
    const db = await before();
    try {
      await runMigrations(db, migrations);
      await db.exec(
        `INSERT INTO sheet_template (id, team_id, slug, make, model, scale, powertrain, name, current_version, updated_at)
         VALUES ($i, NULL, 'generic', 'Generic', 'Generic', '1/8', 'nitro', 'Generic', 1, $n)`,
        { $i: tplId, $n: now },
      );
      await db.exec("INSERT INTO sheet_template_version (template_id, version, definition) VALUES ($i, 1, '{}')", { $i: tplId });
      await expect(db.exec("UPDATE sheet_template_version SET definition = '{\"x\":1}'")).rejects.toThrow();
      await expect(db.exec("INSERT INTO sheet_template_version (template_id, version, definition) VALUES ($i, 2, 'nope')", { $i: tplId })).rejects.toThrow();
    } finally {
      await db.close();
    }
  });
});
