import { query, transaction } from "@/db/client";
import type { SqlParams, SqlStatement } from "@/db/protocol";

/** What the sync engine needs from the local database (the test SQLite implements it too). */
export interface SyncDatabase {
  query<T = Record<string, unknown>>(sql: string, params?: SqlParams): Promise<T[]>;
  transaction<T = Record<string, unknown>>(
    statements: SqlStatement[],
    options?: { requireRows?: boolean },
  ): Promise<T[]>;
}

export const defaultSyncDatabase: SyncDatabase = {
  query,
  transaction: <T>(statements: SqlStatement[], options?: { requireRows?: boolean }) =>
    transaction<T>(statements, options),
};
