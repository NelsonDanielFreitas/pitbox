// @vitest-environment node

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { catalogStatements, syncEntities } from "@/sync/entities";
import { teamBootstrapStatements } from "@/sync/team-statements";
import { openTestDatabase } from "@/test/sqlite";

import { SetupInvalidError, SetupNotFoundError } from "../domain/errors";
import { LocalGarageRepository } from "@/features/garage/data/local-garage-repository";
import { GarageNotFoundError } from "@/features/garage/domain/errors";
import { LocalSetupRepository } from "./local-setup-repository";
import { LocalTemplateRepository } from "./template-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const otherTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const carId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70";
const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80";
const tplA = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a01";
const team: Team = { id: teamId, name: "A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

const vectors = JSON.parse(
  readFileSync(
    fileURLToPath(new URL("../../../../../backend/internal/setup/domain/testdata/template/vectors.json", import.meta.url)),
    "utf8",
  ),
) as { templates: { a: Record<string, unknown> } };

function snapshot(truncated = false) {
  const a = vectors.templates.a;
  return {
    scales: [],
    powertrains: [],
    sheet_templates: [
      {
        id: tplA, slug: "model-a", make: "Acme", model: "A8", scale: "1/8", powertrain: "nitro",
        name: "Acme A8", source_url: null, current_version: 1, truncated,
        versions: truncated ? [] : [{ version: 1, definition: a }],
      },
    ],
  };
}

async function setup() {
  const db = await openTestDatabase();
  await db.transaction(teamBootstrapStatements([team, { ...team, id: otherTeamId, name: "B" }]));
  await db.exec(
    `INSERT INTO car (id, team_id, make, model, status, scale, powertrain, created_at, updated_at)
     VALUES ($id, $team, 'Acme', 'A8', 'active', '1/8', 'nitro', 't', 't')`,
    { $id: carId, $team: teamId },
  );
  await db.transaction(catalogStatements(snapshot(), "2026-11-01T10:00:00.000Z"));
  let tick = 0;
  const repository = new LocalSetupRepository(db, () => `2026-11-01T12:00:${String(tick++).padStart(2, "0")}.000Z`);
  return { db, repository, templates: new LocalTemplateRepository(db) };
}

const outbox = (db: Awaited<ReturnType<typeof setup>>["db"]) =>
  db.query<{ entity: string; fields_json: string }>("SELECT entity, fields_json FROM sync_outbox ORDER BY created_at, rowid");

describe("the catalogue snapshot", () => {
  it("stores templates once and never rewrites a published version", async () => {
    const { db, templates } = await setup();
    expect((await templates.listTemplates(teamId)).map((t) => t.id)).toEqual([tplA]);
    const changed = snapshot();
    const version = changed.sheet_templates[0]?.versions[0];
    if (version) version.definition = { ...vectors.templates.a, name: { pt: "Alterado" } };
    await db.transaction(catalogStatements(changed, "2026-11-02T10:00:00.000Z"));
    expect((await templates.getTemplate(teamId, tplA, 1))?.name.pt).toBe("Acme A8");
  });

  it("lists a truncated template without its definition", async () => {
    const db = await openTestDatabase();
    await db.transaction(teamBootstrapStatements([team]));
    await db.transaction(catalogStatements(snapshot(true)));
    const templates = new LocalTemplateRepository(db);
    expect((await templates.listTemplates(teamId))[0]?.truncated).toBe(true);
    expect(await templates.getTemplate(teamId, tplA, 1)).toBeUndefined();
  });

  it("does not let a team row take over an id, nor show it to another team", async () => {
    const { db, templates } = await setup();
    await db.transaction(
      syncEntities["setup.sheet_template"]?.upsert(otherTeamId, tplA, { ...snapshot().sheet_templates[0], based_on_id: null, version: 1 }, "now") ?? [],
    );
    expect((await templates.getTemplateInfo(teamId, tplA))?.teamId).toBeNull();
  });
});

describe("a setup on a template", () => {
  it("starts as v2, queues the template, and saves validated values without canonical", async () => {
    const { db, repository } = await setup();
    const { setup: s, version } = await repository.createSetup(teamId, {
      carId, name: "Terra", notes: null, source: null, createdBy: userId, template: { id: tplA, version: 1 },
    });
    expect(version).toMatchObject({ schemaVersion: 2, data: { template: { id: tplA, version: 1 }, values: {} } });
    const saved = await repository.createVersion(teamId, s.id, {
      baseVersion: 1, changeSummary: null, createdBy: userId,
      data: { template: { id: tplA, version: 1 }, values: { front_cst: 500, tower: "3" }, canonical: { "diffs.front_cst": 1 } },
    });
    expect(saved.schemaVersion).toBe(2);
    expect(saved.data).not.toHaveProperty("canonical");
    const ops = await outbox(db);
    expect(JSON.parse(ops[0]?.fields_json ?? "{}")).toMatchObject({ sheet_template_id: tplA, sheet_template_version: 1 });
    const sent = JSON.parse(ops[1]?.fields_json ?? "{}") as { schema_version: number; data: Record<string, unknown> };
    expect(sent.schema_version).toBe(2);
    expect(sent.data).not.toHaveProperty("canonical");
  });

  it("refuses unknown keys and out-of-range values without echoing them", async () => {
    const { repository } = await setup();
    const { setup: s } = await repository.createSetup(teamId, {
      carId, name: "T", notes: null, source: null, createdBy: userId, template: { id: tplA, version: 1 },
    });
    const bad = repository.createVersion(teamId, s.id, {
      baseVersion: 1, changeSummary: null, createdBy: userId,
      data: { template: { id: tplA, version: 1 }, values: { secret_key_zzz: 1 } },
    });
    await expect(bad).rejects.toBeInstanceOf(SetupInvalidError);
    await expect(bad).rejects.not.toThrow(/secret_key_zzz/);
    await expect(
      repository.createVersion(teamId, s.id, {
        baseVersion: 1, changeSummary: null, createdBy: userId,
        data: { template: { id: tplA, version: 1 }, values: { front_cst: 1 } },
      }),
    ).rejects.toBeInstanceOf(SetupInvalidError);
  });

  it("refuses an unchanged v2 version and a return to v1", async () => {
    const { repository } = await setup();
    const { setup: s } = await repository.createSetup(teamId, {
      carId, name: "T", notes: null, source: null, createdBy: userId, template: { id: tplA, version: 1 },
    });
    await expect(
      repository.createVersion(teamId, s.id, {
        baseVersion: 1, changeSummary: null, createdBy: userId, data: { template: { id: tplA, version: 1 }, values: {} },
      }),
    ).rejects.toMatchObject({ code: "no_changes" });
    await expect(
      repository.createVersion(teamId, s.id, {
        baseVersion: 1, changeSummary: null, createdBy: userId, data: {}, schemaVersion: 1,
      }),
    ).rejects.toBeInstanceOf(SetupInvalidError);
  });

  it("does not use a template the team cannot see", async () => {
    const { db, repository } = await setup();
    const other = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a77";
    await db.exec(
      `INSERT INTO sheet_template (id, team_id, slug, make, model, scale, powertrain, name, current_version, updated_at)
       VALUES ($id, $t, 'priv', 'X', 'Y', '1/8', 'nitro', 'Priv', 1, 'now')`,
      { $id: other, $t: otherTeamId },
    );
    await db.exec("INSERT INTO sheet_template_version (template_id, version, definition) VALUES ($id, 1, '{}')", { $id: other });
    await expect(
      repository.createSetup(teamId, { carId, name: "T", notes: null, source: null, createdBy: userId, template: { id: other, version: 1 } }),
    ).rejects.toBeInstanceOf(SetupNotFoundError);
  });

  it("leaves v1 setups working", async () => {
    const { repository } = await setup();
    const { setup: s, version } = await repository.createSetup(teamId, { carId, name: "Old", notes: null, source: null, createdBy: userId });
    expect(version.schemaVersion).toBe(1);
    const next = await repository.createVersion(teamId, s.id, {
      baseVersion: 1, changeSummary: null, createdBy: userId, data: { diffs: { front_cst: 500 } },
    });
    expect(next).toMatchObject({ schemaVersion: 1, data: { diffs: { front_cst: 500 } } });
  });
});

describe("duplicating a template for the team", () => {
  it("copies it offline with the slug the server computes, and queues the create", async () => {
    const { db, templates } = await setup();
    const copy = await templates.duplicateForTeam(teamId, tplA);
    expect(copy).toMatchObject({ teamId, basedOnId: tplA, currentVersion: 1 });
    expect(copy.slug).toBe(`model-a-${copy.id.replaceAll("-", "").slice(24)}`);
    expect((await templates.getTemplate(teamId, copy.id))?.id).toBe(copy.id);
    expect(await outbox(db)).toEqual([{ entity: "setup.sheet_template", fields_json: JSON.stringify({ based_on_id: tplA }) }]);
    // A team template is not duplicated again, and another team does not see it.
    await expect(templates.duplicateForTeam(teamId, copy.id)).rejects.toThrow();
    expect(await templates.getTemplateInfo(otherTeamId, copy.id)).toBeUndefined();
  });
});

describe("linking a car to a sheet", () => {
  const carInput = { make: "Acme", model: "A9", modelYear: null, serialNumber: null, nickname: null, transponderNumber: null, status: "active" as const, notes: null };

  it("links a visible template, queues it, and clears it with null", async () => {
    const { db } = await setup();
    const garage = new LocalGarageRepository(db);
    const car = await garage.createCar(teamId, { ...carInput, sheetTemplateId: tplA });
    expect(car.sheetTemplateId).toBe(tplA);
    expect(JSON.parse((await outbox(db))[0]?.fields_json ?? "{}")).toMatchObject({ sheet_template_id: tplA });
    // Not mentioning the sheet leaves it alone; null clears it and queues the change.
    const kept = await garage.updateCar(teamId, car.id, car.localRev, { ...carInput, nickname: "x" });
    expect(kept.sheetTemplateId).toBe(tplA);
    const cleared = await garage.updateCar(teamId, car.id, kept.localRev, { ...carInput, nickname: "x", sheetTemplateId: null });
    expect(cleared.sheetTemplateId).toBeNull();
    const ops = (await outbox(db)).map((o) => JSON.parse(o.fields_json) as Record<string, unknown>);
    expect(ops.at(-1)).toEqual({ sheet_template_id: null });
  });

  it("refuses a private template of another team", async () => {
    const { db } = await setup();
    const other = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a78";
    await db.exec(
      `INSERT INTO sheet_template (id, team_id, slug, make, model, scale, powertrain, name, current_version, updated_at)
       VALUES ($id, $t, 'priv', 'X', 'Y', '1/8', 'nitro', 'Priv', 1, 'now')`,
      { $id: other, $t: otherTeamId },
    );
    const garage = new LocalGarageRepository(db);
    await expect(garage.createCar(teamId, { ...carInput, sheetTemplateId: other })).rejects.toBeInstanceOf(GarageNotFoundError);
  });
});
