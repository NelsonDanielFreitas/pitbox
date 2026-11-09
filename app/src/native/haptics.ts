import { Haptics, ImpactStyle, NotificationType } from "@capacitor/haptics";

import { isNativePlatform } from "@/shared/lib/platform";

export type HapticKind = "tap" | "success" | "warning";

/**
 * Short vibration as feedback for a touch. It is a nicety, never part of the result: on the web, or
 * when the phone refuses, it does nothing and never throws.
 */
export async function haptic(kind: HapticKind): Promise<void> {
  if (!isNativePlatform()) return;
  try {
    if (kind === "tap") await Haptics.impact({ style: ImpactStyle.Medium });
    else await Haptics.notification({ type: kind === "success" ? NotificationType.Success : NotificationType.Warning });
  } catch {
    // Feedback only: a phone without a vibrator must not break the action that asked for it.
  }
}
