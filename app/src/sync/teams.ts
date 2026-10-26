import { query, transaction } from "@/db/client";
import { ConnectTeamsRepository } from "@/features/teams/data/connect-teams-repository";
import { teamBootstrapStatements, teamsSyncedAtKey } from "./team-statements";

const staleAfterMs = 5 * 60_000;




async function lastSync(): Promise<number | null> {
  const rows = await query<{ value: string }>("SELECT value FROM app_meta WHERE key = $key", {
    $key: teamsSyncedAtKey,
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
export async function bootstrapTeams(force = false): Promise<void> {
  const syncedAt = await lastSync();
  if (!force && syncedAt !== null && Date.now() - syncedAt < staleAfterMs) return;

  const remote = await new ConnectTeamsRepository().listMyTeams();
  await transaction(teamBootstrapStatements(remote));
}
