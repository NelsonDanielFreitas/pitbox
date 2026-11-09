import {
  CapacitorSQLite,
  SQLiteConnection,
  type SQLiteDBConnection,
} from "@capacitor-community/sqlite";

import type { NativeSqliteDriver, Row } from "./native-database";
import type { SqlValue } from "./sql-params";

const SECRET_BYTES = 32;
const SCHEMA_VERSION = 1; // the plugin's own upgrade mechanism is not used: our runner migrates

/** 32 bytes from the CSPRNG as base64url. Generated here, handed to the plugin, kept nowhere else. */
function newSecret(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(SECRET_BYTES));
  return btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

/** The plugin lists files (`<name>SQLite.db` on Android); the rest of the app uses the bare name. */
function bareName(file: string): string {
  return file.replace(/SQLite\.db$/, "").replace(/\.db$/, "");
}

/**
 * Forwards to `@capacitor-community/sqlite` with SQLCipher in `secret` mode (ADR 0033): the
 * passphrase lives inside the plugin, wrapped by the Android Keystore, and this code only ever
 * sees it once, when it creates it.
 *
 * This file is deliberately a thin shell with no decisions in it. Everything it calls was read
 * from the plugin's typings and Java sources, but none of it has run on a device yet; the logic
 * that matters is in `NativeDatabase`, which has tests. Verify on a device or emulator: that
 * `run` with `transaction: false` does not open a transaction of its own, that `query` and
 * `run(..., "all")` return rows as plain objects, and the format of `getDatabaseList`.
 */
export class CapacitorSqliteDriver implements NativeSqliteDriver {
  private readonly sqlite = new SQLiteConnection(CapacitorSQLite);
  private connection: SQLiteDBConnection | undefined;

  async ensureSecret(): Promise<void> {
    const { result } = await this.sqlite.isSecretStored();
    if (!result) await this.sqlite.setEncryptionSecret(newSecret());
  }

  async clearSecret(): Promise<void> {
    await this.sqlite.clearEncryptionSecret();
  }

  async open(name: string): Promise<void> {
    const known = (await this.sqlite.isConnection(name, false)).result === true;
    const connection = known
      ? await this.sqlite.retrieveConnection(name, false)
      : await this.sqlite.createConnection(name, true, "secret", SCHEMA_VERSION, false);
    await connection.open();
    this.connection = connection;
  }

  async isEncrypted(name: string): Promise<boolean> {
    return (await this.sqlite.isDatabaseEncrypted(name)).result === true;
  }

  async close(name: string): Promise<void> {
    const connection = this.connection;
    this.connection = undefined;
    if (connection) await connection.close();
    await this.sqlite.closeConnection(name, false);
  }

  async exists(name: string): Promise<boolean> {
    return (await this.sqlite.isDatabase(name)).result === true;
  }

  async delete(name: string): Promise<void> {
    // The plugin only deletes through a registered connection (it opens the file with the key,
    // closes it and removes it), so one is registered first. Seen on a phone: without it,
    // "No available connection for database".
    const known = (await this.sqlite.isConnection(name, false)).result === true;
    if (!known) await this.sqlite.createConnection(name, true, "secret", SCHEMA_VERSION, false);
    try {
      await CapacitorSQLite.deleteDatabase({ database: name });
    } finally {
      try {
        await this.sqlite.closeConnection(name, false);
      } catch {
        // deleteDatabase may already have dropped the connection; the delete's own result is what counts.
      }
    }
  }

  async list(): Promise<string[]> {
    let values: string[] | undefined;
    try {
      ({ values } = await this.sqlite.getDatabaseList());
    } catch (error) {
      // An empty folder is an error for the plugin ("No databases available"), not an empty list
      // (seen on a phone). Only that message means empty: any other failure must propagate, so
      // the passphrase is never dropped because the listing was unreadable.
      if (!(error instanceof Error && error.message.includes("No databases available"))) throw error;
      return [];
    }
    return (values ?? []).filter((v): v is string => typeof v === "string").map(bareName);
  }

  async script(sql: string): Promise<void> {
    await this.db().execute(sql, false);
  }

  async read(sql: string, values: SqlValue[]): Promise<Row[]> {
    const result = await this.db().query(sql, values);
    return (result.values ?? []) as Row[];
  }

  async write(sql: string, values: SqlValue[], returning: boolean): Promise<Row[]> {
    const result = await this.db().run(sql, values, false, returning ? "all" : "no");
    return returning ? ((result.changes?.values ?? []) as Row[]) : [];
  }

  async begin(): Promise<void> {
    await this.db().beginTransaction();
  }

  async commit(): Promise<void> {
    await this.db().commitTransaction();
  }

  async rollback(): Promise<void> {
    await this.db().rollbackTransaction();
  }

  private db(): SQLiteDBConnection {
    if (!this.connection) throw new Error("no open connection");
    return this.connection;
  }
}
