import { CorruptLocalDataError } from "@/db/errors";
import { newId } from "@/shared/lib/id";
import { notifyLocalWrite, outboxStatement } from "@/sync/outbox";
import { OpKind } from "@/sync/signing";
import { z } from "zod";

import type { TemplateInfo } from "../domain/models";
import { duplicateTemplate, parseTemplate, type Template } from "../domain/template";

import { defaultSetupDatabase, type SetupDatabase } from "./setup-database";

/** The generic 1/8 nitro template (ADR 0030): what a v1 version and a car without a sheet use. */
export const GENERIC_TEMPLATE_ID = "01a1990b-b4dd-777f-9c5b-e31aaf01355f";

export interface TemplateRepository {
  /** Catalogue plus the team's own templates, by make and model. */
  listTemplates(teamId: string): Promise<TemplateInfo[]>;
  getTemplateInfo(teamId: string, id: string): Promise<TemplateInfo | undefined>;
  /** One immutable version (the current one when omitted); undefined if not stored locally. */
  getTemplate(teamId: string, id: string, version?: number): Promise<Template | undefined>;
  /** Copies a catalogue template into the team (mechanic or owner); works offline. */
  duplicateForTeam(teamId: string, sourceId: string): Promise<TemplateInfo>;
}

const rowSchema = z.object({
  id: z.uuid(),
  team_id: z.uuid().nullable(),
  slug: z.string(),
  make: z.string(),
  model: z.string(),
  scale: z.string(),
  powertrain: z.string(),
  name: z.string(),
  source_url: z.string().nullable(),
  current_version: z.number().int().min(1),
  based_on_id: z.uuid().nullable(),
  truncated: z.number().int(),
  version: z.number().int().min(1),
});

const COLUMNS =
  "id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version, based_on_id, truncated, version";

function infoFrom(row: unknown): TemplateInfo {
  const r = rowSchema.safeParse(row);
  if (!r.success) throw new CorruptLocalDataError("a sheet template row in the local database is invalid");
  const v = r.data;
  return {
    id: v.id,
    teamId: v.team_id,
    slug: v.slug,
    make: v.make,
    model: v.model,
    scale: v.scale,
    powertrain: v.powertrain,
    name: v.name,
    sourceUrl: v.source_url,
    currentVersion: v.current_version,
    basedOnId: v.based_on_id,
    truncated: v.truncated === 1,
    version: v.version,
  };
}

export class LocalTemplateRepository implements TemplateRepository {
  constructor(private readonly db: SetupDatabase = defaultSetupDatabase) {}

  async listTemplates(teamId: string): Promise<TemplateInfo[]> {
    const rows = await this.db.query(
      `SELECT ${COLUMNS} FROM sheet_template
        WHERE (team_id IS NULL OR team_id = $teamId) AND deleted_at IS NULL
        ORDER BY make COLLATE NOCASE, model COLLATE NOCASE, name COLLATE NOCASE`,
      { $teamId: teamId },
    );
    return rows.map(infoFrom);
  }

  async getTemplateInfo(teamId: string, id: string): Promise<TemplateInfo | undefined> {
    const rows = await this.db.query(
      `SELECT ${COLUMNS} FROM sheet_template
        WHERE id = $id AND (team_id IS NULL OR team_id = $teamId) AND deleted_at IS NULL`,
      { $id: id, $teamId: teamId },
    );
    return rows[0] ? infoFrom(rows[0]) : undefined;
  }

  async getTemplate(teamId: string, id: string, version?: number): Promise<Template | undefined> {
    const rows = await this.db.query<{ definition: string; version: number }>(
      `SELECT v.definition, v.version
         FROM sheet_template t
         JOIN sheet_template_version v ON v.template_id = t.id
        WHERE t.id = $id AND (t.team_id IS NULL OR t.team_id = $teamId) AND t.deleted_at IS NULL
          AND v.version = COALESCE($version, t.current_version)`,
      { $id: id, $teamId: teamId, $version: version ?? null },
    );
    const row = rows[0];
    if (!row) return undefined;
    let raw: unknown;
    try {
      raw = JSON.parse(row.definition);
    } catch (error) {
      throw new CorruptLocalDataError("a stored sheet template is not valid JSON", { cause: error });
    }
    const t = parseTemplate(raw);
    if (!t || t.id !== id || t.version !== row.version) {
      throw new CorruptLocalDataError("a stored sheet template is invalid");
    }
    return t;
  }

  async duplicateForTeam(teamId: string, sourceId: string): Promise<TemplateInfo> {
    const info = await this.getTemplateInfo(teamId, sourceId);
    if (!info || info.teamId !== null) throw new CorruptLocalDataError("only a catalogue template can be duplicated");
    const src = await this.getTemplate(teamId, sourceId, info.currentVersion);
    if (!src) throw new CorruptLocalDataError("the template definition is not stored locally");
    const id = newId();
    const copy = duplicateTemplate(src, id);
    if (!copy) throw new CorruptLocalDataError("the template could not be duplicated");
    const now = new Date().toISOString();
    await this.db.transaction([
      {
        sql: `INSERT INTO sheet_template (
                id, team_id, slug, make, model, scale, powertrain, name, source_url,
                current_version, based_on_id, truncated, version, updated_at
              ) VALUES ($id, $teamId, $slug, $make, $model, $scale, $powertrain, $name, $sourceUrl,
                        1, $basedOn, 0, 1, $now)`,
        params: {
          $id: id,
          $teamId: teamId,
          $slug: copy.slug,
          $make: info.make,
          $model: info.model,
          $scale: info.scale,
          $powertrain: info.powertrain,
          $name: info.name,
          $sourceUrl: info.sourceUrl,
          $basedOn: sourceId,
          $now: now,
        },
      },
      {
        sql: "INSERT INTO sheet_template_version (template_id, version, definition) VALUES ($id, 1, $definition)",
        params: { $id: id, $definition: JSON.stringify(copy) },
      },
      outboxStatement({
        teamId,
        entity: "setup.sheet_template",
        entityId: id,
        kind: OpKind.create,
        baseVersion: null,
        fields: { based_on_id: sourceId },
      }),
    ]);
    notifyLocalWrite();
    const created = await this.getTemplateInfo(teamId, id);
    if (!created) throw new CorruptLocalDataError("a duplicated template could not be read back");
    return created;
  }
}

/** The repository on the app's local database; screens take it as an optional prop for tests. */
export const defaultTemplateRepository: TemplateRepository = new LocalTemplateRepository();
