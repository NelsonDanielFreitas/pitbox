import { query } from "@/db/client";
import { CorruptLocalDataError } from "@/db/errors";
import { localTeamRowSchema } from "@/db/schema";

import type { Team } from "../domain/team";

export class LocalTeamsRepository {
  async listMyTeams(): Promise<Team[]> {
    const rows = await query(
      `SELECT id, name, category, club, country_code, role
         FROM team
        WHERE deleted_at IS NULL
        ORDER BY name COLLATE NOCASE, id`,
    );

    const teams: Team[] = [];
    for (const row of rows) {
      const parsed = localTeamRowSchema.safeParse(row);
      if (!parsed.success) {
        throw new CorruptLocalDataError("a team row in the local database is invalid", {
          cause: parsed.error,
        });
      }
      teams.push({
        id: parsed.data.id,
        name: parsed.data.name,
        category: parsed.data.category,
        club: parsed.data.club ?? undefined,
        countryCode: parsed.data.country_code,
        role: parsed.data.role,
      });
    }
    return teams;
  }
}
