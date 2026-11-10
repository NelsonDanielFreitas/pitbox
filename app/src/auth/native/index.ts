import { PitboxSecure } from "pitbox-secure";

import { env } from "@/shared/lib/env";

import { MobileAuth } from "./mobile-auth";
import { localMobileSessionStorage } from "./mobile-session";

export * from "./mobile-auth";

let instance: MobileAuth | undefined;

/** The one login of this phone. Only call it inside the Android shell. */
export function mobileAuth(): MobileAuth {
  instance ??= new MobileAuth({
    plugin: PitboxSecure,
    storage: localMobileSessionStorage(),
    apiBase: env.apiBaseUrl,
  });
  return instance;
}
