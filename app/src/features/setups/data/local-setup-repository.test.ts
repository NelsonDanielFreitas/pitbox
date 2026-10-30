// @vitest-environment node

import { describe, expect, it } from "vitest";

import { CorruptLocalDataError } from "@/db/errors";
import type { SqlParams } from "@/db/protocol";
import type { Team } from "@/features/teams/domain/team";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { openTestDatabase } from "@/test/sqlite";

import { SetupConflictError, SetupInvalidError, SetupNotFoundError } from "../domain/errors";
import { LocalSetupRepository } from "./local-setup-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const carId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70";
const otherCarId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const pilotId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d81";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  for (const [id, owner] of [
    [carId, teamId],
    [otherCarId, otherTeamId],
  ] as const) {
    await db.exec(
      `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
       VALUES ($id, $team, 'Mugen', 'MBX8', 'active', '1/8', 'nitro', 't', 't')`,
      { $id: id, $team: owner },
    );
  }
  let tick = 0;
  const repository = new LocalSetupRepository(db, () => `2026-10-30T12:00:${String(tick++).padStart(2, "0")}.000Z`);
  return { db, repository };
}

const blank = { carId, name: "Pista seca", notes: null, source: null, createdBy: userId };

describe("creating a setup", () => {
  it("starts with an empty first version authored by the user", async () => {
    const { repository } = await setup();
    const { setup: created, version } = await repository.createSetup(teamId, blank);
    expect(created).toMatchObject({ carId, name: "Pista seca", currentVersion: 1, localRev: 0, createdBy: userId });
    expect(version).toMatchObject({ version: 1, data: {}, basedOn: null, createdBy: userId, schemaVersion: 1 });
    expect(await repository.listSetups(teamId)).toHaveLength(1);
  });

  it("copies the data of another setup and remembers where it came from", async () => {
    const { repository } = await setup();
    const first = await repository.createSetup(teamId, blank);
    await repository.createVersion(teamId, first.setup.id, {
      baseVersion: 1,
      data: { diffs: { front_cst: 500 }, bars: { rear_removed: true } },
      changeSummary: "Base",
      createdBy: userId,
    });
    const copy = await repository.createSetup(teamId, {
      ...blank,
      name: "Cópia",
      source: { setupId: first.setup.id, version: 2 },
    });
    expect(copy.version.data).toEqual({ diffs: { front_cst: 500 }, bars: { rear_removed: true } });
    expect(copy.version.basedOn).toEqual({ setupId: first.setup.id, version: 2 });
    expect(copy.version.changeSummary).toBeNull();
    // The original is untouched.
    expect((await repository.getVersion(teamId, first.setup.id))?.data).toEqual(copy.version.data);
  });

  it("can copy an older version", async () => {
    const { repository } = await setup();
    const first = await repository.createSetup(teamId, blank);
    await repository.createVersion(teamId, first.setup.id, {
      baseVersion: 1,
      data: { diffs: { front_cst: 500 } },
      changeSummary: null,
      createdBy: userId,
    });
    const copy = await repository.createSetup(teamId, { ...blank, source: { setupId: first.setup.id, version: 1 } });
    expect(copy.version.data).toEqual({});
  });

  it("refuses a source of another team and leaves nothing behind", async () => {
    const { repository } = await setup();
    const foreign = await repository.createSetup(otherTeamId, { ...blank, carId: otherCarId });
    await expect(
      repository.createSetup(teamId, { ...blank, source: { setupId: foreign.setup.id, version: 1 } }),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
    expect(await repository.listSetups(teamId)).toEqual([]);
  });

  it("refuses a missing source version", async () => {
    const { repository } = await setup();
    const first = await repository.createSetup(teamId, blank);
    await expect(
      repository.createSetup(teamId, { ...blank, source: { setupId: first.setup.id, version: 9 } }),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
    expect(await repository.listSetups(teamId)).toHaveLength(1);
  });

  it("refuses a car of another team or one that does not exist", async () => {
    const { repository } = await setup();
    await expect(repository.createSetup(teamId, { ...blank, carId: otherCarId })).rejects.toBeInstanceOf(
      SetupNotFoundError,
    );
    await expect(
      repository.createSetup(teamId, { ...blank, carId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4dff" }),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
  });

  it("refuses a blank name through the local constraint", async () => {
    const { repository } = await setup();
    await expect(repository.createSetup(teamId, { ...blank, name: "" })).rejects.toMatchObject({
      code: "constraint",
    });
  });
});

describe("saving versions", () => {
  const edit = (baseVersion: number, data: Record<string, unknown>) => ({
    baseVersion,
    data,
    changeSummary: null,
    createdBy: userId,
  });

  it("appends version base+1, moves the setup forward and keeps the old version intact", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    const v2 = await repository.createVersion(teamId, created.id, {
      ...edit(1, { diffs: { front_cst: 500 } }),
      changeSummary: "Óleo mais grosso",
    });
    expect(v2).toMatchObject({ version: 2, changeSummary: "Óleo mais grosso", createdBy: userId });
    expect((await repository.getSetup(teamId, created.id))?.currentVersion).toBe(2);
    expect((await repository.listVersions(teamId, created.id)).map((v) => v.version)).toEqual([2, 1]);
    expect((await repository.getVersion(teamId, created.id, 1))?.data).toEqual({});
    expect((await repository.getVersion(teamId, created.id))?.version).toBe(2);
  });

  it("drops empty groups before storing", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    const v2 = await repository.createVersion(teamId, created.id, edit(1, { bars: {}, diffs: { front_cst: 500 } }));
    expect(v2.data).toEqual({ diffs: { front_cst: 500 } });
  });

  it("refuses a stale base when another writer commits between the check and the write", async () => {
    const { db, repository: other } = await setup();
    const { setup: created } = await other.createSetup(teamId, blank);
    let fired = false;
    // The test executor shares one connection, so the rival write is injected right after
    // the repository has read the current version (the real worker serialises writes).
    const racing = new LocalSetupRepository({
      query: async <T>(sql: string, params?: SqlParams) => {
        const rows = await db.query<T>(sql, params);
        if (!fired && sql.includes("FROM setup_version v")) {
          fired = true;
          await other.createVersion(teamId, created.id, edit(1, { diffs: { front_cst: 600 } }));
        }
        return rows;
      },
      transaction: (statements, options) => db.transaction(statements, options),
    });
    await expect(
      racing.createVersion(teamId, created.id, edit(1, { diffs: { front_cst: 500 } })),
    ).rejects.toMatchObject({ code: "stale" });
    const versions = await other.listVersions(teamId, created.id);
    expect(versions.map((v) => v.version)).toEqual([2, 1]);
    expect(versions[0]?.data).toEqual({ diffs: { front_cst: 600 } });
  });

  it("only the latest version can be the base", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await repository.createVersion(teamId, created.id, edit(1, { diffs: { front_cst: 500 } }));
    await expect(
      repository.createVersion(teamId, created.id, edit(1, { diffs: { front_cst: 600 } })),
    ).rejects.toMatchObject({ code: "stale" });
    await expect(
      repository.createVersion(teamId, created.id, edit(7, { diffs: { front_cst: 600 } })),
    ).rejects.toMatchObject({ code: "stale" });
  });

  it("refuses a version equal to the current one", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await expect(repository.createVersion(teamId, created.id, edit(1, {}))).rejects.toMatchObject({
      code: "no_changes",
    });
    await expect(repository.createVersion(teamId, created.id, edit(1, { bars: {} }))).rejects.toBeInstanceOf(
      SetupConflictError,
    );
  });

  it("refuses data that breaks schema v1, naming the path and rule but never the value", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    const error = await repository
      .createVersion(teamId, created.id, edit(1, { geometry: { front_camber_deg: -99 }, nope: 1 }))
      .catch((e: unknown) => e);
    expect(error).toBeInstanceOf(SetupInvalidError);
    const invalid = error as SetupInvalidError;
    expect(invalid.issues).toEqual([
      { path: "geometry.front_camber_deg", ruleId: "setup.data.range" },
      { path: "nope", ruleId: "setup.data.unknown" },
    ]);
    expect(invalid.message).not.toContain("99");
    expect(await repository.listVersions(teamId, created.id)).toHaveLength(1);
  });

  it("refuses a bar that is both removed and sized", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await expect(
      repository.createVersion(teamId, created.id, edit(1, { bars: { front_removed: true, front_mm: 3 } })),
    ).rejects.toMatchObject({ issues: [{ path: "bars.front_mm", ruleId: "setup.data.bars_conflict" }] });
  });

  it("makes stored versions immutable at the database", async () => {
    const { db, repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await expect(
      db.exec("UPDATE setup_version SET data = '{\"diffs\":{}}' WHERE setup_id = $id", { $id: created.id }),
    ).rejects.toThrow(/immutable/);
  });
});

describe("team isolation", () => {
  it("never shows or changes setups of another team", async () => {
    const { repository } = await setup();
    const { setup: mine } = await repository.createSetup(teamId, blank);
    expect(await repository.getSetup(otherTeamId, mine.id)).toBeUndefined();
    expect(await repository.listSetups(otherTeamId)).toEqual([]);
    expect(await repository.listVersions(otherTeamId, mine.id)).toEqual([]);
    expect(await repository.getVersion(otherTeamId, mine.id)).toBeUndefined();
    await expect(
      repository.createVersion(otherTeamId, mine.id, {
        baseVersion: 1,
        data: { diffs: { front_cst: 500 } },
        changeSummary: null,
        createdBy: userId,
      }),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
    await expect(
      repository.updateSetup(otherTeamId, mine.id, 0, { name: "X", notes: null }),
    ).rejects.toMatchObject({ code: "stale" });
    await expect(
      repository.addFeedback(otherTeamId, mine.id, 1, { ratings: { traction: 3 }, trackZone: null, note: null }, pilotId),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
    expect(await repository.listFeedback(otherTeamId, mine.id)).toEqual([]);
    expect((await repository.getSetup(teamId, mine.id))?.name).toBe("Pista seca");
  });

  it("hides soft-deleted setups", async () => {
    const { db, repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await db.exec("UPDATE setup SET deleted_at = 't' WHERE id = $id", { $id: created.id });
    expect(await repository.getSetup(teamId, created.id)).toBeUndefined();
    expect(await repository.listSetups(teamId)).toEqual([]);
    expect(await repository.getVersion(teamId, created.id)).toBeUndefined();
    expect(await repository.listVersions(teamId, created.id)).toEqual([]);
  });
});

describe("listing and editing", () => {
  it("lists the most recently changed setup first and filters by car", async () => {
    const { db, repository } = await setup();
    await db.exec(
      `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
       VALUES ('0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d72', $team, 'Kyosho', 'MP10', 'spare', '1/8', 'nitro', 't', 't')`,
      { $team: teamId },
    );
    const a = await repository.createSetup(teamId, { ...blank, name: "A" });
    const b = await repository.createSetup(teamId, {
      ...blank,
      name: "B",
      carId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d72",
    });
    expect((await repository.listSetups(teamId)).map((s) => s.name)).toEqual(["B", "A"]);
    await repository.createVersion(teamId, a.setup.id, {
      baseVersion: 1,
      data: { diffs: { front_cst: 500 } },
      changeSummary: null,
      createdBy: userId,
    });
    expect((await repository.listSetups(teamId)).map((s) => s.name)).toEqual(["A", "B"]);
    expect((await repository.listSetups(teamId, b.setup.carId)).map((s) => s.name)).toEqual(["B"]);
  });

  it("renames with optimistic concurrency", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    const renamed = await repository.updateSetup(teamId, created.id, 0, { name: "Molhada", notes: "Chuva" });
    expect(renamed).toMatchObject({ name: "Molhada", notes: "Chuva", localRev: 1, currentVersion: 1 });
    await expect(
      repository.updateSetup(teamId, created.id, 0, { name: "Outra", notes: null }),
    ).rejects.toMatchObject({ code: "stale" });
  });

  it("treats a malformed row as corrupt local data", async () => {
    const { db, repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await db.exec("DROP TRIGGER setup_version_immutable");
    await db.exec("UPDATE setup_version SET schema_version = 0 WHERE setup_id = $id", { $id: created.id });
    await expect(repository.getVersion(teamId, created.id)).rejects.toBeInstanceOf(CorruptLocalDataError);
  });
});

describe("driver feedback", () => {
  const ratings = { steeringEntry: 2, traction: 5 };

  it("stores feedback on a version, authored by the id only", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    const feedback = await repository.addFeedback(
      teamId,
      created.id,
      1,
      { ratings, trackZone: "Curva 3", note: "Sai de frente" },
      pilotId,
    );
    expect(feedback).toMatchObject({
      setupId: created.id,
      version: 1,
      authorId: pilotId,
      ratings,
      trackZone: "Curva 3",
      note: "Sai de frente",
    });
  });

  it("lists newest first and filters by version", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await repository.createVersion(teamId, created.id, {
      baseVersion: 1,
      data: { diffs: { front_cst: 500 } },
      changeSummary: null,
      createdBy: userId,
    });
    await repository.addFeedback(teamId, created.id, 1, { ratings, trackZone: null, note: "v1" }, pilotId);
    await repository.addFeedback(teamId, created.id, 2, { ratings: {}, trackZone: null, note: "v2" }, pilotId);
    await repository.addFeedback(teamId, created.id, 2, { ratings: { braking: 4 }, trackZone: null, note: null }, pilotId);
    expect((await repository.listFeedback(teamId, created.id)).map((f) => f.note)).toEqual([null, "v2", "v1"]);
    expect((await repository.listFeedback(teamId, created.id, 1)).map((f) => f.note)).toEqual(["v1"]);
  });

  it("refuses a version that does not exist", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await expect(
      repository.addFeedback(teamId, created.id, 5, { ratings, trackZone: null, note: null }, pilotId),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
  });

  it("refuses feedback with nothing in it", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await expect(
      repository.addFeedback(teamId, created.id, 1, { ratings: {}, trackZone: "Curva", note: null }, pilotId),
    ).rejects.toMatchObject({ code: "constraint" });
  });

  it("keeps feedback when new versions are saved", async () => {
    const { repository } = await setup();
    const { setup: created } = await repository.createSetup(teamId, blank);
    await repository.addFeedback(teamId, created.id, 1, { ratings, trackZone: null, note: null }, pilotId);
    await repository.createVersion(teamId, created.id, {
      baseVersion: 1,
      data: { diffs: { front_cst: 500 } },
      changeSummary: null,
      createdBy: userId,
    });
    expect(await repository.listFeedback(teamId, created.id, 1)).toHaveLength(1);
  });
});
