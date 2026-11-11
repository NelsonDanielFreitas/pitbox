import { z } from "zod";

import { accountRequest, failureOf } from "@/features/account/data/account-api";

const codesSchema = z.object({ codes: z.array(z.string().min(1).max(64)).min(1).max(16) }).strict();

export type RecoveryCodesResult =
  | { status: "ok"; codes: string[] }
  | { status: "stepUp" | "unauthenticated" | "unavailable" };

/**
 * Generates a fresh set of single-use recovery codes, on the web (cookie) and in the Android
 * shell (Bearer). The server enforces the step-up: a strong factor used in the last 5 minutes.
 * The codes come back once; nothing here keeps them.
 */
export async function generateRecoveryCodes(signal?: AbortSignal): Promise<RecoveryCodesResult> {
  const answer = await accountRequest("/auth/recovery-codes", "POST", signal);
  if (answer.status !== "response") return { status: answer.status };
  if (!answer.response.ok) return failureOf(answer.response);
  try {
    const parsed = codesSchema.safeParse(await answer.response.json());
    return parsed.success ? { status: "ok", codes: parsed.data.codes } : { status: "unavailable" };
  } catch {
    return { status: "unavailable" };
  }
}
