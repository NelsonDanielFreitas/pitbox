import { readFileSync } from "node:fs";
import { join } from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  AuthUnavailableError,
  ContractError,
  NetworkError,
  OriginMismatchError,
} from "@/shared/lib/errors";

import { fetchSession, loginHref, safeReturnTo } from "./session";

const fixtures = join(process.cwd(), "../backend/internal/platform/auth/testdata");

function fixture(name: string): string {
  return readFileSync(join(fixtures, name), "utf8").trim();
}

function response(body: string, status = 200): Response {
  return new Response(body, { status, headers: { "Content-Type": "application/json" } });
}

describe("fetchSession", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("parses the authenticated response and normalizes amr to an array", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(fixture("session_authenticated.json"))));
    await expect(fetchSession()).resolves.toEqual({
      authenticated: true,
      user_id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
      amr: [],
      auth_time: "2026-10-13T12:00:00Z",
    });
  });

  it("parses 401 as unauthenticated", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(fixture("session_unauthenticated.json"), 401)));
    await expect(fetchSession()).resolves.toEqual({ authenticated: false });
  });

  it("rejects amr null instead of silently accepting it", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        response(
          JSON.stringify({
            authenticated: true,
            user_id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
            amr: null,
            auth_time: "2026-10-13T12:00:00Z",
          }),
        ),
      ),
    );
    await expect(fetchSession()).rejects.toBeInstanceOf(ContractError);
  });

  it("maps 403 to an origin configuration error", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(fixture("session_forbidden.json"), 403)));
    await expect(fetchSession()).rejects.toBeInstanceOf(OriginMismatchError);
  });

  it("maps 503 to a recoverable unavailable error", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(fixture("session_unavailable.json"), 503)));
    await expect(fetchSession()).rejects.toBeInstanceOf(AuthUnavailableError);
  });

  it("maps a fetch that never got an answer to NetworkError", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    await expect(fetchSession()).rejects.toBeInstanceOf(NetworkError);
  });

  it("does not treat a cancelled fetch as missing network", async () => {
    const abort = new DOMException("aborted", "AbortError");
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(abort));
    await expect(fetchSession()).rejects.toBe(abort);

    const controller = new AbortController();
    controller.abort();
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("cancelled")));
    await expect(fetchSession(controller.signal)).rejects.not.toBeInstanceOf(NetworkError);
  });

  it("never turns a server answer into NetworkError", async () => {
    for (const status of [401, 403, 500, 503]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response(JSON.stringify({ error: "x" }), status)));
      await expect(fetchSession()).rejects.not.toBeInstanceOf(NetworkError);
    }
  });

  it("rejects malformed JSON with a contract error", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response("not json")));
    await expect(fetchSession()).rejects.toBeInstanceOf(ContractError);
  });
});

describe("loginHref", () => {
  it("encodes only internal paths", () => {
    expect(loginHref("/teams")).toBe("/auth/login?return_to=%2Fteams");
    expect(loginHref("//evil.example")).toBe("/auth/login?return_to=%2Fteams");
    expect(loginHref("https://evil.example")).toBe("/auth/login?return_to=%2Fteams");
    expect(safeReturnTo("/auth/login")).toBe("/teams");
  });
});
