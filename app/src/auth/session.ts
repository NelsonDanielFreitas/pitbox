import { z } from "zod";

import {
  AuthUnavailableError,
  ContractError,
  NetworkError,
  OriginMismatchError,
} from "@/shared/lib/errors";

const authenticatedSchema = z
  .object({
    authenticated: z.literal(true),
    user_id: z.string().uuid(),
    amr: z.array(z.string()),
    auth_time: z.iso.datetime({ offset: true }),
  })
  .strict();

const unauthenticatedSchema = z.object({ authenticated: z.literal(false) }).strict();
const errorSchema = z.object({ error: z.string() }).strict();

export type OnlineSession = z.infer<typeof authenticatedSchema> & { offline?: false };

/**
 * The server could not be reached, so the user comes from the last known user and a valid
 * offline pass (ADR 0028). It carries no factors: nothing that needs a strong factor works.
 */
export interface OfflineSession {
  authenticated: true;
  offline: true;
  user_id: string;
  amr: readonly string[];
}

export type Session = OnlineSession | OfflineSession | z.infer<typeof unauthenticatedSchema>;

/** A cancelled request is not a missing network: the caller gave up, the server did not fail. */
export function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export async function fetchSession(signal?: AbortSignal): Promise<OnlineSession | z.infer<typeof unauthenticatedSchema>> {
  let response: Response;
  try {
    response = await fetch("/auth/session", {
      cache: "no-store",
      credentials: "same-origin",
      signal,
    });
  } catch (error) {
    // Only a fetch that rejects without being cancelled is "no network". Every answer
    // from the server, even an error, takes the paths below and fails closed.
    if (isAbortError(error) || signal?.aborted) throw error;
    throw new NetworkError();
  }

  if (response.status === 401) {
    return parse(unauthenticatedSchema, response);
  }
  if (response.status === 403) {
    await parse(errorSchema, response);
    throw new OriginMismatchError();
  }
  if (response.status === 503) {
    await parse(errorSchema, response);
    throw new AuthUnavailableError();
  }
  if (!response.ok) {
    throw new ContractError();
  }
  return parse(authenticatedSchema, response);
}

async function parse<T extends z.ZodType>(schema: T, response: Response): Promise<z.infer<T>> {
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    throw new ContractError();
  }
  const parsed = schema.safeParse(body);
  if (!parsed.success) {
    throw new ContractError();
  }
  return parsed.data;
}

export function loginHref(returnTo: string): string {
  const safe = safeReturnTo(returnTo);
  return `/auth/login?return_to=${encodeURIComponent(safe)}`;
}

export function stepUpHref(returnTo: string): string {
  const safe = safeReturnTo(returnTo);
  return `/auth/login?step_up=true&return_to=${encodeURIComponent(safe)}`;
}

export function safeReturnTo(value: string): string {
  if (!value.startsWith("/") || value.startsWith("//") || value.startsWith("/\\")) {
    return "/teams";
  }
  if (value.startsWith("/auth/") || value === "/auth") {
    return "/teams";
  }
  return value;
}
