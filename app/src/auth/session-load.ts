import { NetworkError } from "@/shared/lib/errors";

import { clearLastUserId, writeLastUserId } from "./last-user";
import { resolveOfflineSession } from "./offline-session";
import { fetchSession, type OnlineSession, type Session } from "./session";

type Answer = OnlineSession | { authenticated: false };

/** Asks the server and keeps the "last user" in step: written on 200, cleared on 401. */
export async function fetchRememberedSession(signal?: AbortSignal): Promise<Answer> {
  const session = await fetchSession(signal);
  if (session.authenticated) writeLastUserId(session.user_id);
  else clearLastUserId();
  return session;
}

/**
 * Session for the first render. Only "the request never got an answer" may fall back to the
 * offline session; 401, 403, 503 and a bad contract keep failing closed (ADR 0028).
 */
export async function loadSession(signal?: AbortSignal): Promise<Session> {
  try {
    return await fetchRememberedSession(signal);
  } catch (error) {
    if (!(error instanceof NetworkError)) throw error;
    const offline = await resolveOfflineSession();
    if (offline) return offline;
    throw error;
  }
}
