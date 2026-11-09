import { Capacitor } from "@capacitor/core";

/**
 * True inside the Capacitor shell (Android), false in a browser. This is the single place that
 * decides which engine and which key store the app uses (ADR 0033), and whether the service
 * worker exists at all (ADR 0028 applies to the web only).
 */
export function isNativePlatform(): boolean {
  return Capacitor.isNativePlatform();
}
