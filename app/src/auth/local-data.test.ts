import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  clearLocalUserData: vi.fn(),
  flushBeforeLogout: vi.fn(),
  stopSync: vi.fn(),
}));
vi.mock("@/db/lifecycle", () => ({ clearLocalUserData: mocks.clearLocalUserData }));
vi.mock("@/sync/runtime", () => ({
  flushBeforeLogout: mocks.flushBeforeLogout,
  stopSync: mocks.stopSync,
  UnsyncedChangesError: class UnsyncedChangesError extends Error {},
}));

import { clearLocalData } from "./local-data";
import { lastUserKey, readLastUserId, writeLastUserId } from "./last-user";

const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";

describe("clearLocalData", () => {
  afterEach(() => {
    vi.resetAllMocks();
    window.localStorage.clear();
  });

  it("forgets the last user after an explicit logout", async () => {
    writeLastUserId(userId);
    await clearLocalData(userId);
    expect(mocks.clearLocalUserData).toHaveBeenCalledWith(userId);
    expect(window.localStorage.getItem(lastUserKey)).toBeNull();
  });

  it("keeps the last user when the logout is refused because work is not sent", async () => {
    writeLastUserId(userId);
    mocks.flushBeforeLogout.mockRejectedValue(new Error("pending"));
    await expect(clearLocalData(userId)).rejects.toThrow("pending");
    expect(readLastUserId()).toBe(userId);
    expect(mocks.clearLocalUserData).not.toHaveBeenCalled();
  });
});
