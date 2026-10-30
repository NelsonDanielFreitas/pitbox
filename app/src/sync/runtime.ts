import { hashUserId } from "@/db/client";

import { IndexedDbKeyStore } from "./device-key";
import { SyncEngine, type SyncSnapshot } from "./engine";
import { assertSafeToErase, UnsyncedChangesError } from "./logout-guard";
import { onLocalWrite } from "./outbox";
import { ConnectSyncApi } from "./sync-api";

// Owns the single engine of the signed-in user and the browser triggers that wake it:
// a local write, `online`, the tab becoming visible, and (in the engine) a periodic poll.
// Data changes pulled by one tab are announced to the others over a BroadcastChannel.

export interface RuntimeOptions {
  userId: string;
  /** Local data changed (a Pull applied rows): refresh what the screens show. */
  onDataChanged: () => void;
  /** The session is gone: ask for a new login. */
  onUnauthenticated: () => void;
}

const idle: SyncSnapshot = {
  phase: "idle",
  pending: 0,
  failed: 0,
  conflicts: 0,
  lastSyncAt: null,
  problem: null,
};

let engine: SyncEngine | undefined;
let teardown: Array<() => void> = [];
let activeUser: string | undefined;
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

export const subscribeSync = (listener: () => void): (() => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

export const getSyncSnapshot = (): SyncSnapshot => engine?.getSnapshot() ?? idle;

export function getEngine(): SyncEngine | undefined {
  return engine;
}

function webLockExclusive(name: string) {
  return async (fn: () => Promise<void>): Promise<boolean> => {
    if (typeof navigator === "undefined" || !("locks" in navigator)) {
      await fn();
      return true;
    }
    // ifAvailable: a tab that finds the lock taken skips this cycle instead of queueing.
    const result: unknown = await navigator.locks.request(name, { ifAvailable: true }, async (lock) => {
      if (!lock) return false;
      await fn();
      return true;
    });
    return result === true;
  };
}

export async function startSync(options: RuntimeOptions): Promise<void> {
  if (engine && activeUser === options.userId) return;
  stopSync();
  const userHash = await hashUserId(options.userId);
  activeUser = options.userId;

  const channel = typeof BroadcastChannel === "undefined" ? null : new BroadcastChannel(`pitbox-sync-${userHash}`);
  const next = new SyncEngine({
    api: new ConnectSyncApi(),
    userId: options.userId,
    userHash,
    keyStore: new IndexedDbKeyStore(),
    deviceName: "Browser",
    exclusive: webLockExclusive(`pitbox-sync-${userHash}`),
    onDataChanged: () => {
      options.onDataChanged();
      channel?.postMessage("changed");
    },
    onUnauthenticated: options.onUnauthenticated,
  });
  engine = next;

  const wake = () => next.requestSync(true);
  const onVisible = () => {
    if (document.visibilityState === "visible") next.requestSync(true);
  };
  window.addEventListener("online", wake);
  document.addEventListener("visibilitychange", onVisible);
  if (channel) channel.onmessage = () => options.onDataChanged();
  teardown = [
    () => window.removeEventListener("online", wake),
    () => document.removeEventListener("visibilitychange", onVisible),
    onLocalWrite(() => next.requestSync()),
    next.subscribe(notify),
    () => channel?.close(),
  ];
  next.start();
  notify();
}

export function stopSync(): void {
  for (const undo of teardown) undo();
  teardown = [];
  void engine?.close();
  engine = undefined;
  activeUser = undefined;
  notify();
}

export { UnsyncedChangesError };

/**
 * Before the local data is erased at logout: try one last sync, then refuse if anything would
 * be lost (operations still waiting, or data of entities that do not sync yet).
 */
export async function flushBeforeLogout(timeoutMs = 10_000): Promise<void> {
  const current = engine;
  if (current) {
    await Promise.race([
      current.syncNow().catch(() => undefined),
      new Promise<void>((resolve) => setTimeout(resolve, timeoutMs)),
    ]);
    await current.refreshCounts();
  }
  await assertSafeToErase();
}
