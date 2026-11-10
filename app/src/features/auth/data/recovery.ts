import { z } from "zod";

import { apiUrl } from "@/shared/lib/env";

/** A plain address, trimmed and lowercased, at most 254 characters (RFC 5321). */
export const recoveryEmailSchema = z.string().trim().toLowerCase().max(254).pipe(z.email());

/**
 * What the server said. `accepted` means "asked", never "an email was sent": the answer is the
 * same whether the account exists or not (ADR 0034), so the screen must not claim more.
 */
export type RecoveryOutcome = "accepted" | "invalid" | "rateLimited" | "unavailable";

/**
 * Asks for a passkey registration link by email. Public endpoint: no cookie and no Bearer go with
 * it. A network failure or any other answer is `unavailable` (fail closed, no detail).
 */
export async function requestRecovery(email: string, signal?: AbortSignal): Promise<RecoveryOutcome> {
  const parsed = recoveryEmailSchema.safeParse(email);
  if (!parsed.success) return "invalid";
  let response: Response;
  try {
    response = await fetch(apiUrl("/auth/recover"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: parsed.data }),
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
