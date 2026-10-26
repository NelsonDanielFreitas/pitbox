import type { SqlStatement } from "@/db/protocol";

export const teamsSyncedAtKey = "teams.synced_at";
import type { Team } from "@/features/teams/domain/team";

export function teamBootstrapStatements(teams: Team[]): SqlStatement[] {
  const now = new Date().toISOString();
  const ids = JSON.stringify(teams.map((team) => team.id));
  return [
    ...teams.map((team) => ({
      sql: `INSERT INTO team (id, name, category, club, country_code, role, created_at, updated_at, version, deleted_at)
            VALUES ($id, $name, $category, $club, $countryCode, $role, $createdAt, $updatedAt, 1, NULL)
            ON CONFLICT(id) DO UPDATE SET
              name = excluded.name,
              category = excluded.category,
              club = excluded.club,
              country_code = excluded.country_code,
              role = excluded.role,
              updated_at = excluded.updated_at,
              deleted_at = NULL`,
      params: {
        $id: team.id,
        $name: team.name,
        $category: team.category,
        $club: team.club ?? null,
        $countryCode: team.countryCode,
        $role: team.role,
        $createdAt: now,
        $updatedAt: now,
      },
    })),
    {
      // Soft-delete teams that disappeared. A hard delete would cascade into local
      // cars/engines/setups and destroy offline work; F1-13 will turn removal into
      // a server tombstone.
      sql: "UPDATE team SET deleted_at = $now WHERE id NOT IN (SELECT value FROM json_each($ids))",
      params: { $now: now, $ids: ids },
    },
    {
      sql: "INSERT INTO app_meta (key, value) VALUES ($key, $value) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
      params: { $key: teamsSyncedAtKey, $value: now },
    },
  ];
}
