import {
  CorruptLocalDataError,
  LocalConstraintError,
  LocalDatabaseBusyError,
  LocalDatabaseUnavailableError,
} from "./errors";
import type { LocalDatabase } from "./local-database";
import { runMigrations } from "./migrations/runner";
import { databasePragmas } from "./pragmas";
import type { SqlParams, SqlStatement } from "./protocol";
import { bindValues, parseSql, SqlParamsError, type ParsedSql, type SqlValue } from "./sql-params";
import { IndexedDbLogoutMarks } from "./logout-marks";
import type { LogoutMarks } from "./logout-marks";
import { hashUserId } from "./user-hash";

export type Row = Record<string, unknown>;

/**
 * What the native engine needs from the SQLCipher plugin, and nothing more (ADR 0033). It is
 * positional (`?`) and works on one open connection at a time. `NativeDatabase` holds all the
 * logic; a driver only forwards calls, so the logic can be tested without a phone.
 */
export interface NativeSqliteDriver {
  /**
   * Makes sure the database passphrase exists, generating it on the first run. The passphrase is
   * created once, kept by the plugin (wrapped by the Android Keystore) and never handed back.
   */
  ensureSecret(): Promise<void>;
  /** Forgets the passphrase. Only called when no database of the app is left. */
  clearSecret(): Promise<void>;
  /** Opens (or creates, encrypted) the database and makes it the current connection. */
  open(name: string): Promise<void>;
  /** Whether the file is encrypted. Called after `open`; a plain file must never be used. */
  isEncrypted(name: string): Promise<boolean>;
  close(name: string): Promise<void>;
  exists(name: string): Promise<boolean>;
  delete(name: string): Promise<void>;
  /** Every database file the plugin holds, whatever its name. */
  list(): Promise<string[]>;
  /** Several statements, no parameters, no rows, no transaction of its own. */
  script(sql: string): Promise<void>;
  /** One statement that returns rows. */
  read(sql: string, values: SqlValue[]): Promise<Row[]>;
  /** One statement that writes; with `returning`, the rows of its RETURNING clause. */
  write(sql: string, values: SqlValue[], returning: boolean): Promise<Row[]>;
  begin(): Promise<void>;
  commit(): Promise<void>;
  rollback(): Promise<void>;
}

const NAME_PREFIX = "pitbox_";
const READ_KEYWORDS = new Set(["select", "with", "values", "pragma", "explain"]);

/**
 * What a constraint error may say: SQLite's own words, which name only objects ("UNIQUE constraint
 * failed: tyre_set.team_id, tyre_set.label") and never a value. The repositories read it to tell
 * a repeated tyre label from a repeated transponder, as they do on the web; without it every
 * native failure looked like the generic "conflicts with another record". It travels in `detail`,
 * never in `message`. Anything that does not look like that is dropped (the plugin message can
 * carry SQL text).
 */
export function constraintDetail(text: string): string | undefined {
  const found = /(UNIQUE|NOT NULL|CHECK|FOREIGN KEY|PRIMARY KEY) constraint failed(?:: ([A-Za-z0-9_., ]{1,200}))?/.exec(text);
  return found ? found[0].trim() : undefined;
}

/** Engine errors never reach the screens as they are: only the classes of `./errors`. */
export function toLocalError(error: unknown): Error {
  if (
    error instanceof LocalConstraintError ||
    error instanceof LocalDatabaseBusyError ||
    error instanceof LocalDatabaseUnavailableError ||
    error instanceof CorruptLocalDataError
  ) {
    return error;
  }
  const text = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  // Generic messages on purpose: a plugin message can carry SQL text. The cause keeps the original.
  if (/constraint/i.test(text)) return new LocalConstraintError(undefined, { cause: error, detail: constraintDetail(text) });
  if (/busy|locked/i.test(text)) return new LocalDatabaseBusyError(undefined, { cause: error });
  // A wrong or missing key looks like this to SQLCipher. Retrying cannot fix it.
  if (/not a database|file is encrypted|malformed|corrupt|cipher/i.test(text)) {
    return new CorruptLocalDataError(undefined, { cause: error });
  }
  return new LocalDatabaseUnavailableError(undefined, { cause: error });
}

/** The Android engine: SQLCipher behind a driver (ADR 0033). */
export class NativeDatabase implements LocalDatabase {
  private current: string | undefined;
  private tail: Promise<unknown> = Promise.resolve();

  constructor(
    private readonly driver: NativeSqliteDriver,
    private readonly marks: LogoutMarks = new IndexedDbLogoutMarks(),
  ) {}

  // JavaScript async functions are not a mutex: every await yields. The plugin has one
  // connection, so every public call runs one at a time, like the web worker's message chain.
  private serial<T>(task: () => Promise<T>): Promise<T> {
    const run = this.tail.then(task, task);
    this.tail = run.catch(() => undefined);
    return run;
  }

  private static nameOf(userHash: string): string {
    return `${NAME_PREFIX}${userHash}`;
  }

  async exists(userId: string): Promise<boolean> {
    const name = NativeDatabase.nameOf(await hashUserId(userId));
    return this.serial(async () => {
      try {
        return await this.driver.exists(name);
      } catch {
        return false;
      }
    });
  }

  async open(userId: string): Promise<void> {
    const userHash = await hashUserId(userId);
    const name = NativeDatabase.nameOf(userHash);
    return this.serial(async () => {
      if (this.current === name) return;
      await this.closeRaw();
      await this.wipeMarked();
      try {
        await this.driver.ensureSecret();
        await this.driver.open(name);
        this.current = name;
        // A database that was just created is an empty file until something is written, and the
        // plugin probes the file with an empty passphrase: an empty file "opens" without a key and
        // is reported as plain (seen on a real phone). Writing the header first makes the file
        // real, and encrypted, so the probe tells the truth.
        await this.driver.script("PRAGMA user_version = 1");
        // Fail closed: never carry on with a plain file, and never replace one we cannot read.
        if (!(await this.driver.isEncrypted(name))) {
          throw new CorruptLocalDataError("local database is not encrypted");
        }
        for (const pragma of databasePragmas) await this.driver.script(pragma);
        const integrity = await this.driver.read("PRAGMA quick_check", []);
        if (Object.values(integrity[0] ?? {})[0] !== "ok") {
          throw new CorruptLocalDataError("quick_check failed");
        }
        await runMigrations({
          exec: (sql, params) => this.execRaw(sql, params),
          query: <T = Row>(sql: string, params?: SqlParams) =>
            this.queryRaw(sql, params) as Promise<T[]>,
          transaction: (statements) => this.transactionRaw(statements),
        });
      } catch (error) {
        await this.closeRaw().catch(() => undefined);
        throw toLocalError(error);
      }
    });
  }

  async close(): Promise<void> {
    return this.serial(() => this.closeRaw());
  }

  async markLogout(userId: string): Promise<void> {
    await this.marks.add(await hashUserId(userId));
  }

  async deleteUserDatabase(userId: string): Promise<void> {
    const userHash = await hashUserId(userId);
    return this.serial(async () => {
      try {
        if (this.current === NativeDatabase.nameOf(userHash)) await this.closeRaw();
        await this.deleteFiles(userHash);
        await this.marks.remove(userHash);
        await this.clearSecretIfNoneLeft();
      } catch (error) {
        throw toLocalError(error);
      }
    });
  }

  exec(sql: string, params?: SqlParams): Promise<void> {
    return this.serial(async () => {
      await this.execRaw(sql, params);
    });
  }

  query<T = Row>(sql: string, params?: SqlParams): Promise<T[]> {
    return this.serial(async () => (await this.queryRaw(sql, params)) as T[]);
  }

  transaction<T = Row>(
    statements: SqlStatement[],
    options?: { requireRows?: boolean },
  ): Promise<T[]> {
    return this.serial(
      async () => (await this.transactionRaw(statements, options?.requireRows)) as T[],
    );
  }

  // --- unserialised internals: the caller already holds the chain ------------------------

  private async closeRaw(): Promise<void> {
    const name = this.current;
    this.current = undefined;
    if (name !== undefined) await this.driver.close(name);
  }

  private requireOpen(): void {
    if (this.current === undefined) {
      throw new LocalDatabaseUnavailableError("database is not open");
    }
  }

  private async execRaw(sql: string, params?: SqlParams): Promise<void> {
    await this.statement(sql, params);
  }

  private queryRaw(sql: string, params?: SqlParams): Promise<Row[]> {
    return this.statement(sql, params);
  }

  /** Runs one statement, or one parameterless script, and returns the rows it yields. */
  private async statement(sql: string, params: SqlParams | undefined): Promise<Row[]> {
    this.requireOpen();
    try {
      const parsed = parseSql(sql);
      return await this.route(parsed, bindValues(parsed, params));
    } catch (error) {
      throw error instanceof SqlParamsError
        ? new LocalDatabaseUnavailableError("unsupported statement", { cause: error })
        : toLocalError(error);
    }
  }

  private async route(parsed: ParsedSql, values: SqlValue[]): Promise<Row[]> {
    if (parsed.statements > 1) {
      // A script has no parameters: binding them to the right statement is not guessable.
      if (parsed.names.length > 0) {
        throw new SqlParamsError("parameters in a multi-statement script");
      }
      await this.driver.script(parsed.sql);
      return [];
    }
    if (parsed.returning) return this.driver.write(parsed.sql, values, true);
    if (READ_KEYWORDS.has(parsed.firstKeyword)) return this.driver.read(parsed.sql, values);
    await this.driver.write(parsed.sql, values, false);
    return [];
  }

  /** All or nothing; the result is the rows of the last statement. */
  private async transactionRaw(statements: SqlStatement[], requireRows = false): Promise<Row[]> {
    this.requireOpen();
    try {
      await this.driver.begin();
    } catch (error) {
      throw toLocalError(error);
    }
    try {
      let rows: Row[] = [];
      for (const { sql, params } of statements) {
        rows = await this.statement(sql, params);
      }
      if (requireRows && rows.length === 0) {
        throw new LocalConstraintError("transaction required at least one row");
      }
      await this.driver.commit();
      return rows;
    } catch (error) {
      try {
        await this.driver.rollback();
      } catch {
        // The transaction may already be gone; the original error is the one to report.
      }
      throw toLocalError(error);
    }
  }

  private async deleteFiles(userHash: string): Promise<void> {
    const name = NativeDatabase.nameOf(userHash);
    if (await this.driver.exists(name)) await this.driver.delete(name);
  }

  private async wipeMarked(): Promise<void> {
    let wiped = false;
    for (const userHash of await this.marks.list()) {
      try {
        await this.deleteFiles(userHash);
        await this.marks.remove(userHash);
        wiped = true;
      } catch {
        // Keep the mark: the next open tries again. Wiping must not block the user's own data.
      }
    }
    if (wiped) await this.clearSecretIfNoneLeft().catch(() => undefined);
  }

  /**
   * The passphrase is global to the plugin, so it can only go when the last database is gone.
   * Losing it while a database remains would make that database unreadable, so this is
   * deliberately cautious: ANY name in the listing, whatever its format, keeps the passphrase.
   */
  private async clearSecretIfNoneLeft(): Promise<void> {
    if ((await this.driver.list()).length === 0) await this.driver.clearSecret();
  }
}

export async function createNativeDatabase(): Promise<LocalDatabase> {
  const { CapacitorSqliteDriver } = await import("./capacitor-sqlite-driver");
  return new NativeDatabase(new CapacitorSqliteDriver());
}
