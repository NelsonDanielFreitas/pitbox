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
  const mapped: DbErrorCode =
    lower.includes("busy") || lower.includes("locked")
      ? "busy"
      : (error as { code?: number } | null)?.code === SQLite.SQLITE_CONSTRAINT
        ? "constraint"
        : code;
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
    codeValue === SQLite.SQLITE_CONSTRAINT
      ? "constraint"
      : candidate === "busy" ||
          candidate === "corrupt" ||
          candidate === "unavailable" ||
          candidate === "constraint" ||
          candidate === "internal"
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
    await navigator.locks.request(`pitbox-migrate-${hash}`, { mode: "exclusive" }, async () => {
      await runMigrations({
        exec: execRaw,
        query: <T = Record<string, unknown>>(sql: string, params?: SqlParams) =>
          queryRaw(sql, params) as Promise<T[]>,
        transaction: withTransaction,
      });
    });
  } catch (error) {
    const detail = vfs?.lastError;
    const suffix = detail instanceof Error ? ` (${detail.name}: ${detail.message})` : "";
    await closeDatabase();
    if (error instanceof Error && error.message.startsWith("corrupt:")) {
      throw error;
    }
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
  const lockName = `ahp:/${ROOT}/${hash}/${DB_FILE}`;
  try {
    await navigator.locks.request(
      lockName,
      { mode: "exclusive", signal: AbortSignal.timeout(5000) },
      async () => {
        const deadline = Date.now() + 3_000;
        let delay = 25;
        for (;;) {
          try {
            await pitbox.removeEntry(hash, { recursive: true });
            return;
          } catch (error) {
            const name = error instanceof DOMException ? error.name : "";
            if (
              Date.now() >= deadline ||
              (name !== "NoModificationAllowedError" &&
                name !== "InvalidModificationError" &&
                name !== "NotFoundError")
            ) {
              throw error;
            }
            await new Promise((resolve) => setTimeout(resolve, delay));
            delay = Math.min(delay * 2, 250);
          }
        }
      },
    );
  } catch (error) {
    if (error instanceof DOMException && error.name === "NotFoundError") return;
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
async function withTransaction(
  statements: SqlStatement[],
  requireRows = false,
): Promise<Record<string, unknown>[]> {
  const deadline = Date.now() + 2_000;
  let lastError: unknown;
  for (let attempt = 0; attempt < 10 && Date.now() < deadline; attempt += 1) {
    let began = false;
    try {
      await execRaw("BEGIN IMMEDIATE");
      began = true;
      let rows: Record<string, unknown>[] = [];
      for (const statement of statements) {
        rows = await queryRaw(statement.sql, statement.params);
      }
      if (requireRows && rows.length === 0) {
        fail("constraint", new Error("transaction required at least one row"));
      }
      await execRaw("COMMIT");
      return rows;
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
        return {
          id: request.id,
          ok: true,
          rows: await withTransaction(request.statements, request.requireRows),
        };
    }
  } catch (error) {
    return { id: request.id, ok: false, error: responseError(error) };
  }
}

let tail: Promise<void> = Promise.resolve();

// JavaScript async functions are not a mutex: every await yields. The worker has one
// SQLite connection, so all messages must run one at a time. A promise chain is the
// mutex; otherwise a query can run inside another request's transaction.
self.onmessage = (event: MessageEvent<DbRequest>) => {
  const request = event.data;
  tail = tail
    .then(() => handle(request))
    .then((response) => {
      self.postMessage(response);
    })
    .catch((error: unknown) => {
      self.postMessage({ id: request.id, ok: false, error: responseError(error) });
    });
};
