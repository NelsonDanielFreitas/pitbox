import type { SqlParams, SqlStatement } from "./protocol";

/**
 * The local database of the signed-in user, whatever the engine underneath (ADR 0033): wa-sqlite
 * on OPFS in the browser, SQLCipher through Capacitor on Android. Screens and repositories never
 * see which one it is; `client.ts` picks it once.
 *
 * Every implementation must behave the same for the callers:
 * - named parameters (`$id`) bound by name, a missing one binds NULL;
 * - `transaction` is all or nothing and returns the rows of its last statement;
 * - failures surface as the errors of `./errors`, never as engine-specific objects;
 * - it fails closed: it never creates an empty database to replace one it could not open.
 */
export interface LocalDatabase {
  /** Read-only: whether this user already has a local database. Never creates one. */
  exists(userId: string): Promise<boolean>;
  open(userId: string): Promise<void>;
  close(): Promise<void>;
  /** Flags the user's data for deletion on the next open, in case the app dies mid-logout. */
  markLogout(userId: string): Promise<void>;
  deleteUserDatabase(userId: string): Promise<void>;
  exec(sql: string, params?: SqlParams): Promise<void>;
  query<T = Record<string, unknown>>(sql: string, params?: SqlParams): Promise<T[]>;
  transaction<T = Record<string, unknown>>(
    statements: SqlStatement[],
    options?: { requireRows?: boolean },
  ): Promise<T[]>;
}
