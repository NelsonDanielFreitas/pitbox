import { LocalNotifications } from "@capacitor/local-notifications";

import { isNativePlatform } from "@/shared/lib/platform";

export type NotificationPermission = "granted" | "denied" | "unsupported";

const MAX_ID = 2 ** 31 - 1; // Android notification ids are 32-bit ints
const MAX_TITLE = 80;
const MAX_BODY = 200;

export interface LocalNotificationRequest {
  /** Chosen by the caller so it can cancel the same one later (positive 32-bit integer). */
  id: number;
  title: string;
  body: string;
  at: Date;
}

/**
 * Asks for the permission when it is still undecided. "unsupported" is the web: the PWA cannot
 * schedule a notification with the screen locked, which is why the app exists (F2-06).
 */
export async function ensureNotificationPermission(): Promise<NotificationPermission> {
  if (!isNativePlatform()) return "unsupported";
  try {
    const current = await LocalNotifications.checkPermissions();
    if (current.display === "granted") return "granted";
    const asked = await LocalNotifications.requestPermissions();
    return asked.display === "granted" ? "granted" : "denied";
  } catch {
    return "denied"; // fail closed: if we cannot tell, nothing is scheduled
  }
}

/**
 * Schedules one notification for a moment in the future. The text appears on the lock screen: it
 * must not carry anything the team would not want a stranger to read (no setup values, no names).
 * Returns false when it was not scheduled (web, no permission, or the phone refused); a request
 * that is malformed is a bug of the caller and throws.
 */
export async function scheduleNotification(request: LocalNotificationRequest): Promise<boolean> {
  const { id, title, body, at } = request;
  if (!Number.isSafeInteger(id) || id < 1 || id > MAX_ID) throw new RangeError("invalid notification id");
  if (title.length < 1 || title.length > MAX_TITLE) throw new RangeError("invalid notification title");
  if (body.length > MAX_BODY) throw new RangeError("invalid notification body");
  if (!(at.getTime() > Date.now())) throw new RangeError("notification time must be in the future");

  if ((await ensureNotificationPermission()) !== "granted") return false;
  try {
    // allowWhileIdle: in Doze the refuel warning has to arrive on time (spec §3).
    await LocalNotifications.schedule({
      notifications: [{ id, title, body, schedule: { at, allowWhileIdle: true } }],
    });
    return true;
  } catch {
    return false;
  }
}

export async function cancelNotification(id: number): Promise<void> {
  if (!isNativePlatform()) return;
  try {
    await LocalNotifications.cancel({ notifications: [{ id }] });
  } catch {
    // Already fired or never scheduled: the end state is the one asked for.
  }
}
