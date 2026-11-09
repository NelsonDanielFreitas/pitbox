import {
  CorruptLocalDataError,
  LocalConstraintError,
  LocalDatabaseBusyError,
  LocalDatabaseUnavailableError,
} from "./errors";
import type { LocalDatabase } from "./local-database";
import type { DbRequest, DbResponse, SqlParams, SqlStatement } from "./protocol";
import { hashUserId } from "./user-hash";

type RequestPayload = DbRequest extends infer Request
  ? Request extends { id: number }
    ? Omit<Request, "id">
    : never
  : never;

function throwResponse(response: DbResponse): asserts response is Extract<DbResponse, { ok: true }> {
  if (response.ok) return;
  switch (response.error.code) {
    case "busy":
      throw new LocalDatabaseBusyError(response.error.message);
    case "corrupt":
      throw new CorruptLocalDataError(response.error.message);
    case "constraint":
      throw new LocalConstraintError(response.error.message);
    case "unavailable":
      throw new LocalDatabaseUnavailableError(response.error.message);
    default:
      throw new LocalDatabaseUnavailableError(response.error.message);
  }
}

/** The browser engine: wa-sqlite on OPFS, inside a dedicated Worker (ADR 0018). */
export class WebDatabase implements LocalDatabase {
  private worker: Worker | undefined;
  private nextId = 1;
  private openedHash: string | undefined;
  private opening: Promise<void> | undefined;
  private readonly pending = new Map<
    number,
    { resolve: (response: DbResponse) => void; reject: (error: Error) => void }
  >();

  private ensureWorker(): Worker {
    if (this.worker) return this.worker;
    const worker = new Worker(new URL("./worker.ts", import.meta.url), { type: "module" });
    worker.onmessage = (event: MessageEvent<DbResponse>) => {
      const waiter = this.pending.get(event.data.id);
      if (!waiter) return;
      this.pending.delete(event.data.id);
      waiter.resolve(event.data);
    };
    worker.onerror = (event) => {
      const error = new LocalDatabaseUnavailableError(event.message);
      for (const waiter of this.pending.values()) waiter.reject(error);
      this.pending.clear();
      this.worker = undefined;
    };
    this.worker = worker;
    return worker;
  }

  private send(request: RequestPayload): Promise<DbResponse> {
    const id = this.nextId++;
    const message: DbRequest = { ...request, id };
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      try {
        this.ensureWorker().postMessage(message);
      } catch (error) {
        this.pending.delete(id);
        reject(
          new LocalDatabaseUnavailableError("could not start the database worker", { cause: error }),
        );
      }
    });
  }

  // Same layout as worker.ts (`pitbox/<sha256(user_id)>/`). Read-only, so it never creates
  // the folder: a made-up user id must not leave an empty database behind.
  async exists(userId: string): Promise<boolean> {
    try {
      const root = await navigator.storage.getDirectory();
      const pitbox = await root.getDirectoryHandle("pitbox");
      await pitbox.getDirectoryHandle(await hashUserId(userId));
      return true;
    } catch {
      return false;
    }
  }

  async open(userId: string): Promise<void> {
    const userHash = await hashUserId(userId);
    if (this.openedHash === userHash) return;
    if (this.opening) {
      await this.opening;
      if (this.openedHash === userHash) return;
    }
    const operation = (async () => {
      const response = await this.send({ type: "open", userHash });
      throwResponse(response);
      this.openedHash = userHash;
    })();
    this.opening = operation;
    try {
      await operation;
    } finally {
      if (this.opening === operation) this.opening = undefined;
    }
  }

  async close(): Promise<void> {
    if (this.opening) await this.opening.catch(() => undefined);
    if (!this.worker) {
      this.openedHash = undefined;
      return;
    }
    const response = await this.send({ type: "close" });
    this.openedHash = undefined;
    throwResponse(response);
  }

  async markLogout(userId: string): Promise<void> {
    const response = await this.send({ type: "mark-logout", userHash: await hashUserId(userId) });
    throwResponse(response);
  }

  async deleteUserDatabase(userId: string): Promise<void> {
    const userHash = await hashUserId(userId);
    const response = await this.send({ type: "delete", userHash });
    if (this.openedHash === userHash) this.openedHash = undefined;
    throwResponse(response);
  }

  async exec(sql: string, params?: SqlParams): Promise<void> {
    const response = await this.send({ type: "exec", sql, params });
    throwResponse(response);
  }

  async query<T = Record<string, unknown>>(sql: string, params?: SqlParams): Promise<T[]> {
    const response = await this.send({ type: "query", sql, params });
    throwResponse(response);
    return (response.rows ?? []) as T[];
  }

  async transaction<T = Record<string, unknown>>(
    statements: SqlStatement[],
    options?: { requireRows?: boolean },
  ): Promise<T[]> {
    const response = await this.send({
      type: "transaction",
      statements,
      requireRows: options?.requireRows,
    });
    throwResponse(response);
    return (response.rows ?? []) as T[];
  }
}
