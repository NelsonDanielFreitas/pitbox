// @vitest-environment node

import { describe, expect, it } from "vitest";

import { migrations } from "@/db/migrations";
import { runMigrations } from "@/db/migrations/runner";
import { databasePragmas } from "@/db/pragmas";
import type { Team } from "@/features/teams/domain/team";
import { syncEntities } from "@/sync/entities";
import { OpKind } from "@/sync/signing";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { NodeSqliteExecutor, openTestDatabase } from "@/test/sqlite";

import { LocalSetupRepository } from "./local-setup-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const carId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const pilotId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d81";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };
const NOW = "2026-10-30T12:00:00.000Z";

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team]));
  await db.exec(
    `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
     VALUES ($id, $t, 'Mugen', 'MBX8', 'active', '1/8', 'nitro', 't', 't')`,
    { $id: carId, $t: teamId },
  );
  const repository = new LocalSetupRepository(db, () => NOW);
  const { setup: created } = await repository.createSetup(teamId, {
    carId,
    name: "Pista seca",
    notes: null,
    source: null,
    createdBy: userId,
  });
  return { db, repository, setupId: created.id };
}

const queued = async (db: Awaited<ReturnType<typeof setup>>["db"]) =>
  (
    await db.query<{ entity_id: string; kind: number; base_version: number | null; fields_json: string }>(
      "SELECT entity_id, kind, base_version, fields_json FROM sync_outbox WHERE entity = 'setup.setup_feedback' ORDER BY rowid",
    )
  ).map((o) => ({ ...o, fields: JSON.parse(o.fields_json) as Record<string, unknown> }));

describe("driver feedback is queued", () => {
  it("queues an append with the scales, the texts and the time, but not the author", async () => {
    const { db, repository, setupId } = await setup();
    try {
      const saved = await repository.addFeedback(
        teamId,
        setupId,
        1,
        { ratings: { traction: 4, braking: 2 }, trackZone: "curva 3", note: "ressalta" },
        pilotId,
      );
      const ops = await queued(db);
      expect(ops).toHaveLength(1);
      expect(ops[0]).toMatchObject({ entity_id: saved.id, kind: OpKind.append, base_version: null });
      expect(ops[0]?.fields).toMatchObject({
        setup_id: setupId,
        version: 1,
        traction: 4,
        braking: 2,
        steering_mid: null,
        track_zone: "curva 3",
        note: "ressalta",
        created_at: NOW,
      });
      expect(ops[0]?.fields).not.toHaveProperty("author_id");
    } finally {
      await db.close();
    }
  });

  it("queues nothing, and stores nothing, for a version that does not exist", async () => {
    const { db, repository, setupId } = await setup();
    try {
      await expect(
        repository.addFeedback(teamId, setupId, 9, { ratings: { traction: 3 }, trackZone: null, note: null }, pilotId),
      ).rejects.toThrow();
      expect(await queued(db)).toHaveLength(0);
      expect(await db.query("SELECT 1 FROM setup_feedback")).toHaveLength(0);
    } finally {
      await db.close();
    }
  });
});

describe("pulling feedback", () => {
  it("adds feedback from another device once, with its author, and skips an unknown version", async () => {
    const { db, repository, setupId } = await setup();
    try {
      const entity = syncEntities["setup.setup_feedback"];
      if (!entity) throw new Error("feedback entity missing");
      const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d99";
      const wire = {
        setup_id: setupId,
        version: 1,
        author_id: pilotId,
        steering_entry: null,
        steering_mid: null,
        steering_exit: null,
        traction: 5,
        jump_stability: null,
        braking: null,
        engine_low_end: null,
        engine_top_end: null,
        track_zone: null,
        note: "boa",
        created_at: NOW,
      };
      await db.transaction(entity.upsert(teamId, id, wire, NOW));
      await db.transaction(entity.upsert(teamId, id, { ...wire, traction: 1 }, NOW));
      const listed = await repository.listFeedback(teamId, setupId);
      expect(listed).toHaveLength(1);
      expect(listed[0]).toMatchObject({ id, authorId: pilotId, note: "boa" });
      expect(listed[0]?.ratings.traction).toBe(5);

      await db.transaction(entity.upsert(teamId, "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d98", { ...wire, version: 7 }, NOW));
      expect(await repository.listFeedback(teamId, setupId)).toHaveLength(1);
    } finally {
      await db.close();
    }
  });
});

describe("0012 sync feedback backfill", () => {
  it("queues the local feedback of live setups, oldest first, once", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      for (const pragma of databasePragmas) await db.exec(pragma);
      await runMigrations(db, migrations.filter((m) => m.version <= 11));
      await db.transaction(teamBootstrapStatements([team]));
      await db.exec(
        `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
         VALUES ($id, $t, 'M', 'X', 'active', '1/8', 'nitro', 't', 't')`,
        { $id: carId, $t: teamId },
      );
      const repository = new LocalSetupRepository(db, () => NOW);
      const live = (await repository.createSetup(teamId, { carId, name: "Viva", notes: null, source: null, createdBy: userId })).setup;
      const gone = (await repository.createSetup(teamId, { carId, name: "Velha", notes: null, source: null, createdBy: userId })).setup;
      // Written before the sync existed: straight into the table, no operation.
      const feedback = (id: string, setupId: string, at: string) =>
        db.exec(
          `INSERT INTO setup_feedback (id, team_id, setup_id, version, author_id, traction, note, created_at)
           VALUES ($id, $t, $s, 1, $a, 3, 'n', $at)`,
          { $id: id, $t: teamId, $s: setupId, $a: pilotId, $at: at },
        );
      await feedback("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e02", live.id, "2026-10-02T10:00:00.000Z");
      await feedback("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e01", live.id, "2026-10-01T10:00:00.000Z");
      await feedback("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4e03", gone.id, "2026-10-01T10:00:00.000Z");
      await db.exec("UPDATE setup SET deleted_at = $n WHERE id = $id", { $n: NOW, $id: gone.id });
      await db.exec("DELETE FROM sync_outbox");

      await runMigrations(db, migrations);

      const ops = await queued(db);
      expect(ops.map((o) => o.entity_id.slice(-2))).toEqual(["01", "02"]);
      expect(ops.every((o) => o.kind === OpKind.append && o.base_version === null)).toBe(true);
      expect(ops[0]?.fields).toMatchObject({ setup_id: live.id, version: 1, traction: 3, note: "n", created_at: "2026-10-01T10:00:00.000Z" });
      expect(ops[0]?.fields).not.toHaveProperty("author_id");
      await runMigrations(db, migrations);
      expect(await queued(db)).toHaveLength(2);
    } finally {
      await db.close();
    }
  });
});
