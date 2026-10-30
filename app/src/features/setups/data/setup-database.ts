import { query, transaction } from "@/db/client";
import type { SqlParams, SqlStatement } from "@/db/protocol";

export interface WriteOptions {
  requireRows?: boolean;
}

export interface SetupDatabase {
  query<T = Record<string, unknown>>(sql: string, params?: SqlParams): Promise<T[]>;
  transaction<T = Record<string, unknown>>(
    statements: SqlStatement[],
    options?: WriteOptions,
  ): Promise<T[]>;
}

export const defaultSetupDatabase: SetupDatabase = {
  query,
  transaction: <T>(statements: SqlStatement[], options?: WriteOptions) =>
    transaction<T>(statements, options),
};
