import { clearLocalUserData } from "@/db/lifecycle";

let lastUserId: string | undefined;

export function rememberUserId(userId: string): void {
  lastUserId = userId;
}

export function lastKnownUserId(): string | undefined {
  return lastUserId;
}

export async function clearLocalData(userId = lastUserId): Promise<void> {
  if (!userId) return;
  await clearLocalUserData(userId);
  lastUserId = undefined;
}
