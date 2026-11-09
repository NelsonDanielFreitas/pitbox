import { beforeEach, describe, expect, it, vi } from "vitest";

const isNative = vi.fn<() => boolean>();
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => isNative() }));

const haptics = vi.hoisted(() => ({ impact: vi.fn(), notification: vi.fn() }));
vi.mock("@capacitor/haptics", () => ({
  Haptics: haptics,
  ImpactStyle: { Medium: "MEDIUM" },
  NotificationType: { Success: "SUCCESS", Warning: "WARNING" },
}));

const keepAwake = vi.hoisted(() => ({
  isSupported: vi.fn(),
  keepAwake: vi.fn(),
  allowSleep: vi.fn(),
}));
vi.mock("@capacitor-community/keep-awake", () => ({ KeepAwake: keepAwake }));

const notifications = vi.hoisted(() => ({
  checkPermissions: vi.fn(),
  requestPermissions: vi.fn(),
  schedule: vi.fn(),
  cancel: vi.fn(),
}));
vi.mock("@capacitor/local-notifications", () => ({ LocalNotifications: notifications }));

import { haptic } from "./haptics";
import { keepScreenAwake } from "./keep-awake";
import {
  cancelNotification,
  ensureNotificationPermission,
  scheduleNotification,
} from "./notifications";

const inOneMinute = () => new Date(Date.now() + 60_000);

beforeEach(() => {
  vi.clearAllMocks();
  isNative.mockReturnValue(true);
});

describe("on the web", () => {
  beforeEach(() => isNative.mockReturnValue(false));

  it("does nothing and calls no plugin", async () => {
    await haptic("tap");
    const release = await keepScreenAwake();
    await release();
    expect(await ensureNotificationPermission()).toBe("unsupported");
    expect(await scheduleNotification({ id: 1, title: "t", body: "b", at: inOneMinute() })).toBe(false);
    await cancelNotification(1);
    for (const plugin of [haptics, keepAwake, notifications]) {
      for (const fn of Object.values(plugin)) expect(fn).not.toHaveBeenCalled();
    }
  });
});

describe("haptics", () => {
  it("maps each kind to a plugin call", async () => {
    await haptic("tap");
    await haptic("success");
    await haptic("warning");
    expect(haptics.impact).toHaveBeenCalledWith({ style: "MEDIUM" });
    expect(haptics.notification).toHaveBeenCalledWith({ type: "SUCCESS" });
    expect(haptics.notification).toHaveBeenCalledWith({ type: "WARNING" });
  });

  it("never throws when the phone refuses", async () => {
    haptics.impact.mockRejectedValue(new Error("no vibrator"));
    await expect(haptic("tap")).resolves.toBeUndefined();
  });
});

describe("keepScreenAwake", () => {
  it("keeps the screen on and gives it back", async () => {
    keepAwake.isSupported.mockResolvedValue({ isSupported: true });
    const release = await keepScreenAwake();
    expect(keepAwake.keepAwake).toHaveBeenCalledTimes(1);
    await release();
    expect(keepAwake.allowSleep).toHaveBeenCalledTimes(1);
  });

  it("does nothing when the phone does not support it", async () => {
    keepAwake.isSupported.mockResolvedValue({ isSupported: false });
    const release = await keepScreenAwake();
    await release();
    expect(keepAwake.keepAwake).not.toHaveBeenCalled();
    expect(keepAwake.allowSleep).not.toHaveBeenCalled();
  });

  it("degrades to a no-op when the plugin fails", async () => {
    keepAwake.isSupported.mockRejectedValue(new Error("boom"));
    const release = await keepScreenAwake();
    await expect(release()).resolves.toBeUndefined();
  });
});

describe("notifications", () => {
  it("asks for the permission only when it is not granted yet", async () => {
    notifications.checkPermissions.mockResolvedValue({ display: "granted" });
    expect(await ensureNotificationPermission()).toBe("granted");
    expect(notifications.requestPermissions).not.toHaveBeenCalled();

    notifications.checkPermissions.mockResolvedValue({ display: "prompt" });
    notifications.requestPermissions.mockResolvedValue({ display: "denied" });
    expect(await ensureNotificationPermission()).toBe("denied");
  });

  it("fails closed when the permission cannot be read", async () => {
    notifications.checkPermissions.mockRejectedValue(new Error("boom"));
    expect(await ensureNotificationPermission()).toBe("denied");
  });

  it("schedules one notification that may fire while the phone idles", async () => {
    notifications.checkPermissions.mockResolvedValue({ display: "granted" });
    const at = inOneMinute();
    expect(await scheduleNotification({ id: 7, title: "Reabastecer", body: "Daqui a 2 voltas", at })).toBe(true);
    expect(notifications.schedule).toHaveBeenCalledWith({
      notifications: [
        { id: 7, title: "Reabastecer", body: "Daqui a 2 voltas", schedule: { at, allowWhileIdle: true } },
      ],
    });
  });

  it("does not schedule without permission, and says so", async () => {
    notifications.checkPermissions.mockResolvedValue({ display: "denied" });
    notifications.requestPermissions.mockResolvedValue({ display: "denied" });
    expect(await scheduleNotification({ id: 7, title: "t", body: "b", at: inOneMinute() })).toBe(false);
    expect(notifications.schedule).not.toHaveBeenCalled();
  });

  it("returns false when the phone refuses to schedule", async () => {
    notifications.checkPermissions.mockResolvedValue({ display: "granted" });
    notifications.schedule.mockRejectedValue(new Error("boom"));
    expect(await scheduleNotification({ id: 7, title: "t", body: "b", at: inOneMinute() })).toBe(false);
  });

  it.each([
    ["id zero", { id: 0, title: "t", body: "b" }],
    ["id too large", { id: 2 ** 31, title: "t", body: "b" }],
    ["fractional id", { id: 1.5, title: "t", body: "b" }],
    ["empty title", { id: 1, title: "", body: "b" }],
    ["long title", { id: 1, title: "x".repeat(81), body: "b" }],
    ["long body", { id: 1, title: "t", body: "x".repeat(201) }],
  ])("rejects a malformed request: %s", async (_name, partial) => {
    await expect(scheduleNotification({ ...partial, at: inOneMinute() })).rejects.toThrow(RangeError);
    expect(notifications.schedule).not.toHaveBeenCalled();
  });

  it("rejects a time in the past", async () => {
    await expect(
      scheduleNotification({ id: 1, title: "t", body: "b", at: new Date(Date.now() - 1) }),
    ).rejects.toThrow(RangeError);
  });

  it("cancels by id and ignores a notification that is already gone", async () => {
    notifications.cancel.mockRejectedValue(new Error("not found"));
    await expect(cancelNotification(7)).resolves.toBeUndefined();
    expect(notifications.cancel).toHaveBeenCalledWith({ notifications: [{ id: 7 }] });
  });
});
