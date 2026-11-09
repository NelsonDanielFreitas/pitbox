import { createConnectTransport } from "@connectrpc/connect-web";

import { mobileAuth } from "@/auth/native";
import { env } from "@/shared/lib/env";
import { isNativePlatform } from "@/shared/lib/platform";

export function createPitboxTransport() {
  if (!isNativePlatform()) return createConnectTransport({ baseUrl: env.apiBaseUrl });
  // The Android shell authenticates with the Bearer, never with a cookie (ADR 0032).
  return createConnectTransport({
    baseUrl: env.apiBaseUrl,
    fetch: (input, init) => {
      if (typeof input !== "string" && !(input instanceof URL)) {
        return Promise.reject(new TypeError("unsupported request"));
      }
      return mobileAuth().authedFetch(String(input), init);
    },
  });
}
