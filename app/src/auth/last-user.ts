import { z } from "zod";

// The "last user" (ADR 0028): only the user id, so a cold start without network knows which
// local database to try. Never tokens, emails or roles. It is a hint, not proof: opening the
// database still needs an unexpired offline pass, and the server decides on every Push.

export const lastUserKey = "pitbox.last-user.v1";

const lastUserSchema = z.object({ user_id: z.string().uuid() }).strict();

export function readLastUserId(storage: Storage = window.localStorage): string | null {
  let raw: string | null;
  try {
    raw = storage.getItem(lastUserKey);
  } catch {
    return null;
  }
  if (raw === null) return null;
  try {
    const parsed = lastUserSchema.safeParse(JSON.parse(raw));
    if (parsed.success) return parsed.data.user_id;
  } catch {
    // Not JSON: treated as tampered, like any other invalid value.
  }
  clearLastUserId(storage);
  return null;
}

export function writeLastUserId(userId: string, storage: Storage = window.localStorage): void {
  try {
    storage.setItem(lastUserKey, JSON.stringify({ user_id: userId }));
  } catch {
    // Storage full or blocked: the app just cannot cold-start offline.
  }
}

export function clearLastUserId(storage: Storage = window.localStorage): void {
  try {
    storage.removeItem(lastUserKey);
  } catch {
    // Nothing to clear.
  }
}
