import { clearLocalUserData } from "@/db/lifecycle";
import { clearLastUserId } from "./last-user";
import { flushBeforeLogout, stopSync, UnsyncedChangesError } from "@/sync/runtime";

export { UnsyncedChangesError };

let lastUserId: string | undefined;

export function rememberUserId(userId: string): void {
  lastUserId = userId;
}

export function lastKnownUserId(): string | undefined {
  return lastUserId;
}

export async function clearLocalData(userId = lastUserId): Promise<void> {
  if (!userId) {
    clearLastUserId();
    return;
  }
  // Offline work comes first: if it cannot be sent now, keep the data and say so.
  await flushBeforeLogout();
  stopSync();
  await clearLocalUserData(userId);
  clearLastUserId(); // explicit logout: no offline cold start for this user any more (ADR 0028)
  lastUserId = undefined;
}
