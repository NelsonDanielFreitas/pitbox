import { z } from "zod";

import { closeDatabase, localDatabaseExists, openDatabase, query } from "@/db/client";

import { readLastUserId } from "./last-user";
import type { OfflineSession } from "./session";

// Cold start without network (ADR 0028). The local pass only decides whether the database
// opens: it is not verified here and the phone clock may be wrong. The server verifies the
// pass and every signature on Push (ADR 0025).

const passSchema = z.object({ token: z.string(), expiresAt: z.string() });

/** True when `raw` (the value of `sync.pass.<team>`) is a well-formed pass that has not expired. */
export function isPassValid(raw: string | null, now: Date): boolean {
  if (!raw) return false;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return false;
  }
  const parsed = passSchema.safeParse(value);
  if (!parsed.success) return false;
  const expires = Date.parse(parsed.data.expiresAt);
  return Number.isFinite(expires) && expires > now.getTime();
}

export async function hasValidOfflinePass(now: Date = new Date()): Promise<boolean> {
  const rows = await query<{ value: string }>(
    "SELECT value FROM app_meta WHERE key LIKE 'sync.pass.%'",
  );
  return rows.some((row) => isPassValid(row.value, now));
}

/**
 * Opens the last user's database and checks for a valid pass. Without one the database is
 * closed again (never deleted: it may hold work that is not yet sent) and nothing is shown.
 */
export async function resolveOfflineSession(now: Date = new Date()): Promise<OfflineSession | null> {
  const userId = readLastUserId();
  if (!userId || !(await localDatabaseExists(userId))) return null;
  try {
    await openDatabase(userId);
    if (await hasValidOfflinePass(now)) {
      return { authenticated: true, offline: true, user_id: userId, amr: [] };
    }
  } catch {
    // An unreadable database is not a reason to show data.
  }
  await closeDatabase().catch(() => undefined);
  return null;
}
