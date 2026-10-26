import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

import SQLiteESMFactory from "wa-sqlite/dist/wa-sqlite.mjs";
import { MemoryVFS } from "wa-sqlite/src/examples/MemoryVFS.js";
import * as SQLite from "wa-sqlite";

import { LocalConstraintError } from "@/db/errors";
import type { SqlParams, SqlStatement } from "@/db/protocol";
import type { SqlExecutor } from "@/db/migrations/runner";
import { runMigrations } from "@/db/migrations/runner";
import { migrations } from "@/db/migrations";

const require = createRequire(import.meta.url);
type SQLiteAPI = ReturnType<typeof SQLite.Factory>;

export class NodeSqliteExecutor implements SqlExecutor {
  private constructor(
    private readonly sqlite3: SQLiteAPI,
    private readonly db: number,
  ) {}

  static async create(): Promise<NodeSqliteExecutor> {
    const wasmBinary = readFileSync(require.resolve("wa-sqlite/dist/wa-sqlite.wasm"));
    const module = await SQLiteESMFactory({ wasmBinary });
    const sqlite3 = SQLite.Factory(module);
    const vfs = await MemoryVFS.create("memory", module);
    sqlite3.vfs_register(vfs as never, false);
    const db = await sqlite3.open_v2(
      "test",
      SQLite.SQLITE_OPEN_READWRITE | SQLite.SQLITE_OPEN_CREATE,
      "memory",
    );
    return new NodeSqliteExecutor(sqlite3, db);
  }

  async exec(sql: string, params?: SqlParams): Promise<void> {
    if (params) {
      for await (const stmt of this.sqlite3.statements(this.db, sql)) {
        this.sqlite3.bind_collection(stmt, params);
        while ((await this.sqlite3.step(stmt)) === SQLite.SQLITE_ROW) {
          // Writes do not return rows here.
        }
      }
      return;
    }
    await this.sqlite3.exec(this.db, sql);
  }

  async query<T>(sql: string, params?: SqlParams): Promise<T[]> {
    const rows: T[] = [];
    try {
      for await (const stmt of this.sqlite3.statements(this.db, sql)) {
        if (params) this.sqlite3.bind_collection(stmt, params);
        const names = this.sqlite3.column_names(stmt);
        while ((await this.sqlite3.step(stmt)) === SQLite.SQLITE_ROW) {
          const values = this.sqlite3.row(stmt);
          rows.push(
            Object.fromEntries(names.map((name, index) => [name, values[index]])) as T,
          );
        }
      }
    } catch (error) {
      if ((error as { code?: number }).code === SQLite.SQLITE_CONSTRAINT) {
        throw new LocalConstraintError(error instanceof Error ? error.message : String(error), {
          cause: error,
        });
      }
      throw error;
    }
    return rows;
  }

  async transaction<T = Record<string, unknown>>(
    statements: SqlStatement[],
    options?: { requireRows?: boolean },
  ): Promise<T[]> {
    await this.exec("BEGIN IMMEDIATE");
    try {
      let rows: T[] = [];
      for (const statement of statements) {
        rows = await this.query<T>(statement.sql, statement.params);
      }
      if (options?.requireRows && rows.length === 0) {
        throw new LocalConstraintError("transaction required at least one row");
      }
      await this.exec("COMMIT");
      return rows;
    } catch (error) {
      try {
        await this.exec("ROLLBACK");
      } catch {
        // Keep original error.
      }
      throw error;
    }
  }

  async close(): Promise<void> {
    await this.sqlite3.close(this.db);
  }
}

export async function openTestDatabase(): Promise<NodeSqliteExecutor> {
  const db = await NodeSqliteExecutor.create();
  await runMigrations(db, migrations);
  return db;
}
