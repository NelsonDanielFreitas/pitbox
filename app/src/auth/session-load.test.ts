import { afterEach, describe, expect, it, vi } from "vitest";

const offline = vi.hoisted(() => ({ resolveOfflineSession: vi.fn() }));
vi.mock("./offline-session", () => offline);

import { NetworkError, OriginMismatchError } from "@/shared/lib/errors";

import { lastUserKey, readLastUserId, writeLastUserId } from "./last-user";
import { loadSession } from "./session-load";

const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const offlineSession = { authenticated: true, offline: true, user_id: userId, amr: [] };

function answer(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("loadSession", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.resetAllMocks();
    window.localStorage.clear();
  });

  it("remembers the user after a 200", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        answer(200, { authenticated: true, user_id: userId, amr: [], auth_time: "2026-10-31T12:00:00Z" }),
      ),
    );
    await expect(loadSession()).resolves.toMatchObject({ authenticated: true, user_id: userId });
    expect(readLastUserId()).toBe(userId);
    expect(offline.resolveOfflineSession).not.toHaveBeenCalled();
  });

  it("forgets the user after a 401 and never goes offline", async () => {
    writeLastUserId(userId);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(answer(401, { authenticated: false })));
    await expect(loadSession()).resolves.toEqual({ authenticated: false });
    expect(window.localStorage.getItem(lastUserKey)).toBeNull();
    expect(offline.resolveOfflineSession).not.toHaveBeenCalled();
  });

  it("falls back to the offline session only when there is no network", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    offline.resolveOfflineSession.mockResolvedValue(offlineSession);
    await expect(loadSession()).resolves.toEqual(offlineSession);
  });

  it("fails with NetworkError (no data) when there is no valid offline session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    offline.resolveOfflineSession.mockResolvedValue(null);
    await expect(loadSession()).rejects.toBeInstanceOf(NetworkError);
  });

  it.each([403, 503, 500])("fails closed on a %i answer, without the offline session", async (status) => {
    writeLastUserId(userId);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(answer(status, { error: "x" })));
    await expect(loadSession()).rejects.toBeDefined();
    expect(offline.resolveOfflineSession).not.toHaveBeenCalled();
    expect(readLastUserId()).toBe(userId); // an error is not a logout
  });

  it("does not use the offline session for a cancelled request", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new DOMException("aborted", "AbortError")));
    await expect(loadSession()).rejects.toMatchObject({ name: "AbortError" });
    expect(offline.resolveOfflineSession).not.toHaveBeenCalled();
  });

  it("keeps the OriginMismatchError of a 403", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(answer(403, { error: "x" })));
    await expect(loadSession()).rejects.toBeInstanceOf(OriginMismatchError);
  });
});
