import { CorruptLocalDataError, LocalConstraintError } from "@/db/errors";
import type { SqlParams, SqlStatement } from "@/db/protocol";
import { newId } from "@/shared/lib/id";

import { compareSetupData } from "../domain/compare";
import { normalizeData } from "../domain/data";
import { SetupConflictError, SetupInvalidError, SetupNotFoundError } from "../domain/errors";
import {
  feedbackScales,
  SCHEMA_VERSION,
  type Setup,
  type SetupFeedback,
  type SetupFeedbackInput,
  type SetupVersion,
} from "../domain/models";
import {
  feedbackColumns,
  feedbackFromRow,
  setupFeedbackRowSchema,
  setupFromRow,
  setupRowSchema,
  setupVersionRowSchema,
  versionFromRow,
} from "../domain/schemas";
import { validateSetupData } from "../domain/validate";

import { defaultSetupDatabase, type SetupDatabase } from "./setup-database";
import type { CreateSetupInput, CreateVersionInput, SetupRepository } from "./setup-repository";

const SETUP_COLUMNS =
  "id, car_id, name, notes, current_version, created_at, updated_at, created_by, local_rev";

const VERSION_COLUMNS = `
  setup_id, version, data, schema_version, based_on_setup_id, based_on_version,
  change_summary, created_at, created_by
`;

const FEEDBACK_COLUMNS = `
  id, setup_id, version, author_id, ${feedbackScales.map((s) => feedbackColumns[s]).join(", ")},
  track_zone, note, created_at
`;

type Clock = () => string;

/** Parses a row with its schema; anything unexpected is corrupt local data, never shown. */
function parse<T>(
  schema: { safeParse: (value: unknown) => { success: boolean; data?: T; error?: unknown } },
  row: unknown,
): T {
  const result = schema.safeParse(row);
  if (!result.success || result.data === undefined) {
    throw new CorruptLocalDataError("a setup row in the local database is invalid", {
      cause: result.error,
    });
  }
  return result.data;
}

function isNoRows(error: unknown): boolean {
  return error instanceof LocalConstraintError && /required at least one row/i.test(error.message);
}

export class LocalSetupRepository implements SetupRepository {
  constructor(
    private readonly db: SetupDatabase = defaultSetupDatabase,
    private readonly now: Clock = () => new Date().toISOString(),
  ) {}

  async listSetups(teamId: string, carId?: string): Promise<Setup[]> {
    const rows = await this.db.query(
      `SELECT ${SETUP_COLUMNS}
         FROM setup
        WHERE team_id = $teamId AND deleted_at IS NULL
          AND ($carId IS NULL OR car_id = $carId)
        ORDER BY updated_at DESC, name COLLATE NOCASE`,
      { $teamId: teamId, $carId: carId ?? null },
    );
    return rows.map((row) => this.setupFrom(row));
  }

  async getSetup(teamId: string, setupId: string): Promise<Setup | undefined> {
    const rows = await this.db.query(
      `SELECT ${SETUP_COLUMNS}
         FROM setup
        WHERE team_id = $teamId AND id = $id AND deleted_at IS NULL`,
      { $teamId: teamId, $id: setupId },
    );
    return rows[0] ? this.setupFrom(rows[0]) : undefined;
  }

  async listVersions(teamId: string, setupId: string): Promise<SetupVersion[]> {
    const rows = await this.db.query(
      `SELECT ${VERSION_COLUMNS}
         FROM setup_version v
        WHERE v.team_id = $teamId AND v.setup_id = $setupId
          AND EXISTS (SELECT 1 FROM setup s
                       WHERE s.team_id = v.team_id AND s.id = v.setup_id AND s.deleted_at IS NULL)
        ORDER BY version DESC`,
      { $teamId: teamId, $setupId: setupId },
    );
    return rows.map((row) => this.versionFrom(row));
  }

  async getVersion(
    teamId: string,
    setupId: string,
    version?: number,
  ): Promise<SetupVersion | undefined> {
    const rows = await this.db.query(
      `SELECT ${VERSION_COLUMNS}
         FROM setup_version v
        WHERE v.team_id = $teamId AND v.setup_id = $setupId
          AND v.version = COALESCE($version,
                (SELECT current_version FROM setup WHERE team_id = $teamId AND id = $setupId))
          AND EXISTS (SELECT 1 FROM setup s
                       WHERE s.team_id = v.team_id AND s.id = v.setup_id AND s.deleted_at IS NULL)`,
      { $teamId: teamId, $setupId: setupId, $version: version ?? null },
    );
    return rows[0] ? this.versionFrom(rows[0]) : undefined;
  }

  async createSetup(
    teamId: string,
    input: CreateSetupInput,
  ): Promise<{ setup: Setup; version: SetupVersion }> {
    const cars = await this.db.query(
      "SELECT 1 AS found FROM car WHERE team_id = $teamId AND id = $carId AND deleted_at IS NULL",
      { $teamId: teamId, $carId: input.carId },
    );
    if (cars.length === 0) throw new SetupNotFoundError("car not found");

    const id = newId();
    const now = this.now();
    const insertSetup: SqlStatement = {
      sql: `INSERT INTO setup (
              id, team_id, car_id, name, current_version, notes, created_at, updated_at,
              version, local_rev, created_by
            ) VALUES ($id, $teamId, $carId, $name, 1, $notes, $now, $now, 1, 0, $createdBy)`,
      params: {
        $id: id,
        $teamId: teamId,
        $carId: input.carId,
        $name: input.name,
        $notes: input.notes,
        $now: now,
        $createdBy: input.createdBy,
      },
    };
    // With a source, the data is copied inside SQL from a version of this team, so a source
    // of another team (or one that does not exist) selects no row and the whole write rolls back.
    const insertVersion: SqlStatement = input.source
      ? {
          sql: `INSERT INTO setup_version (
                  team_id, setup_id, version, data, schema_version, based_on_setup_id,
                  based_on_version, created_at, created_by
                )
                SELECT $teamId, $id, 1, v.data, v.schema_version, v.setup_id, v.version, $now, $createdBy
                  FROM setup_version v
                 WHERE v.team_id = $teamId AND v.setup_id = $sourceId AND v.version = $sourceVersion
                   AND EXISTS (SELECT 1 FROM setup s
                                WHERE s.team_id = v.team_id AND s.id = v.setup_id
                                  AND s.deleted_at IS NULL)
                RETURNING ${VERSION_COLUMNS}`,
          params: {
            $teamId: teamId,
            $id: id,
            $now: now,
            $createdBy: input.createdBy,
            $sourceId: input.source.setupId,
            $sourceVersion: input.source.version,
          },
        }
      : {
          sql: `INSERT INTO setup_version (
                  team_id, setup_id, version, data, schema_version, created_at, created_by
                ) VALUES ($teamId, $id, 1, '{}', $schemaVersion, $now, $createdBy)
                RETURNING ${VERSION_COLUMNS}`,
          params: {
            $teamId: teamId,
            $id: id,
            $schemaVersion: SCHEMA_VERSION,
            $now: now,
            $createdBy: input.createdBy,
          },
        };
    try {
      const rows = await this.db.transaction([insertSetup, insertVersion], { requireRows: true });
      const setup = await this.getSetup(teamId, id);
      if (!setup || !rows[0]) throw new CorruptLocalDataError("a new setup could not be read back");
      return { setup, version: this.versionFrom(rows[0]) };
    } catch (error) {
      if (isNoRows(error)) throw new SetupNotFoundError("source setup version not found", { cause: error });
      this.rethrowWrite(error);
    }
  }

  async createVersion(
    teamId: string,
    setupId: string,
    input: CreateVersionInput,
  ): Promise<SetupVersion> {
    const issues = validateSetupData(input.data);
    if (issues.length > 0) throw new SetupInvalidError(issues);
    const data = normalizeData(input.data);

    const current = await this.getVersion(teamId, setupId);
    if (!current) throw new SetupNotFoundError();
    if (current.version !== input.baseVersion) {
      throw new SetupConflictError("stale", "setup changed since this version was opened");
    }
    if (compareSetupData(current.data, data).length === 0) {
      throw new SetupConflictError("no_changes", "the new version equals the current one");
    }

    const next = input.baseVersion + 1;
    const now = this.now();
    try {
      const rows = await this.db.transaction(
        [
          // Only the writer that still sees `base` as current moves it forward.
          {
            sql: `UPDATE setup
                     SET current_version = $next, updated_at = $now
                   WHERE team_id = $teamId AND id = $setupId AND current_version = $base
                     AND deleted_at IS NULL`,
            params: {
              $teamId: teamId,
              $setupId: setupId,
              $base: input.baseVersion,
              $next: next,
              $now: now,
            },
          },
          {
            sql: `INSERT INTO setup_version (
                    team_id, setup_id, version, data, schema_version, change_summary,
                    created_at, created_by
                  )
                  SELECT s.team_id, s.id, $next, $data, $schemaVersion, $summary, $now, $createdBy
                    FROM setup s
                   WHERE s.team_id = $teamId AND s.id = $setupId AND s.current_version = $next
                     AND NOT EXISTS (SELECT 1 FROM setup_version x
                                      WHERE x.team_id = s.team_id AND x.setup_id = s.id
                                        AND x.version = $next)
                  RETURNING ${VERSION_COLUMNS}`,
            params: {
              $teamId: teamId,
              $setupId: setupId,
              $next: next,
              $data: JSON.stringify(data),
              $schemaVersion: SCHEMA_VERSION,
              $summary: input.changeSummary,
              $now: now,
              $createdBy: input.createdBy,
            },
          },
        ],
        { requireRows: true },
      );
      if (!rows[0]) throw new CorruptLocalDataError("a new version could not be read back");
      return this.versionFrom(rows[0]);
    } catch (error) {
      if (isNoRows(error)) {
        throw new SetupConflictError("stale", "setup changed in another tab", { cause: error });
      }
      this.rethrowWrite(error);
    }
  }

  async updateSetup(
    teamId: string,
    setupId: string,
    localRev: number,
    input: { name: string; notes: string | null },
  ): Promise<Setup> {
    try {
      const rows = await this.db.transaction(
        [
          {
            sql: `UPDATE setup
                     SET name = $name, notes = $notes, updated_at = $now, local_rev = local_rev + 1
                   WHERE team_id = $teamId AND id = $id AND local_rev = $localRev
                     AND deleted_at IS NULL
                RETURNING ${SETUP_COLUMNS}`,
            params: {
              $teamId: teamId,
              $id: setupId,
              $localRev: localRev,
              $name: input.name,
              $notes: input.notes,
              $now: this.now(),
            },
          },
        ],
        { requireRows: true },
      );
      if (!rows[0]) throw new CorruptLocalDataError("an updated setup could not be read back");
      return this.setupFrom(rows[0]);
    } catch (error) {
      if (isNoRows(error)) {
        throw new SetupConflictError("stale", "setup changed in another tab", { cause: error });
      }
      this.rethrowWrite(error);
    }
  }

  async addFeedback(
    teamId: string,
    setupId: string,
    version: number,
    input: SetupFeedbackInput,
    authorId: string,
  ): Promise<SetupFeedback> {
    const columns = feedbackScales.map((scale) => feedbackColumns[scale]);
    const params: SqlParams = {
      $teamId: teamId,
      $setupId: setupId,
      $version: version,
      $id: newId(),
      $authorId: authorId,
      $zone: input.trackZone,
      $note: input.note,
      $now: this.now(),
    };
    for (const scale of feedbackScales) params[`$${feedbackColumns[scale]}`] = input.ratings[scale] ?? null;
    try {
      const rows = await this.db.transaction(
        [
          {
            sql: `INSERT INTO setup_feedback (
                    id, team_id, setup_id, version, author_id, ${columns.join(", ")},
                    track_zone, note, created_at
                  )
                  SELECT $id, v.team_id, v.setup_id, v.version, $authorId,
                         ${columns.map((c) => `$${c}`).join(", ")}, $zone, $note, $now
                    FROM setup_version v
                   WHERE v.team_id = $teamId AND v.setup_id = $setupId AND v.version = $version
                     AND EXISTS (SELECT 1 FROM setup s
                                  WHERE s.team_id = v.team_id AND s.id = v.setup_id
                                    AND s.deleted_at IS NULL)
                  RETURNING ${FEEDBACK_COLUMNS}`,
            params,
          },
        ],
        { requireRows: true },
      );
      if (!rows[0]) throw new CorruptLocalDataError("new feedback could not be read back");
      return this.feedbackFrom(rows[0]);
    } catch (error) {
      if (isNoRows(error)) throw new SetupNotFoundError("setup version not found", { cause: error });
      this.rethrowWrite(error);
    }
  }

  async listFeedback(teamId: string, setupId: string, version?: number): Promise<SetupFeedback[]> {
    const rows = await this.db.query(
      `SELECT ${FEEDBACK_COLUMNS}
         FROM setup_feedback f
        WHERE f.team_id = $teamId AND f.setup_id = $setupId
          AND ($version IS NULL OR f.version = $version)
          AND EXISTS (SELECT 1 FROM setup s
                       WHERE s.team_id = f.team_id AND s.id = f.setup_id AND s.deleted_at IS NULL)
        ORDER BY f.created_at DESC, f.id DESC`,
      { $teamId: teamId, $setupId: setupId, $version: version ?? null },
    );
    return rows.map((row) => this.feedbackFrom(row));
  }

  private setupFrom(row: unknown): Setup {
    return setupFromRow(parse(setupRowSchema, row));
  }

  private versionFrom(row: unknown): SetupVersion {
    const parsed = parse(setupVersionRowSchema, row);
    try {
      return versionFromRow(parsed);
    } catch (error) {
      throw new CorruptLocalDataError("stored setup data is invalid", { cause: error });
    }
  }

  private feedbackFrom(row: unknown): SetupFeedback {
    return feedbackFromRow(parse(setupFeedbackRowSchema, row));
  }

  private rethrowWrite(error: unknown): never {
    if (error instanceof LocalConstraintError) {
      throw new SetupConflictError("constraint", "local setup constraint", { cause: error });
    }
    throw error;
  }
}
