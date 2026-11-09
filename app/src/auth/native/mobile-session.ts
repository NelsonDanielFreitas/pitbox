import { z } from "zod";

/**
 * What the phone remembers between runs of the app (ADR 0032): the session reference, the id of
 * its Keystore key and the renewal counter. None of it is a secret: without the key in the
 * Keystore the reference opens nothing. The Bearer itself is never stored.
 */
const recordSchema = z
  .object({
    session: z.string().regex(/^[0-9a-f]{64}$/),
    keyId: z.string().regex(/^[0-9a-f]{32}$/),
    counter: z.number().int().min(0).max(2 ** 40),
  })
  .strict();

export type MobileSessionRecord = z.infer<typeof recordSchema>;

export interface MobileSessionStorage {
  read(): MobileSessionRecord | undefined;
  write(record: MobileSessionRecord): void;
  clear(): void;
}

const KEY = "pitbox.mobile-session.v1";

/** localStorage of the WebView: private to the app, and Android backup is off (manifest). */
export function localMobileSessionStorage(): MobileSessionStorage {
  return {
    read() {
      try {
        const raw = localStorage.getItem(KEY);
        if (raw === null) return undefined;
        const parsed = recordSchema.safeParse(JSON.parse(raw));
        return parsed.success ? parsed.data : undefined;
      } catch {
        return undefined;
      }
    },
    write(record) {
      // Fail loudly: a counter that was not saved must not be used.
      localStorage.setItem(KEY, JSON.stringify(recordSchema.parse(record)));
    },
    clear() {
      try {
        localStorage.removeItem(KEY);
      } catch {
        // nothing to clear
      }
    },
  };
}
