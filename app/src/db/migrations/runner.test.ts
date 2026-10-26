// @vitest-environment node

import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

import SQLiteESMFactory from "wa-sqlite/dist/wa-sqlite.mjs";
import { MemoryVFS } from "wa-sqlite/src/examples/MemoryVFS.js";
import * as SQLite from "wa-sqlite";
import { describe, expect, it } from "vitest";

import type { SqlStatement } from "../protocol";
import type { SqlExecutor } from "./runner";
import { runMigrations } from "./runner";
import { migrations } from "./index";

const require = createRequire(import.meta.url);

type SQLiteAPI = ReturnType<typeof SQLite.Factory>;

class NodeSqliteExecutor implements SqlExecutor {
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
      "migrations",
      SQLite.SQLITE_OPEN_READWRITE | SQLite.SQLITE_OPEN_CREATE,
      "memory",
    );
    return new NodeSqliteExecutor(sqlite3, db);
  }

  async exec(sql: string, params?: Record<string, string | number | null | Uint8Array>): Promise<void> {
    if (params) {
      for await (const stmt of this.sqlite3.statements(this.db, sql)) {
        this.sqlite3.bind_collection(stmt, params);
        while ((await this.sqlite3.step(stmt)) === SQLite.SQLITE_ROW) {
          // Writes do not return rows.
        }
      }
      return;
    }
    await this.sqlite3.exec(this.db, sql);
  }

  async query<T>(sql: string, params?: Record<string, string | number | null | Uint8Array>): Promise<T[]> {
    const rows: T[] = [];
    for await (const stmt of this.sqlite3.statements(this.db, sql)) {
      if (params) this.sqlite3.bind_collection(stmt, params);
      const names = this.sqlite3.column_names(stmt);
      while ((await this.sqlite3.step(stmt)) === SQLite.SQLITE_ROW) {
        const values = this.sqlite3.row(stmt);
        rows.push(Object.fromEntries(names.map((name: string, index: number) => [name, values[index]])) as T);
      }
    }
    return rows;
  }

  async transaction(statements: SqlStatement[]): Promise<void> {
    await this.exec("BEGIN IMMEDIATE");
    try {
      for (const statement of statements) await this.exec(statement.sql, statement.params);
      await this.exec("COMMIT");
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

describe("runMigrations", () => {
  it("applies v1, preserves its rows, and applies v2", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      await runMigrations(db, [migrations[0]!]);
      await db.exec(
        "INSERT INTO team (id, name, category, country_code, role, updated_at) VALUES ($id, $name, $category, $country, $role, $updatedAt)",
        {
          $id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
          $name: "Equipa Alpha",
          $category: "1/8 TT nitro",
          $country: "PT",
          $role: "owner",
          $updatedAt: "2026-10-25T12:00:00Z",
        },
      );

      await runMigrations(db, [migrations[1]!]);
      const teams = await db.query<{ name: string }>("SELECT name FROM team");
      expect(teams).toEqual([{ name: "Equipa Alpha" }]);
      const applied = await db.query<{ version: number }>(
        "SELECT version FROM schema_migrations ORDER BY version",
      );
      expect(applied.map((row) => row.version)).toEqual([1, 2]);
    } finally {
      await db.close();
    }
  });

  it("is idempotent and rejects a changed migration", async () => {
    const db = await NodeSqliteExecutor.create();
    try {
      await runMigrations(db, [migrations[0]!]);
      await expect(runMigrations(db, [migrations[0]!])).resolves.toBeUndefined();
      await expect(
        runMigrations(db, [{ ...migrations[0]!, sql: `${migrations[0]!.sql}\n-- changed` }]),
      ).rejects.toThrow(/modified/);
    } finally {
      await db.close();
    }
  });
});
