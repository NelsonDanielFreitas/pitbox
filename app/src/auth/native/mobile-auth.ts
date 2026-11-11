import { z } from "zod";

import { AuthUnavailableError, ContractError, NetworkError } from "@/shared/lib/errors";

import type { MobileSessionRecord, MobileSessionStorage } from "./mobile-session";
import { newPkce, PROOF_REFRESH, PROOF_TOKEN, signProof, type SessionKeyPlugin } from "./proof";

/** Renew this long before the Bearer's end, so a request never meets an expired one. */
const MARGIN_MS = 60_000;

const startSchema = z.object({ authorization_url: z.string().url(), state: z.string().min(1) }).strict();
const bearerSchema = z
  .object({
    session: z.string().regex(/^[0-9a-f]{64}$/).optional(),
    access_token: z.string().regex(/^pbm1_[A-Za-z0-9_-]{43}$/),
    expires_in: z.number().int().positive().max(3600),
  })
  .strict();

export class LoginCancelledError extends Error {
  constructor() {
    super("login cancelled");
    this.name = "LoginCancelledError";
  }
}

export class AuthTabUnavailableError extends Error {
  constructor() {
    super("auth tab unavailable");
    this.name = "AuthTabUnavailableError";
  }
}

/** The server refused the login (not allowed, email not verified, bad state...). */
export class LoginRefusedError extends Error {
  constructor(readonly reason: string) {
    super("login refused");
    this.name = "LoginRefusedError";
  }
}

/** The session is gone (revoked, expired, reuse alarm): signed out, the key is deleted. */
export class SessionEndedError extends Error {
  constructor() {
    super("session ended");
    this.name = "SessionEndedError";
  }
}

export interface MobileAuthDeps {
  plugin: SessionKeyPlugin;
  storage: MobileSessionStorage;
  /** Absolute API origin, e.g. https://host. */
  apiBase: string;
  fetchFn?: typeof fetch;
  now?: () => number;
}

function pluginCode(error: unknown): string | undefined {
  if (typeof error === "object" && error !== null && "code" in error) {
    const code: unknown = error.code;
    return typeof code === "string" ? code : undefined;
  }
  return undefined;
}

/**
 * The phone's side of the mobile login (ADR 0032). The Bearer lives in memory only; the refresh
 * token never reaches the phone. Renewals are single-flight and signed with the Keystore key; the
 * counter is saved BEFORE a renewal is sent, so a lost response leaves a gap (allowed by the
 * server) and never a repeat (which the server treats as theft).
 */
export class MobileAuth {
  private bearer: { token: string; expiresAt: number } | undefined;
  private inflight: Promise<string> | undefined;
  private readonly fetchFn: typeof fetch;
  private readonly now: () => number;

  constructor(private readonly deps: MobileAuthDeps) {
    this.fetchFn = deps.fetchFn ?? ((input, init) => fetch(input, init));
    this.now = deps.now ?? Date.now;
  }

  hasSession(): boolean {
    return this.deps.storage.read() !== undefined;
  }

  /**
   * Reauthenticates this phone's session (the "step-up" before a sensitive action): a new login
   * that must come back as the same account with a strong factor. The server replaces the old
   * session with the new one, so a failure leaves the old session as it was.
   */
  async stepUp(): Promise<void> {
    return this.login({ stepUp: true });
  }

  /** Login in three steps. On any failure the key made for this attempt is deleted. */
  async login(options: { stepUp?: boolean } = {}): Promise<void> {
    const { plugin, storage } = this.deps;
    let keyId: string | undefined;
    try {
      const key = await plugin.createSessionKey();
      keyId = key.keyId;
      const { verifier, challenge } = await newPkce();
      const start = options.stepUp
        ? startSchema.parse(
            await this.postJson(
              "/auth/mobile/start",
              { session_pub: key.publicKey, code_challenge: challenge, step_up: true },
              { Authorization: `Bearer ${await this.bearerToken()}` },
            ),
          )
        : startSchema.parse(
            await this.postJson("/auth/mobile/start", { session_pub: key.publicKey, code_challenge: challenge }),
          );
      let redirect: { code: string; state: string };
      try {
        redirect = await plugin.loginWithBrowser({ url: start.authorization_url });
      } catch (error) {
        const code = pluginCode(error);
        if (code === "LOGIN_CANCELLED") throw new LoginCancelledError();
        if (code === "AUTH_TAB_UNAVAILABLE") throw new AuthTabUnavailableError();
        throw new LoginRefusedError("browser");
      }
      // The state is ours: a redirect for another attempt never reaches the server.
      if (redirect.state !== start.state) throw new LoginRefusedError("state");
      const proof = await signProof(plugin, keyId, PROOF_TOKEN, [redirect.state, redirect.code]);
      const issued = bearerSchema.parse(
        await this.postJson("/auth/mobile/token", {
          state: redirect.state,
          code: redirect.code,
          code_verifier: verifier,
          proof,
        }),
      );
      if (issued.session === undefined) throw new ContractError();
      // A previous session of this phone, if any, is replaced: its key goes too.
      const previous = this.stopSession();
      if (previous) await plugin.deleteSessionKey({ keyId: previous.keyId }).catch(() => undefined);
      storage.write({ session: issued.session, keyId, counter: 0 });
      this.keep(issued.access_token, issued.expires_in);
    } catch (error) {
      if (keyId !== undefined) await plugin.deleteSessionKey({ keyId }).catch(() => undefined);
      if (error instanceof z.ZodError) throw new ContractError();
      throw error;
    }
  }

  /** A valid Bearer, renewing it first when needed. Rejects with SessionEndedError when signed out. */
  async bearerToken(): Promise<string> {
    if (this.bearer && this.now() < this.bearer.expiresAt - MARGIN_MS) return this.bearer.token;
    return this.renew();
  }

  /** The Bearer was refused (401): forget it and get a new one, once. */
  async renewAfterRejection(rejected: string): Promise<string> {
    if (this.bearer?.token === rejected) this.bearer = undefined;
    return this.bearerToken();
  }

  /** `fetch` with the Bearer; one renewal and one retry after a 401. */
  async authedFetch(input: string, init: RequestInit = {}): Promise<Response> {
    const send = (token: string) => {
      const headers = new Headers(init.headers);
      headers.set("Authorization", `Bearer ${token}`);
      return this.fetchFn(input, { ...init, headers, credentials: "omit" });
    };
    const token = await this.bearerToken();
    let response: Response;
    try {
      response = await send(token);
    } catch (error) {
      if (init.signal?.aborted) throw error;
      throw new NetworkError();
    }
    if (response.status !== 401) return response;
    const fresh = await this.renewAfterRejection(token);
    try {
      return await send(fresh);
    } catch (error) {
      if (init.signal?.aborted) throw error;
      throw new NetworkError();
    }
  }

  /**
   * Ends the session at the server (best effort: the local wipe never waits on the network
   * forever) and then deletes the key and everything in memory.
   */
  async logout(): Promise<void> {
    const token = this.bearer?.token ?? (this.hasSession() ? await this.bearerToken().catch(() => undefined) : undefined);
    if (token !== undefined) {
      await this.fetchFn(`${this.deps.apiBase}/auth/mobile/logout`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: "{}",
        credentials: "omit",
        signal: AbortSignal.timeout(8000),
      }).catch(() => undefined);
    }
    await this.stopSessionAndKey();
  }

  private renew(): Promise<string> {
    this.inflight ??= this.doRenew().finally(() => {
      this.inflight = undefined;
    });
    return this.inflight;
  }

  private async doRenew(): Promise<string> {
    const record = this.deps.storage.read();
    if (!record) throw new SessionEndedError();
    const counter = record.counter + 1;
    // Saved first: if the answer is lost, the next attempt uses a higher number.
    this.deps.storage.write({ ...record, counter });
    const proof = await signProof(this.deps.plugin, record.keyId, PROOF_REFRESH, [record.session, String(counter)]);
    let response: Response;
    try {
      response = await this.fetchFn(`${this.deps.apiBase}/auth/mobile/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session: record.session, counter, proof }),
        credentials: "omit",
        signal: AbortSignal.timeout(15_000),
      });
    } catch {
      throw new NetworkError();
    }
    if (response.status === 401) {
      await this.stopSessionAndKey();
      throw new SessionEndedError();
    }
    if (response.status === 503 || response.status === 429) throw new AuthUnavailableError();
    if (!response.ok) throw new ContractError();
    const parsed = bearerSchema.safeParse(await response.json().catch(() => undefined));
    if (!parsed.success) throw new ContractError();
    this.keep(parsed.data.access_token, parsed.data.expires_in);
    return parsed.data.access_token;
  }

  private keep(token: string, expiresInSeconds: number): void {
    this.bearer = { token, expiresAt: this.now() + expiresInSeconds * 1000 };
  }

  private stopSession(): MobileSessionRecord | undefined {
    const record = this.deps.storage.read();
    this.bearer = undefined;
    this.deps.storage.clear();
    return record;
  }

  private async stopSessionAndKey(): Promise<void> {
    const record = this.stopSession();
    if (record) await this.deps.plugin.deleteSessionKey({ keyId: record.keyId }).catch(() => undefined);
  }

  private async postJson(path: string, body: unknown, extraHeaders: Record<string, string> = {}): Promise<unknown> {
    let response: Response;
    try {
      response = await this.fetchFn(`${this.deps.apiBase}${path}`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...extraHeaders },
        body: JSON.stringify(body),
        credentials: "omit",
        signal: AbortSignal.timeout(15_000),
      });
    } catch {
      throw new NetworkError();
    }
    const payload: unknown = await response.json().catch(() => undefined);
    if (response.ok) return payload;
    if (response.status === 503 || response.status === 429) throw new AuthUnavailableError();
    const reason = z.object({ error: z.string() }).safeParse(payload);
    throw new LoginRefusedError(reason.success ? reason.data.error : "unknown");
  }
}
