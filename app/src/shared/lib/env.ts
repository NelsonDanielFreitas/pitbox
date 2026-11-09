import { isNativePlatform } from "@/shared/lib/platform";

/**
 * On the web the API shares the page's origin (BFF, ADR 0017). Inside the Android shell the page is
 * served from https://localhost by Capacitor, which is not the API: the build injects the real
 * origin (PITBOX_API_ORIGIN, validated in vite.config.ts). Without it the native app has no API
 * and says so instead of asking its own asset server.
 */
function apiBaseUrl(): string {
  if (!isNativePlatform()) return window.location.origin;
  const origin: unknown = import.meta.env.VITE_API_ORIGIN;
  return typeof origin === "string" ? origin : "";
}

export const env = {
  apiBaseUrl: apiBaseUrl(),
  isDev: import.meta.env.DEV,
} as const;

/** URL for an API path: relative on the web (same origin), absolute inside the native shell. */
export function apiUrl(path: string): string {
  return isNativePlatform() ? `${env.apiBaseUrl}${path}` : path;
}
