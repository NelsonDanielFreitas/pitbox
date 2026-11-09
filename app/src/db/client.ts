import type { LocalDatabase } from "./local-database";
import type { SqlParams, SqlStatement } from "./protocol";
import { hashUserId } from "./user-hash";
import { isNativePlatform } from "@/shared/lib/platform";

/**
 * The one door to the local database. It picks the engine once (ADR 0033): SQLCipher through
 * Capacitor inside the Android shell, wa-sqlite on OPFS in a browser. The engines are loaded on
 * demand, so a browser never downloads the native driver and the APK never starts the worker.
 */
let instance: Promise<LocalDatabase> | undefined;

function database(): Promise<LocalDatabase> {
  instance ??= isNativePlatform()
    ? import("./native-database").then((module) => module.createNativeDatabase())
    : import("./web-database").then((module) => new module.WebDatabase());
  return instance;
}

export { hashUserId };

export async function localDatabaseExists(userId: string): Promise<boolean> {
  return (await database()).exists(userId);
}

export async function openDatabase(userId: string): Promise<void> {
  await (await database()).open(userId);
}

export async function closeDatabase(): Promise<void> {
  // Nothing was ever opened: do not load an engine just to close it.
  if (!instance) return;
  await (await instance).close();
}

export async function markLogout(userId: string): Promise<void> {
  await (await database()).markLogout(userId);
}

export async function deleteUserDatabase(userId: string): Promise<void> {
  await (await database()).deleteUserDatabase(userId);
}

export async function exec(sql: string, params?: SqlParams): Promise<void> {
  await (await database()).exec(sql, params);
}

export async function query<T = Record<string, unknown>>(
  sql: string,
  params?: SqlParams,
): Promise<T[]> {
  return (await database()).query<T>(sql, params);
}

export async function transaction<T = Record<string, unknown>>(
  statements: SqlStatement[],
  options?: { requireRows?: boolean },
): Promise<T[]> {
  return (await database()).transaction<T>(statements, options);
}
