import { z } from "zod";

import { apiUrl } from "@/shared/lib/env";

/** A plain address, trimmed and lowercased, at most 254 characters (RFC 5321). */
export const recoveryEmailSchema = z.string().trim().toLowerCase().max(254).pipe(z.email());

/**
 * A recovery code as typed: letters, digits and dashes or spaces between groups. The server
 * normalizes and checks it; this only bounds the size. Empty means "I have no code".
 */
export const recoveryCodeSchema = z
  .string()
  .trim()
  .max(64)
  .regex(/^[A-Za-z2-7\s-]*$/);

/**
 * What the server said. `accepted` means "asked", never "an email was sent": the answer is the
 * same whether the account exists or not (ADR 0034), so the screen must not claim more.
 */
export type RecoveryOutcome = "accepted" | "invalid" | "rateLimited" | "unavailable";

/**
 * Asks for a passkey registration link by email. An account with recovery codes needs one of
 * them besides the email (ADR 0035); without a code the request still gets the same answer, and
 * nothing is sent. Public endpoint: no cookie and no Bearer go with it. A network failure or any
 * other answer is `unavailable` (fail closed, no detail). The code is never stored or logged.
 */
export async function requestRecovery(email: string, code = "", signal?: AbortSignal): Promise<RecoveryOutcome> {
  const parsed = recoveryEmailSchema.safeParse(email);
  const parsedCode = recoveryCodeSchema.safeParse(code);
  if (!parsed.success || !parsedCode.success) return "invalid";
  let response: Response;
  try {
    response = await fetch(apiUrl("/auth/recover"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(parsedCode.data === "" ? { email: parsed.data } : { email: parsed.data, code: parsedCode.data }),
      credentials: "omit",
      cache: "no-store",
      signal,
    });
  } catch {
    return "unavailable";
  }
  if (response.status === 202) return "accepted";
  if (response.status === 400) return "invalid";
  if (response.status === 429) return "rateLimited";
  return "unavailable";
}
