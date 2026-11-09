import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

import SQLiteESMFactory from "wa-sqlite/dist/wa-sqlite.mjs";
import { MemoryVFS } from "wa-sqlite/src/examples/MemoryVFS.js";
import * as SQLite from "wa-sqlite";

import type { NativeSqliteDriver, Row } from "@/db/native-database";
import type { SqlValue } from "@/db/sql-params";

const require = createRequire(import.meta.url);
type SQLiteAPI = ReturnType<typeof SQLite.Factory>;

interface FileInfo {
  /** The passphrase the file was created with: another one cannot open it (like SQLCipher). */
  secret: string;
  encrypted: boolean;
  /** Bumped on delete so a re-created file starts empty (the memory VFS keeps files by name). */
  generation: number;
}

/**
 * A `NativeSqliteDriver` over wa-sqlite in memory, for the tests of `NativeDatabase`. It speaks
 * the same positional contract as the Capacitor driver, so the logic above it is exercised for
 * real (SQL execution, constraints, transactions, RETURNING). What it cannot test is the plugin.
 */
export class MemoryNativeDriver implements NativeSqliteDriver {
  secret: string | undefined;
  secretSets = 0;
  readonly files = new Map<string, FileInfo>();
  readonly calls: string[] = [];
  private nextGeneration = 1;
  private db: number | undefined;
  /** Test hook: what `list()` returns instead of the real file names. */
  listOverride: string[] | undefined;

  private constructor(private readonly sqlite3: SQLiteAPI) {}

  static async create(): Promise<MemoryNativeDriver> {
    const wasmBinary = readFileSync(require.resolve("wa-sqlite/dist/wa-sqlite.wasm"));
    // wa-sqlite's own typings also declare this module (as Promise<any>); which declaration wins
    // depends on file load order, so the type is pinned here.
    const module: unknown = await SQLiteESMFactory({ wasmBinary });
    const sqlite3 = SQLite.Factory(module);
    const vfs = await MemoryVFS.create("memory", module);
    Object.assign(vfs, { mxPathname: 255 }); // "pitbox_<64 hex>" is longer than the default of 64, as in worker.ts
    sqlite3.vfs_register(vfs as never, false);
    return new MemoryNativeDriver(sqlite3);
  }

  ensureSecret(): Promise<void> {
    if (this.secret === undefined) {
      this.secret = `secret-${this.secretSets + 1}`;
      this.secretSets += 1;
    }
    return Promise.resolve();
  }

  clearSecret(): Promise<void> {
    this.secret = undefined;
    this.calls.push("clearSecret");
    return Promise.resolve();
  }

  async open(name: string): Promise<void> {
    const secret = this.secret;
    if (secret === undefined) throw new Error("no passphrase stored");
    let info = this.files.get(name);
    if (!info) {
      info = { secret, encrypted: true, generation: this.nextGeneration++ };
      this.files.set(name, info);
    } else if (info.encrypted && info.secret !== secret) {
      throw new Error("file is not a database"); // what SQLCipher says for a wrong key
    }
    this.db = await this.sqlite3.open_v2(
      `${name}.g${info.generation}`,
      SQLite.SQLITE_OPEN_READWRITE | SQLite.SQLITE_OPEN_CREATE,
      "memory",
    );
  }

  isEncrypted(name: string): Promise<boolean> {
    return Promise.resolve(this.files.get(name)?.encrypted === true);
  }

  async close(): Promise<void> {
    if (this.db !== undefined) await this.sqlite3.close(this.db);
    this.db = undefined;
  }

  exists(name: string): Promise<boolean> {
    return Promise.resolve(this.files.has(name));
  }

  async delete(name: string): Promise<void> {
    this.calls.push(`delete:${name}`);
    this.files.delete(name);
    return Promise.resolve();
  }

  list(): Promise<string[]> {
    return Promise.resolve(this.listOverride ?? [...this.files.keys()]);
  }

  async script(sql: string): Promise<void> {
    await this.sqlite3.exec(this.handle(), sql);
  }

  read(sql: string, values: SqlValue[]): Promise<Row[]> {
    return this.run(sql, values);
  }

  async write(sql: string, values: SqlValue[], returning: boolean): Promise<Row[]> {
    const rows = await this.run(sql, values);
    return returning ? rows : [];
  }

  async begin(): Promise<void> {
    await this.sqlite3.exec(this.handle(), "BEGIN IMMEDIATE");
  }

  async commit(): Promise<void> {
    await this.sqlite3.exec(this.handle(), "COMMIT");
  }

  async rollback(): Promise<void> {
    await this.sqlite3.exec(this.handle(), "ROLLBACK");
  }

  private handle(): number {
    if (this.db === undefined) throw new Error("no open connection");
    return this.db;
  }

  private async run(sql: string, values: SqlValue[]): Promise<Row[]> {
    const rows: Row[] = [];
    for await (const stmt of this.sqlite3.statements(this.handle(), sql)) {
      if (values.length > 0) this.sqlite3.bind_collection(stmt, values);
      const names = this.sqlite3.column_names(stmt);
      while ((await this.sqlite3.step(stmt)) === SQLite.SQLITE_ROW) {
        const row = this.sqlite3.row(stmt);
        rows.push(Object.fromEntries(names.map((name, i) => [name, row[i]])));
      }
    }
    return rows;
  }
}
