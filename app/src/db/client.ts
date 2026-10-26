import type { DbRequest, DbResponse, SqlParams, SqlStatement } from "./protocol";
import {
  CorruptLocalDataError,
  LocalDatabaseBusyError,
  LocalDatabaseUnavailableError,
} from "./errors";

let worker: Worker | undefined;
let nextId = 1;
let openedHash: string | undefined;
const pending = new Map<
  number,
  { resolve: (response: DbResponse) => void; reject: (error: Error) => void }
>();

function ensureWorker(): Worker {
  if (worker) return worker;
  worker = new Worker(new URL("./worker.ts", import.meta.url), { type: "module" });
  worker.onmessage = (event: MessageEvent<DbResponse>) => {
    const waiter = pending.get(event.data.id);
    if (!waiter) return;
    pending.delete(event.data.id);
    waiter.resolve(event.data);
  };
  worker.onerror = (event) => {
    const error = new LocalDatabaseUnavailableError(event.message);
    for (const waiter of pending.values()) waiter.reject(error);
    pending.clear();
    worker = undefined;
  };
  return worker;
}

type RequestPayload = DbRequest extends infer Request
  ? Request extends { id: number }
    ? Omit<Request, "id">
    : never
  : never;

function send(request: RequestPayload): Promise<DbResponse> {
  const id = nextId++;
  const message: DbRequest = { ...request, id };
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    try {
      ensureWorker().postMessage(message);
    } catch (error) {
      pending.delete(id);
      reject(new LocalDatabaseUnavailableError("could not start the database worker", { cause: error }));
    }
  });
}

function throwResponse(response: DbResponse): asserts response is Extract<DbResponse, { ok: true }> {
  if (response.ok) return;
  switch (response.error.code) {
    case "busy":
      throw new LocalDatabaseBusyError(response.error.message);
    case "corrupt":
      throw new CorruptLocalDataError(response.error.message);
    case "unavailable":
      throw new LocalDatabaseUnavailableError(response.error.message);
    default:
      throw new LocalDatabaseUnavailableError(response.error.message);
  }
}

export async function hashUserId(userId: string): Promise<string> {
  const bytes = new TextEncoder().encode(userId);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export async function openDatabase(userId: string): Promise<void> {
  const userHash = await hashUserId(userId);
  if (openedHash === userHash) return;
  const response = await send({ type: "open", userHash });
  throwResponse(response);
  openedHash = userHash;
}

export async function closeDatabase(): Promise<void> {
  if (!worker) {
    openedHash = undefined;
    return;
  }
  const response = await send({ type: "close" });
  openedHash = undefined;
  throwResponse(response);
}

export async function markLogout(userId: string): Promise<void> {
  const response = await send({ type: "mark-logout", userHash: await hashUserId(userId) });
  throwResponse(response);
}

export async function deleteUserDatabase(userId: string): Promise<void> {
  const userHash = await hashUserId(userId);
  const response = await send({ type: "delete", userHash });
  if (openedHash === userHash) openedHash = undefined;
  throwResponse(response);
}

export async function exec(sql: string, params?: SqlParams): Promise<void> {
  const response = await send({ type: "exec", sql, params });
  throwResponse(response);
}

export async function query<T = Record<string, unknown>>(
  sql: string,
  params?: SqlParams,
): Promise<T[]> {
  const response = await send({ type: "query", sql, params });
  throwResponse(response);
  return (response.rows ?? []) as T[];
}

export async function transaction(statements: SqlStatement[]): Promise<void> {
  const response = await send({ type: "transaction", statements });
  throwResponse(response);
}
