import { defaultSyncDatabase, type SyncDatabase } from "./database";

/**
 * Local tables that never reach the server, by design: the draft of a stock count is only of
 * this device (ADR 0023) until it is applied (which creates synced stock adjustments).
 * Erasing the local database at logout would lose it for good, so while it has a row the
 * logout is refused. (The driver's feedback syncs since ADR 0026, so it is not listed.)
 */
export const localOnlyTables = [
  "inventory_count",
  "inventory_count_line",
] as const;

export class UnsyncedChangesError extends Error {
  constructor(readonly reason: "pending" | "local_only") {
    super("there is local data that was not synchronised");
  }
}

/** Throws UnsyncedChangesError when erasing the local database would lose something. */
export async function assertSafeToErase(db: SyncDatabase = defaultSyncDatabase): Promise<void> {
  const pending = await db.query<{ n: number }>(
    "SELECT count(*) AS n FROM sync_outbox WHERE state = 'pending'",
  );
  if ((pending[0]?.n ?? 0) > 0) throw new UnsyncedChangesError("pending");
  // The names are this file's own constants, never input.
  const counts = localOnlyTables.map((table) => `(SELECT count(*) FROM ${table})`).join(" + ");
  const local = await db.query<{ n: number }>(`SELECT ${counts} AS n`);
  if ((local[0]?.n ?? 0) > 0) throw new UnsyncedChangesError("local_only");
}
