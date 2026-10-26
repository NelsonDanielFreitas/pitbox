import { query, transaction } from "@/db/client";
import { ConnectTeamsRepository } from "@/features/teams/data/connect-teams-repository";
import type { Team } from "@/features/teams/domain/team";

const staleAfterMs = 5 * 60_000;
const syncedAtKey = "teams.synced_at";



function statementsFor(teams: Team[]) {
  const now = new Date().toISOString();
  const statements = [
    { sql: "DELETE FROM team" },
    ...teams.map((team) => ({
      sql: `INSERT INTO team (id, name, category, club, country_code, role, updated_at)
            VALUES ($id, $name, $category, $club, $countryCode, $role, $updatedAt)`,
      params: {
        $id: team.id,
        $name: team.name,
        $category: team.category,
        $club: team.club ?? null,
        $countryCode: team.countryCode,
        $role: team.role,
        $updatedAt: now,
      },
    })),
    {
      sql: "INSERT INTO app_meta (key, value) VALUES ($key, $value) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
      params: { $key: syncedAtKey, $value: now },
    },
  ];
  return statements;
}

async function lastSync(): Promise<number | null> {
  const rows = await query<{ value: string }>("SELECT value FROM app_meta WHERE key = $key", {
    $key: syncedAtKey,
  });
  const value = rows[0]?.value;
  if (!value) return null;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : null;
}

export async function hasLocalTeams(): Promise<boolean> {
  const rows = await query<{ count: number }>(
    "SELECT count(*) AS count FROM team WHERE deleted_at IS NULL",
  );
  return (rows[0]?.count ?? 0) > 0;
}

/**
 * Temporary read-through bootstrap until F1-13. The UI reads the local table;
 * only this module talks to the API for teams.
 */
export async function bootstrapTeams(): Promise<void> {
  const syncedAt = await lastSync();
  if (syncedAt !== null && Date.now() - syncedAt < staleAfterMs) return;

  const remote = await new ConnectTeamsRepository().listMyTeams();
  await transaction(statementsFor(remote));
}
