import { MigrationError } from "../errors";
import type { SqlParams, SqlStatement } from "../protocol";
import type { Migration } from "./index";
import { migrations } from "./index";

export interface SqlExecutor {
  exec(sql: string, params?: SqlParams): Promise<void>;
  query<T = Record<string, unknown>>(sql: string, params?: SqlParams): Promise<T[]>;
  transaction(statements: SqlStatement[]): Promise<unknown>;
}

interface AppliedMigration {
  version: number;
  checksum: string;
}

function normaliseSql(sql: string): string {
  return sql.replace(/\r\n/g, "\n");
}

async function checksum(sql: string): Promise<string> {
  const bytes = new TextEncoder().encode(normaliseSql(sql));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export async function runMigrations(
  executor: SqlExecutor,
  list: readonly Migration[] = migrations,
): Promise<void> {
  await executor.exec(`
    CREATE TABLE IF NOT EXISTS schema_migrations (
      version    INTEGER PRIMARY KEY,
      name       TEXT NOT NULL,
      checksum   TEXT NOT NULL,
      applied_at TEXT NOT NULL
    ) STRICT
  `);

  const appliedRows = await executor.query<AppliedMigration>(
    "SELECT version, checksum FROM schema_migrations ORDER BY version",
  );
  const applied = new Map(appliedRows.map((row) => [row.version, row.checksum]));

  for (const migration of list) {
    const expected = await checksum(migration.sql);
    const previous = applied.get(migration.version);
    if (previous !== undefined) {
      if (previous !== expected) {
        throw new MigrationError(
          `migration ${migration.version} (${migration.name}) was modified after being applied`,
        );
      }
      continue;
    }

    try {
      await executor.transaction([
        { sql: migration.sql },
        {
          sql: "INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES ($version, $name, $checksum, $appliedAt)",
          params: {
            $version: migration.version,
            $name: migration.name,
            $checksum: expected,
            $appliedAt: new Date().toISOString(),
          },
        },
      ]);
    } catch (error) {
      throw new MigrationError(`migration ${migration.version} (${migration.name}) failed`, {
        cause: error,
      });
    }
  }
}
