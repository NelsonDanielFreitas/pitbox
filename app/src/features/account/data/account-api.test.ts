import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const native = vi.hoisted(() => ({ value: false }));
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => native.value }));

const authed = vi.hoisted(() => vi.fn());
const hasSession = vi.hoisted(() => vi.fn());
vi.mock("@/auth/native", () => {
  class SessionEndedError extends Error {}
  return { mobileAuth: () => ({ hasSession, authedFetch: authed }), SessionEndedError };
});
vi.mock("@/shared/lib/env", () => ({ apiUrl: (path: string) => (native.value ? `https://api.test${path}` : path) }));

import { SessionEndedError } from "@/auth/native";

import {
  listSessions,
  recoveryCodesStatus,
  revokeOtherSessions,
  revokeSession,
} from "./account-api";

const REF = "a".repeat(64);
const item = {
  ref: REF,
  kind: "web",
  created_at: "2026-11-10T20:30:16.362179878Z",
  auth_time: "2026-11-10T20:30:16Z",
  user_agent: "Mozilla/5.0",
  ip: "203.0.113.7",
  current: true,
};

function reply(status: number, body: unknown = {}) {
  return new Response(status === 204 ? null : JSON.stringify(body), { status });
}

beforeEach(() => {
  native.value = false;
  hasSession.mockReturnValue(true);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("on the web", () => {
  it("lists the sessions with the cookie and no cache", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(200, { sessions: [item] }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(listSessions()).resolves.toEqual({ status: "ok", sessions: [item] });
    expect(fetchMock).toHaveBeenCalledWith(
      "/auth/sessions",
      expect.objectContaining({ method: "GET", credentials: "same-origin", cache: "no-store" }),
    );
  });

  it.each([
    ["a field the contract does not have", { sessions: [{ ...item, user_id: "u" }] }],
    ["a bad reference", { sessions: [{ ...item, ref: "short" }] }],
    ["an unknown kind", { sessions: [{ ...item, kind: "tv" }] }],
    ["an extra top-level key", { sessions: [], extra: 1 }],
  ])("refuses %s", async (_name, body) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(reply(200, body)));
    await expect(listSessions()).resolves.toEqual({ status: "unavailable" });
  });

  it.each([
    [401, "unauthenticated"],
    [403, "stepUp"],
    [503, "unavailable"],
  ] as const)("maps %i to %s", async (code, status) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(reply(code)));
    await expect(listSessions()).resolves.toEqual({ status });
  });

  it("is unavailable when the network fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("network")));
    await expect(listSessions()).resolves.toEqual({ status: "unavailable" });
  });

  it("ends one session and treats an already-gone one as done", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(reply(204)).mockResolvedValueOnce(reply(404));
    vi.stubGlobal("fetch", fetchMock);
    await expect(revokeSession(REF)).resolves.toEqual({ status: "ok" });
    await expect(revokeSession(REF)).resolves.toEqual({ status: "ok" });
    expect(fetchMock.mock.calls[0]?.[0]).toBe(`/auth/sessions/${REF}`);
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ method: "DELETE" });
  });

  it("never puts a malformed reference in a URL", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(revokeSession("../../auth/logout")).resolves.toEqual({ status: "unavailable" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("ends the other sessions", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(reply(200, { revoked: 2 })));
    await expect(revokeOtherSessions()).resolves.toEqual({ status: "ok", revoked: 2 });
  });

  it("reads how many codes are left", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(reply(200, { unused: 5 })));
    await expect(recoveryCodesStatus()).resolves.toEqual({ status: "ok", unused: 5 });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(reply(200, { unused: -1 })));
    await expect(recoveryCodesStatus()).resolves.toEqual({ status: "unavailable" });
  });
});

describe("on the phone", () => {
  beforeEach(() => {
    native.value = true;
  });

  it("goes through the Bearer fetch with an absolute URL and never the cookie", async () => {
    const plain = vi.fn();
    vi.stubGlobal("fetch", plain);
    authed.mockResolvedValue(reply(200, { sessions: [{ ...item, kind: "mobile" }] }));
    await expect(listSessions()).resolves.toMatchObject({ status: "ok" });
    expect(authed).toHaveBeenCalledWith("https://api.test/auth/sessions", expect.objectContaining({ method: "GET" }));
    expect(plain).not.toHaveBeenCalled();
  });

  it("asks nothing when there is no session on the phone", async () => {
    hasSession.mockReturnValue(false);
    await expect(listSessions()).resolves.toEqual({ status: "unauthenticated" });
    expect(authed).not.toHaveBeenCalled();
  });

  it("reports an ended session as unauthenticated", async () => {
    authed.mockRejectedValue(new SessionEndedError());
    await expect(recoveryCodesStatus()).resolves.toEqual({ status: "unauthenticated" });
  });
});
