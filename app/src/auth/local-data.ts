import { clearLocalUserData } from "@/db/lifecycle";
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
  if (!userId) return;
  // Offline work comes first: if it cannot be sent now, keep the data and say so.
  await flushBeforeLogout();
  stopSync();
  await clearLocalUserData(userId);
  lastUserId = undefined;
}
