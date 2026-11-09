import fc from "fast-check";
import { describe, expect, it, vi } from "vitest";

import { AppLockController, LOCK_AFTER_MS } from "./app-lock";

function setup(initial: "locked" | "unlocked" = "unlocked") {
  let now = 1_000_000;
  const controller = new AppLockController(() => now, LOCK_AFTER_MS, initial);
  return {
    controller,
    advance: (ms: number) => {
      now += ms;
    },
    setNow: (value: number) => {
      now = value;
    },
  };
}

describe("AppLockController", () => {
  it("starts locked: the time spent away is unknown after the process dies", () => {
    expect(new AppLockController().getState()).toBe("locked");
  });

  it("stays unlocked when the app comes back before 15 minutes", () => {
    const { controller, advance } = setup();
    controller.goneToBackground();
    advance(LOCK_AFTER_MS - 1);
    controller.cameToForeground();
    expect(controller.getState()).toBe("unlocked");
  });

  it("locks at exactly 15 minutes", () => {
    const { controller, advance } = setup();
    controller.goneToBackground();
    advance(LOCK_AFTER_MS);
    controller.cameToForeground();
    expect(controller.getState()).toBe("locked");
  });

  it("locks when the clock went backwards", () => {
    const { controller, setNow } = setup();
    controller.goneToBackground();
    setNow(0);
    controller.cameToForeground();
    expect(controller.getState()).toBe("locked");
  });

  it("locks when the clock is not a number", () => {
    const controller = new AppLockController(() => Number.NaN);
    controller.unlock();
    controller.goneToBackground();
    controller.cameToForeground();
    expect(controller.getState()).toBe("locked");
  });

  it("does not restart the countdown on a repeated background event", () => {
    const { controller, advance } = setup();
    controller.goneToBackground();
    advance(LOCK_AFTER_MS - 1000);
    controller.goneToBackground();
    advance(1000);
    controller.cameToForeground();
    expect(controller.getState()).toBe("locked");
  });

  it("ignores a foreground event that was never preceded by a background one", () => {
    const { controller } = setup();
    controller.cameToForeground();
    expect(controller.getState()).toBe("unlocked");
  });

  it("stays locked through any trip to the background until unlocked", () => {
    const { controller, advance } = setup("locked");
    controller.goneToBackground();
    advance(1000);
    controller.cameToForeground();
    expect(controller.getState()).toBe("locked");
    controller.unlock();
    expect(controller.getState()).toBe("unlocked");
  });

  it("tells subscribers only when the state changes", () => {
    const { controller } = setup();
    const listener = vi.fn();
    const stop = controller.subscribe(listener);
    controller.unlock(); // already unlocked
    expect(listener).not.toHaveBeenCalled();
    controller.lock();
    expect(listener).toHaveBeenCalledTimes(1);
    stop();
    controller.unlock();
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("locks if and only if the elapsed time is outside [0, limit) (property)", () => {
    fc.assert(
      fc.property(fc.integer({ min: -2 * LOCK_AFTER_MS, max: 3 * LOCK_AFTER_MS }), (elapsed) => {
        const { controller, advance } = setup();
        controller.goneToBackground();
        advance(elapsed);
        controller.cameToForeground();
        expect(controller.getState() === "locked").toBe(elapsed < 0 || elapsed >= LOCK_AFTER_MS);
      }),
    );
  });
});
