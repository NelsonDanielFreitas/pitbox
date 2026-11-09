import { registerPlugin } from "@capacitor/core";

import type { PitboxSecurePlugin } from "./definitions";

/**
 * No web implementation on purpose: outside the Capacitor shell every call rejects as
 * "not implemented". The web keeps its own non-extractable WebCrypto key (ADR 0025).
 */
const PitboxSecure = registerPlugin<PitboxSecurePlugin>("PitboxSecure");

export * from "./definitions";
export { PitboxSecure };
