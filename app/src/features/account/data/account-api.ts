import { z } from "zod";

import { mobileAuth, SessionEndedError } from "@/auth/native";
import { apiUrl } from "@/shared/lib/env";
import { isNativePlatform } from "@/shared/lib/platform";

/**
 * The account endpoints (sessions and recovery codes). On the web they answer to the session
 * cookie; inside the Android shell to the Bearer of the phone's session (ADR 0032). The
 * server decides what the caller may do; nothing here is kept after the call.
 */
export type Failure = { status: "unauthenticated" | "stepUp" | "unavailable" };

/** The raw answer, or why there is none. A network failure is "unavailable", never a crash. */
export async function accountRequest(path: string, method: "GET" | "POST" | "DELETE", signal?: AbortSignal) {
  try {
    if (isNativePlatform()) {
      if (!mobileAuth().hasSession()) return { status: "unauthenticated" } as const;
      return { status: "response", response: await mobileAuth().authedFetch(apiUrl(path), { method, cache: "no-store", signal }) } as const;
    }
    return {
      status: "response",
      response: await fetch(apiUrl(path), { method, credentials: "same-origin", cache: "no-store", signal }),
    } as const;
  } catch (error) {
    if (error instanceof SessionEndedError) return { status: "unauthenticated" } as const;
    return { status: "unavailable" } as const;
  }
}

/** The failure a non-2xx answer stands for. */
export function failureOf(response: Response): Failure {
  if (response.status === 401) return { status: "unauthenticated" };
  if (response.status === 403) return { status: "stepUp" };
  return { status: "unavailable" };
}

async function parsed<T extends z.ZodType>(schema: T, response: Response): Promise<z.infer<T> | undefined> {
  try {
    const result = schema.safeParse(await response.json());
    return result.success ? result.data : undefined;
  } catch {
    return undefined;
  }
}

const sessionSchema = z
  .object({
    ref: z.string().regex(/^[0-9a-f]{64}$/),
    kind: z.enum(["web", "mobile"]),
    created_at: z.iso.datetime({ offset: true }),
    auth_time: z.iso.datetime({ offset: true }),
    user_agent: z.string().max(1024),
    ip: z.string().max(64),
    current: z.boolean(),
  })
  .strict();
const sessionsSchema = z.object({ sessions: z.array(sessionSchema).max(200) }).strict();
export type SessionItem = z.infer<typeof sessionSchema>;

export type SessionsResult = { status: "ok"; sessions: SessionItem[] } | Failure;

export async function listSessions(signal?: AbortSignal): Promise<SessionsResult> {
  const answer = await accountRequest("/auth/sessions", "GET", signal);
  if (answer.status !== "response") return { status: answer.status };
  if (!answer.response.ok) return failureOf(answer.response);
  const body = await parsed(sessionsSchema, answer.response);
  return body ? { status: "ok", sessions: body.sessions } : { status: "unavailable" };
}

export type ActionResult = { status: "ok" } | Failure;

/** Ends one session of the user. Another user's or an unknown reference is the same "gone". */
export async function revokeSession(ref: string): Promise<ActionResult> {
  if (!/^[0-9a-f]{64}$/.test(ref)) return { status: "unavailable" };
  const answer = await accountRequest(`/auth/sessions/${ref}`, "DELETE");
  if (answer.status !== "response") return { status: answer.status };
  if (answer.response.status === 204 || answer.response.status === 404) return { status: "ok" };
  return failureOf(answer.response);
}

const revokedSchema = z.object({ revoked: z.number().int().min(0).max(1000) }).strict();

export async function revokeOtherSessions(): Promise<({ status: "ok"; revoked: number }) | Failure> {
  const answer = await accountRequest("/auth/sessions", "DELETE");
  if (answer.status !== "response") return { status: answer.status };
  if (!answer.response.ok) return failureOf(answer.response);
  const body = await parsed(revokedSchema, answer.response);
  return body ? { status: "ok", revoked: body.revoked } : { status: "unavailable" };
}

const codesStatusSchema = z.object({ unused: z.number().int().min(0).max(64) }).strict();

export async function recoveryCodesStatus(signal?: AbortSignal): Promise<({ status: "ok"; unused: number }) | Failure> {
  const answer = await accountRequest("/auth/recovery-codes", "GET", signal);
  if (answer.status !== "response") return { status: answer.status };
  if (!answer.response.ok) return failureOf(answer.response);
  const body = await parsed(codesStatusSchema, answer.response);
  return body ? { status: "ok", unused: body.unused } : { status: "unavailable" };
}
