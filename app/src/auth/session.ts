import { z } from "zod";

import { AuthUnavailableError, ContractError, OriginMismatchError } from "@/shared/lib/errors";

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

export type Session = z.infer<typeof authenticatedSchema> | z.infer<typeof unauthenticatedSchema>;

export async function fetchSession(signal?: AbortSignal): Promise<Session> {
  const response = await fetch("/auth/session", {
    cache: "no-store",
    credentials: "same-origin",
    signal,
  });

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

export function safeReturnTo(value: string): string {
  if (!value.startsWith("/") || value.startsWith("//") || value.startsWith("/\\")) {
    return "/teams";
  }
  if (value.startsWith("/auth/") || value === "/auth") {
    return "/teams";
  }
  return value;
}
