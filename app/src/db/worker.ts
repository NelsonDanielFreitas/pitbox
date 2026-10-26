import SQLiteESMFactory from "wa-sqlite/dist/wa-sqlite.mjs";
import wasmUrl from "wa-sqlite/dist/wa-sqlite.wasm?url";
import { OPFSCoopSyncVFS } from "wa-sqlite/src/examples/OPFSCoopSyncVFS.js";
import * as SQLite from "wa-sqlite";

import { databasePragmas } from "./pragmas";
import { runMigrations } from "./migrations/runner";
import type { DbErrorCode, DbRequest, DbResponse, SqlParams, SqlStatement } from "./protocol";

type SQLiteAPI = ReturnType<typeof SQLite.Factory>;

const ROOT = "pitbox";
const DB_FILE = "db.sqlite3";
const MARKER = ".logout-pending";

let sqlite3: SQLiteAPI | undefined;
let modulePromise: Promise<unknown> | undefined;
let db: number | undefined;
let vfs: OPFSCoopSyncVFS | undefined;
let currentHash: string | undefined;

function fail(code: DbErrorCode, error: unknown): never {
  const message = error instanceof Error ? error.message : String(error);
  const lower = message.toLowerCase();
  const mapped: DbErrorCode = lower.includes("busy") || lower.includes("locked") ? "busy" : code;
  const wrapped = new Error(`${mapped}:${message}`) as Error & { code?: number };
  if (typeof (error as { code?: number } | null)?.code === "number") {
    wrapped.code = (error as { code: number }).code;
  }
  throw wrapped;
}

function responseError(error: unknown): { code: DbErrorCode; message: string } {
  const message = error instanceof Error ? error.message : String(error);
  const codeValue = (error as { code?: number } | null)?.code;
  const [candidate, ...rest] = message.split(":");
  const code: DbErrorCode =
    candidate === "busy" || candidate === "corrupt" || candidate === "unavailable" || candidate === "internal"
      ? candidate
      : "internal";
  return {
    code,
    message: `${codeValue === undefined ? "" : `code ${codeValue}: `}${rest.length > 0 ? rest.join(":") : message}`,
  };
}

async function getModule(): Promise<unknown> {
  modulePromise ??= SQLiteESMFactory({ locateFile: () => wasmUrl });
  return modulePromise;
}

async function getSqlite(): Promise<SQLiteAPI> {
  if (sqlite3) return sqlite3;
  sqlite3 = SQLite.Factory(await getModule());
  return sqlite3;
}

function bindParams(statement: number, params?: SqlParams): void {
  if (params) {
    sqlite3!.bind_collection(statement, params);
  }
}

async function execRaw(sql: string, params?: SqlParams): Promise<void> {
  if (!db) fail("unavailable", new Error("database is not open"));
  const sqlite = await getSqlite();
  if (!params) {
    await sqlite.exec(db, sql);
    return;
  }
  for await (const statement of sqlite.statements(db, sql)) {
    bindParams(statement, params);
    while ((await sqlite.step(statement)) === SQLite.SQLITE_ROW) {
      // A write statement with RETURNING is intentionally ignored here.
    }
  }
}

async function queryRaw(sql: string, params?: SqlParams): Promise<Record<string, unknown>[]> {
  if (!db) fail("unavailable", new Error("database is not open"));
  const sqlite = await getSqlite();
  const rows: Record<string, unknown>[] = [];
  for await (const statement of sqlite.statements(db, sql)) {
    bindParams(statement, params);
    const names = sqlite.column_names(statement);
    while ((await sqlite.step(statement)) === SQLite.SQLITE_ROW) {
      const values = sqlite.row(statement);
      rows.push(Object.fromEntries(names.map((name, index) => [name, values[index]])));
    }
  }
  return rows;
}

async function rootDirectory(): Promise<FileSystemDirectoryHandle> {
  return navigator.storage.getDirectory();
}

async function userDirectory(hash: string, create: boolean): Promise<FileSystemDirectoryHandle> {
  const root = await rootDirectory();
  const pitbox = await root.getDirectoryHandle(ROOT, { create });
  return pitbox.getDirectoryHandle(hash, { create });
}

async function cleanupMarkedDatabases(): Promise<void> {
  const root = await rootDirectory();
  const pitbox = await root.getDirectoryHandle(ROOT, { create: true });
  for await (const [name, handle] of pitbox.entries()) {
    if (handle.kind !== "directory") continue;
    try {
      await handle.getFileHandle(MARKER);
    } catch {
      continue;
    }
    await pitbox.removeEntry(name, { recursive: true });
  }
}


async function closeDatabase(): Promise<void> {
  const sqlite = sqlite3;
  if (sqlite && db !== undefined) {
    try {
      await sqlite.close(db);
    } finally {
      db = undefined;
    }
  }
  vfs?.close();
  vfs = undefined;
  currentHash = undefined;
}

async function openDatabase(hash: string): Promise<void> {
  if (currentHash === hash && db !== undefined) return;
  await closeDatabase();
  await cleanupMarkedDatabases();

  const sqlite = await getSqlite();
  const module = await getModule();
  vfs = await OPFSCoopSyncVFS.create("opfs", module);
  vfs.mxPathname = 255;
  sqlite.vfs_register(vfs as never, true);
  try {
    db = await sqlite.open_v2(
      `${ROOT}/${hash}/${DB_FILE}`,
      SQLite.SQLITE_OPEN_READWRITE | SQLite.SQLITE_OPEN_CREATE,
    );
    currentHash = hash;
    for (const pragma of databasePragmas) {
      await execRaw(pragma);
    }
    const integrity = await queryRaw("PRAGMA quick_check");
    if (integrity[0]?.quick_check !== "ok") {
      fail("corrupt", new Error("quick_check failed"));
    }
    await runMigrations({
      exec: execRaw,
      query: <T = Record<string, unknown>>(sql: string, params?: SqlParams) =>
        queryRaw(sql, params) as Promise<T[]>,
      transaction: withTransaction,
    });
  } catch (error) {
    const detail = vfs?.lastError;
    const suffix = detail instanceof Error ? ` (${detail.name}: ${detail.message})` : "";
    await closeDatabase();
    fail("unavailable", new Error(`${error instanceof Error ? error.message : String(error)}${suffix}`));
  }
}

async function markLogout(hash: string): Promise<void> {
  const directory = await userDirectory(hash, true);
  const marker = await directory.getFileHandle(MARKER, { create: true });
  const writable = await marker.createWritable();
  await writable.write("logout");
  await writable.close();
}

async function deleteUserDatabase(hash: string): Promise<void> {
  await closeDatabase();
  const root = await rootDirectory();
  const pitbox = await root.getDirectoryHandle(ROOT, { create: true });
  try {
    await navigator.locks.request(
      `pitbox-db-${hash}`,
      { mode: "exclusive", signal: AbortSignal.timeout(5000) },
      async () => {
        await pitbox.removeEntry(hash, { recursive: true });
      },
    );
  } catch (error) {
    fail("busy", error);
  }
}

function isBusy(error: unknown): boolean {
  return (
    (typeof (error as { code?: number } | null)?.code === "number" &&
      (error as { code: number }).code === SQLite.SQLITE_BUSY) ||
    /sqlite_busy|database is locked|busy/i.test(error instanceof Error ? error.message : String(error))
  );
}

// OPFSCoopSyncVFS returns SQLITE_BUSY while it waits for an asynchronous Web Lock.
// A transaction is retried whole (with rollback/backoff) instead of leaving the
// caller with a half-applied write. There is deliberately no busy_timeout: its
// synchronous wait blocks the async lock progress.
async function withTransaction(statements: SqlStatement[]): Promise<void> {
  const deadline = Date.now() + 2_000;
  let lastError: unknown;
  for (let attempt = 0; attempt < 10 && Date.now() < deadline; attempt += 1) {
    let began = false;
    try {
      await execRaw("BEGIN IMMEDIATE");
      began = true;
      for (const statement of statements) {
        await execRaw(statement.sql, statement.params);
      }
      await execRaw("COMMIT");
      return;
    } catch (error) {
      lastError = error;
      if (began) {
        try {
          await execRaw("ROLLBACK");
        } catch {
          // The transaction may already have been rolled back by SQLite.
        }
      }
      if (!isBusy(error)) fail("internal", error);
      await new Promise((resolve) => setTimeout(resolve, Math.min(25 * 2 ** attempt, 250)));
    }
  }
  fail("busy", lastError ?? new Error("database busy"));
}

async function handle(request: DbRequest): Promise<DbResponse> {
  try {
    switch (request.type) {
      case "open":
        await openDatabase(request.userHash);
        return { id: request.id, ok: true };
      case "close":
        await closeDatabase();
        return { id: request.id, ok: true };
      case "mark-logout":
        await markLogout(request.userHash);
        return { id: request.id, ok: true };
      case "delete":
        await deleteUserDatabase(request.userHash);
        return { id: request.id, ok: true };
      case "exec":
        await execRaw(request.sql, request.params);
        return { id: request.id, ok: true };
      case "query":
        return { id: request.id, ok: true, rows: await queryRaw(request.sql, request.params) };
      case "transaction":
        await withTransaction(request.statements);
        return { id: request.id, ok: true };
    }
  } catch (error) {
    return { id: request.id, ok: false, error: responseError(error) };
  }
}

self.onmessage = (event: MessageEvent<DbRequest>) => {
  void handle(event.data).then((response) => self.postMessage(response));
};
