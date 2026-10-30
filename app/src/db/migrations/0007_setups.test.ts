// @vitest-environment node

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { type NodeSqliteExecutor, openTestDatabase } from "@/test/sqlite";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const carId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70";
const setupId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const now = "2026-10-30T12:00:00Z";

const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

let db: NodeSqliteExecutor;

beforeEach(async () => {
  db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  await db.exec(
    `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
     VALUES ($car, $team, 'Mugen', 'MBX8', 'active', '1/8', 'nitro', $now, $now)`,
    { $car: carId, $team: teamId, $now: now },
  );
  await db.exec(
    `INSERT INTO setup (id, team_id, car_id, name, created_at, updated_at)
     VALUES ($id, $team, $car, 'Pista seca', $now, $now)`,
    { $id: setupId, $team: teamId, $car: carId, $now: now },
  );
  await db.exec(
    `INSERT INTO setup_version (team_id, setup_id, version, data, schema_version, created_at, created_by)
     VALUES ($team, $id, 1, '{}', 1, $now, $user)`,
    { $team: teamId, $id: setupId, $now: now, $user: userId },
  );
});

afterEach(async () => {
  await db.close();
});

let seq = 0;
function feedback(columns: Record<string, string | number | null>) {
  const row = {
    id: `0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b6${String(++seq).padStart(3, "0")}`,
    team_id: teamId,
    setup_id: setupId,
    version: 1,
    author_id: userId,
    created_at: now,
    ...columns,
  };
  const names = Object.keys(row);
  return db.exec(
    `INSERT INTO setup_feedback (${names.join(", ")}) VALUES (${names.map((n) => `$${n}`).join(", ")})`,
    Object.fromEntries(names.map((n) => [`$${n}`, row[n as keyof typeof row]])),
  );
}

describe("0007_setups: setup and setup_version", () => {
  it("starts local_rev at 0 and keeps the author as an id only", async () => {
    const rows = await db.query<{ local_rev: number }>("SELECT local_rev FROM setup WHERE id = $id", {
      $id: setupId,
    });
    expect(rows[0]?.local_rev).toBe(0);
    await expect(
      db.exec("UPDATE setup SET created_by = 'Nelson' WHERE id = $id", { $id: setupId }),
    ).rejects.toThrow(/constraint failed/);
  });

  it("refuses to change a version, whatever the column", async () => {
    await expect(
      db.exec("UPDATE setup_version SET data = '{\"a\":1}' WHERE setup_id = $id", { $id: setupId }),
    ).rejects.toThrow(/immutable/);
    await expect(
      db.exec("UPDATE setup_version SET change_summary = 'x' WHERE setup_id = $id", { $id: setupId }),
    ).rejects.toThrow(/immutable/);
  });

  it("still lets the setup be updated and cascades versions and feedback away with it", async () => {
    await db.exec("UPDATE setup SET name = 'Outro', local_rev = local_rev + 1 WHERE id = $id", { $id: setupId });
    await feedback({ traction: 4 });
    await db.exec("DELETE FROM setup WHERE id = $id", { $id: setupId });
    const versions = await db.query<{ n: number }>("SELECT count(*) AS n FROM setup_version");
    const notes = await db.query<{ n: number }>("SELECT count(*) AS n FROM setup_feedback");
    expect([versions[0]?.n, notes[0]?.n]).toEqual([0, 0]);
  });
});

describe("0007_setups: setup_feedback", () => {
  it("accepts scales, zone and note, or a note alone", async () => {
    await feedback({ steering_entry: 2, traction: 5, track_zone: "Curva 3", note: "Sai de frente" });
    await feedback({ note: "Muito nervoso" });
    const rows = await db.query<{ n: number }>("SELECT count(*) AS n FROM setup_feedback");
    expect(rows[0]?.n).toBe(2);
  });

  it.each([
    ["no value and no note", {}],
    ["only a zone", { track_zone: "Curva 3" }],
    ["a scale below 1", { traction: 0 }],
    ["a scale above 5", { braking: 6 }],
    ["a blank note", { note: "   " }],
    ["a note longer than 2000", { note: "x".repeat(2001) }],
    ["a zone longer than 80", { traction: 3, track_zone: "x".repeat(81) }],
    ["an author that is not an id", { traction: 3, author_id: "Pai" }],
    ["a version that does not exist", { traction: 3, version: 2 }],
    ["a setup of another team", { traction: 3, team_id: otherTeamId }],
  ])("refuses %s", async (_name, columns) => {
    await expect(feedback(columns)).rejects.toThrow();
  });

  it("is append-only: updates are refused", async () => {
    const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b6f00";
    await feedback({ id, traction: 3 });
    await expect(db.exec("UPDATE setup_feedback SET traction = 4 WHERE id = $id", { $id: id })).rejects.toThrow(
      /immutable/,
    );
  });
});
