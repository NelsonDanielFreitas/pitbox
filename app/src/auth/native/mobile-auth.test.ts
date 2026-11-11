import { createPublicKey, generateKeyPairSync, sign, verify, type KeyObject } from "node:crypto";
import { describe, expect, it } from "vitest";

import { AuthUnavailableError, NetworkError } from "@/shared/lib/errors";

import {
  AuthTabUnavailableError,
  LoginCancelledError,
  LoginRefusedError,
  MobileAuth,
  SessionEndedError,
} from "./mobile-auth";
import type { MobileSessionRecord, MobileSessionStorage } from "./mobile-session";
import { PROOF_REFRESH, PROOF_TOKEN, proofMessage, type SessionKeyPlugin } from "./proof";

const BASE = "https://api.test";
const SESSION = "a".repeat(64);
const bearerFor = (n: number) => `pbm1_${String(n).padStart(43, "A")}`;

class FakePlugin implements SessionKeyPlugin {
  readonly keys = new Map<string, { privateKey: KeyObject; publicKey: KeyObject }>();
  browser: () => Promise<{ code: string; state: string }> = () => Promise.resolve({ code: "code-1", state: "" });
  private n = 0;

  createSessionKey() {
    const pair = generateKeyPairSync("ec", { namedCurve: "P-256" });
    const keyId = (++this.n).toString(16).padStart(32, "0");
    this.keys.set(keyId, pair);
    return Promise.resolve({
      keyId,
      publicKey: pair.publicKey.export({ type: "spki", format: "der" }).toString("base64"),
      securityLevel: "tee" as const,
    });
  }
  signWithSessionKey({ keyId, message }: { keyId: string; message: string }) {
    const pair = this.keys.get(keyId);
    if (!pair) return Promise.reject(Object.assign(new Error("x"), { code: "KEYSTORE_UNAVAILABLE" }));
    const signature = sign("sha256", Buffer.from(message, "base64"), pair.privateKey); // DER, like the Keystore
    return Promise.resolve({ signature: signature.toString("base64") });
  }
  deleteSessionKey({ keyId }: { keyId: string }) {
    this.keys.delete(keyId);
    return Promise.resolve();
  }
  loginWithBrowser({ url }: { url: string }) {
    const state = new URL(url).searchParams.get("state") ?? "";
    return this.browser().then((r) => ({ code: r.code, state: r.state || state }));
  }
}

class MemoryStorage implements MobileSessionStorage {
  record: MobileSessionRecord | undefined;
  writes: MobileSessionRecord[] = [];
  read() {
    return this.record;
  }
  write(r: MobileSessionRecord) {
    this.record = r;
    this.writes.push(r);
  }
  clear() {
    this.record = undefined;
  }
}

/** A server that verifies the proofs like the Go one does (same message layout). */
class FakeServer {
  publicKey: KeyObject | undefined;
  counter = 0;
  issued = 0;
  refreshCalls = 0;
  refreshStatus = 200;
  networkDown = false;
  tokenStatus = 200;
  apiStatuses: number[] = [];
  seenAuth: string[] = [];
  seenCounterInStorage: number[] = [];
  starts: { body: Record<string, unknown>; authorization: string | null }[] = [];
  constructor(private readonly storage: MemoryStorage) {}

  readonly fetchFn: typeof fetch = (input, init) => {
    if (this.networkDown) return Promise.reject(new TypeError("down"));
    const url = input instanceof Request ? input.url : String(input);
    const body = typeof init?.body === "string" ? (JSON.parse(init.body) as Record<string, unknown>) : {};
    const json = (status: number, payload: unknown) =>
      Promise.resolve(new Response(JSON.stringify(payload), { status, headers: { "Content-Type": "application/json" } }));
    if (url.endsWith("/auth/mobile/start")) {
      this.starts.push({ body, authorization: new Headers(init?.headers).get("Authorization") });
      this.publicKey = createPublicKey({ key: Buffer.from(String(body.session_pub), "base64"), format: "der", type: "spki" });
      return json(200, { authorization_url: `https://idp.test/authorize?state=st-1&redirect_uri=x`, state: "st-1" });
    }
    if (url.endsWith("/auth/mobile/token")) {
      if (this.tokenStatus !== 200) return json(this.tokenStatus, { error: "not_allowed" });
      if (!this.valid(String(body.proof), PROOF_TOKEN, [String(body.state), String(body.code)])) return json(400, { error: "invalid_request" });
      return json(200, { session: SESSION, access_token: bearerFor(++this.issued), expires_in: 600 });
    }
    if (url.endsWith("/auth/mobile/refresh")) {
      this.refreshCalls++;
      this.seenCounterInStorage.push(this.storage.record?.counter ?? -1);
      if (this.refreshStatus !== 200) return json(this.refreshStatus, { error: "x" });
      const counter = Number(body.counter);
      if (counter <= this.counter || !this.valid(String(body.proof), PROOF_REFRESH, [String(body.session), String(counter)])) {
        return json(401, { error: "unauthenticated" });
      }
      this.counter = counter;
      return json(200, { access_token: bearerFor(++this.issued), expires_in: 600 });
    }
    if (url.endsWith("/auth/mobile/logout")) {
      this.seenAuth.push(String(new Headers(init?.headers).get("Authorization")));
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    // An API call.
    this.seenAuth.push(String(new Headers(init?.headers).get("Authorization")));
    return Promise.resolve(new Response("{}", { status: this.apiStatuses.shift() ?? 200 }));
  };

  private valid(proof: string, domain: string, fields: string[]): boolean {
    if (!this.publicKey) return false;
    const raw = Buffer.from(proof.replaceAll("-", "+").replaceAll("_", "/"), "base64");
    return raw.length === 64 && verify("sha256", proofMessage(domain, fields), { key: this.publicKey, dsaEncoding: "ieee-p1363" }, raw);
  }
}

function setup(now = { t: 1_000_000 }) {
  const plugin = new FakePlugin();
  const storage = new MemoryStorage();
  const server = new FakeServer(storage);
  const auth = new MobileAuth({ plugin, storage, apiBase: BASE, fetchFn: server.fetchFn, now: () => now.t });
  return { plugin, storage, server, auth, now };
}

describe("proofMessage", () => {
  it("is the domain tag and each field with a 4-byte big-endian length", () => {
    expect([...proofMessage("ab", ["c", ""])]).toEqual([0, 0, 0, 2, 97, 98, 0, 0, 0, 1, 99, 0, 0, 0, 0]);
  });

  it("keeps different splits of the same text apart", () => {
    expect(proofMessage("d", ["ab", "c"])).not.toEqual(proofMessage("d", ["a", "bc"]));
  });
});

describe("MobileAuth.login", () => {
  it("signs the token proof with the Keystore key and stores only the reference, key id and counter", async () => {
    const { auth, storage, plugin } = setup();
    await auth.login();
    expect(storage.record).toEqual({ session: SESSION, keyId: [...plugin.keys.keys()][0], counter: 0 });
    expect(JSON.stringify(storage.writes)).not.toContain("pbm1_");
    await expect(auth.bearerToken()).resolves.toBe(bearerFor(1));
  });

  it("deletes the key and stores nothing when the redirect carries another state", async () => {
    const { auth, storage, plugin } = setup();
    plugin.browser = () => Promise.resolve({ code: "c", state: "another-attempt" });
    await expect(auth.login()).rejects.toBeInstanceOf(LoginRefusedError);
    expect(plugin.keys.size).toBe(0);
    expect(storage.record).toBeUndefined();
  });

  it("maps the plugin's codes and cleans up", async () => {
    for (const [code, error] of [
      ["LOGIN_CANCELLED", LoginCancelledError],
      ["AUTH_TAB_UNAVAILABLE", AuthTabUnavailableError],
      ["LOGIN_FAILED", LoginRefusedError],
    ] as const) {
      const { auth, plugin, storage } = setup();
      plugin.browser = () => Promise.reject(Object.assign(new Error("x"), { code }));
      await expect(auth.login()).rejects.toBeInstanceOf(error);
      expect(plugin.keys.size).toBe(0);
      expect(storage.record).toBeUndefined();
    }
  });

  it("reports a refusal by the server with its reason, and a server outage as unavailable", async () => {
    const a = setup();
    a.server.tokenStatus = 403;
    await expect(a.auth.login()).rejects.toMatchObject({ reason: "not_allowed" });
    expect(a.plugin.keys.size).toBe(0);
    const b = setup();
    b.server.tokenStatus = 503;
    await expect(b.auth.login()).rejects.toBeInstanceOf(AuthUnavailableError);
  });

  it("is a network error when the server cannot be reached", async () => {
    const { auth, server, plugin } = setup();
    server.networkDown = true;
    await expect(auth.login()).rejects.toBeInstanceOf(NetworkError);
    expect(plugin.keys.size).toBe(0);
  });

  it("replaces the key of a previous session", async () => {
    const { auth, plugin } = setup();
    await auth.login();
    await auth.login();
    expect(plugin.keys.size).toBe(1);
  });
});

describe("MobileAuth.stepUp", () => {
  it("asks the server to reauthenticate THIS session: step_up and its Bearer, then replaces the key", async () => {
    const { auth, server, plugin, storage } = setup();
    await auth.login();
    const firstKey = storage.record?.keyId;
    await auth.stepUp();
    expect(server.starts[1]?.body).toMatchObject({ step_up: true });
    expect(server.starts[1]?.authorization).toBe(`Bearer ${bearerFor(1)}`);
    expect(plugin.keys.size).toBe(1);
    expect(storage.record?.keyId).not.toBe(firstKey);
    await expect(auth.bearerToken()).resolves.toBe(bearerFor(2));
  });

  it("a plain login never asks for a step-up and never sends a Bearer", async () => {
    const { auth, server } = setup();
    await auth.login();
    expect(server.starts[0]?.body).not.toHaveProperty("step_up");
    expect(server.starts[0]?.authorization).toBeNull();
  });

  it("needs a session on the phone: with none it fails and leaves no key behind", async () => {
    const { auth, plugin } = setup();
    await expect(auth.stepUp()).rejects.toBeInstanceOf(SessionEndedError);
    expect(plugin.keys.size).toBe(0);
  });

  it("keeps the old session and its key when the server refuses the reauthentication", async () => {
    const { auth, server, plugin, storage } = setup();
    await auth.login();
    const before = storage.record;
    server.tokenStatus = 403;
    await expect(auth.stepUp()).rejects.toMatchObject({ reason: "not_allowed" });
    expect(storage.record).toEqual(before);
    expect(plugin.keys.size).toBe(1);
    await expect(auth.bearerToken()).resolves.toBe(bearerFor(1));
  });
});

describe("MobileAuth.bearerToken", () => {
  it("reuses the Bearer until a minute before it ends, then renews it with a proof the server accepts", async () => {
    const { auth, server, now } = setup();
    await auth.login();
    now.t += 8 * 60_000;
    await expect(auth.bearerToken()).resolves.toBe(bearerFor(1));
    expect(server.refreshCalls).toBe(0);
    now.t += 90_000; // inside the margin
    await expect(auth.bearerToken()).resolves.toBe(bearerFor(2));
    expect(server.refreshCalls).toBe(1);
  });

  it("renews once for many concurrent callers (single flight)", async () => {
    const { auth, server, now } = setup();
    await auth.login();
    now.t += 20 * 60_000;
    const tokens = await Promise.all(Array.from({ length: 8 }, () => auth.bearerToken()));
    expect(new Set(tokens).size).toBe(1);
    expect(server.refreshCalls).toBe(1);
  });

  it("saves the counter BEFORE sending the renewal, and never reuses one after a lost answer", async () => {
    const { auth, server, storage, now } = setup();
    await auth.login();
    now.t += 20 * 60_000;
    server.networkDown = true;
    await expect(auth.bearerToken()).rejects.toBeInstanceOf(NetworkError);
    expect(storage.record?.counter).toBe(1); // spent although the answer never came
    server.networkDown = false;
    await auth.bearerToken();
    expect(server.seenCounterInStorage).toEqual([2]); // already saved when the request left
    expect(server.counter).toBe(2);
  });

  it("signs out (key and record gone) when the server says the session ended", async () => {
    const { auth, server, storage, plugin, now } = setup();
    await auth.login();
    now.t += 20 * 60_000;
    server.refreshStatus = 401;
    await expect(auth.bearerToken()).rejects.toBeInstanceOf(SessionEndedError);
    expect(storage.record).toBeUndefined();
    expect(plugin.keys.size).toBe(0);
    await expect(auth.bearerToken()).rejects.toBeInstanceOf(SessionEndedError);
  });

  it("keeps the session on an outage (503/429)", async () => {
    const { auth, server, storage, now } = setup();
    await auth.login();
    now.t += 20 * 60_000;
    server.refreshStatus = 503;
    await expect(auth.bearerToken()).rejects.toBeInstanceOf(AuthUnavailableError);
    expect(storage.record).toBeDefined();
  });

  it("without a session there is nothing to renew", async () => {
    const { auth } = setup();
    await expect(auth.bearerToken()).rejects.toBeInstanceOf(SessionEndedError);
  });
});

describe("MobileAuth.authedFetch", () => {
  it("sends the Bearer and no cookie", async () => {
    const { auth, server } = setup();
    await auth.login();
    await auth.authedFetch(`${BASE}/x`);
    expect(server.seenAuth).toEqual([`Bearer ${bearerFor(1)}`]);
  });

  it("renews once after a 401 and retries once, with the new Bearer", async () => {
    const { auth, server } = setup();
    await auth.login();
    server.apiStatuses = [401, 200];
    const response = await auth.authedFetch(`${BASE}/x`);
    expect(response.status).toBe(200);
    expect(server.seenAuth).toEqual([`Bearer ${bearerFor(1)}`, `Bearer ${bearerFor(2)}`]);
  });

  it("does not loop: a second 401 is returned to the caller", async () => {
    const { auth, server } = setup();
    await auth.login();
    server.apiStatuses = [401, 401, 401];
    const response = await auth.authedFetch(`${BASE}/x`);
    expect(response.status).toBe(401);
    expect(server.refreshCalls).toBe(1);
  });

  it("a network failure is a NetworkError, not a sign-out", async () => {
    const { auth, server, storage } = setup();
    await auth.login();
    server.networkDown = true;
    await expect(auth.authedFetch(`${BASE}/x`)).rejects.toBeInstanceOf(NetworkError);
    expect(storage.record).toBeDefined();
  });
});

describe("MobileAuth.logout", () => {
  it("ends the session at the server, then deletes the key and the record", async () => {
    const { auth, server, plugin, storage } = setup();
    await auth.login();
    await auth.logout();
    expect(server.seenAuth).toEqual([`Bearer ${bearerFor(1)}`]);
    expect(plugin.keys.size).toBe(0);
    expect(storage.record).toBeUndefined();
    expect(auth.hasSession()).toBe(false);
  });

  it("wipes locally even when the server cannot be reached", async () => {
    const { auth, server, plugin, storage } = setup();
    await auth.login();
    server.networkDown = true;
    await auth.logout();
    expect(plugin.keys.size).toBe(0);
    expect(storage.record).toBeUndefined();
  });
});
