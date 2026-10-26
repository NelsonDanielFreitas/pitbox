export type SqlParams = Record<string, string | number | null | Uint8Array>;

export interface SqlStatement {
  sql: string;
  params?: SqlParams;
}

export type DbErrorCode = "unavailable" | "busy" | "corrupt" | "internal";

export type DbRequest =
  | { id: number; type: "open"; userHash: string }
  | { id: number; type: "close" }
  | { id: number; type: "mark-logout"; userHash: string }
  | { id: number; type: "delete"; userHash: string }
  | { id: number; type: "exec"; sql: string; params?: SqlParams }
  | { id: number; type: "query"; sql: string; params?: SqlParams }
  | { id: number; type: "transaction"; statements: SqlStatement[] };

export type DbResponse =
  | { id: number; ok: true; rows?: Record<string, unknown>[] }
  | { id: number; ok: false; error: { code: DbErrorCode; message: string } };
