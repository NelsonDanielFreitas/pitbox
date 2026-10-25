import { createConnectTransport } from "@connectrpc/connect-web";

import { env } from "@/shared/lib/env";

export function createPitboxTransport() {
  return createConnectTransport({ baseUrl: env.apiBaseUrl });
}
