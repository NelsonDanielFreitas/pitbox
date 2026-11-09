/** Remembers which users must be wiped on the next open, in case the app dies mid-logout. */
export interface LogoutMarks {
  list(): Promise<string[]>;
  add(userHash: string): Promise<void>;
  remove(userHash: string): Promise<void>;
}

const MARKS_DB = "pitbox-logout-marks";
const MARKS_STORE = "pending";

function idb<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error("indexeddb request failed"));
  });
}

/**
 * Keeps the marks in IndexedDB (`localStorage` is reserved for the language). The value is a user
 * hash, which is already part of the database file name, so it holds nothing secret. If storage
 * is unusable the mark is lost, but the delete that always follows it still runs.
 */
export class IndexedDbLogoutMarks implements LogoutMarks {
  private opened: Promise<IDBDatabase> | undefined;

  private open(): Promise<IDBDatabase> {
    this.opened ??= new Promise((resolve, reject) => {
      const req = indexedDB.open(MARKS_DB, 1);
      req.onupgradeneeded = () => req.result.createObjectStore(MARKS_STORE);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error ?? new Error("indexeddb open failed"));
    });
    return this.opened;
  }

  private async store(mode: IDBTransactionMode): Promise<IDBObjectStore> {
    return (await this.open()).transaction(MARKS_STORE, mode).objectStore(MARKS_STORE);
  }

  async list(): Promise<string[]> {
    try {
      const keys: unknown[] = await idb((await this.store("readonly")).getAllKeys());
      return keys.filter((k): k is string => typeof k === "string" && /^[0-9a-f]{64}$/.test(k));
    } catch {
      return [];
    }
  }

  async add(userHash: string): Promise<void> {
    try {
      await idb((await this.store("readwrite")).put(true, userHash));
    } catch {
      // See the class comment.
    }
  }

  async remove(userHash: string): Promise<void> {
    try {
      await idb((await this.store("readwrite")).delete(userHash));
    } catch {
      // See the class comment.
    }
  }
}

