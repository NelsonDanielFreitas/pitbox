import { z } from "zod";

const codesSchema = z.object({ codes: z.array(z.string().min(1).max(64)).min(1).max(16) }).strict();

export type RecoveryCodesResult =
  | { status: "ok"; codes: string[] }
  | { status: "stepUp" | "unauthenticated" | "unavailable" };

/**
 * Generates a fresh set of single-use recovery codes (web only: the endpoint answers to the
 * session cookie, and the step-up with a strong factor is enforced by the server). The codes
 * come back once; nothing here keeps them.
 */
export async function generateRecoveryCodes(signal?: AbortSignal): Promise<RecoveryCodesResult> {
  let response: Response;
  try {
    response = await fetch("/auth/recovery-codes", {
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      signal,
    });
  } catch {
    return { status: "unavailable" };
  }
  if (response.status === 401) return { status: "unauthenticated" };
  if (response.status === 403) return { status: "stepUp" };
  if (!response.ok) return { status: "unavailable" };
  try {
    const parsed = codesSchema.safeParse(await response.json());
    return parsed.success ? { status: "ok", codes: parsed.data.codes } : { status: "unavailable" };
  } catch {
    return { status: "unavailable" };
  }
}
