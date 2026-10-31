import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const db = vi.hoisted(() => ({
  closeDatabase: vi.fn(),
  deleteUserDatabase: vi.fn(),
  localDatabaseExists: vi.fn(),
  openDatabase: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/db/client", () => db);

import { writeLastUserId } from "./last-user";
import { hasValidOfflinePass, isPassValid, resolveOfflineSession } from "./offline-session";

const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const now = new Date("2026-10-31T12:00:00Z");
const pass = (expiresAt: string) => JSON.stringify({ token: "t", expiresAt });
const future = pass("2026-11-02T12:00:00Z");
const past = pass("2026-10-31T11:59:59Z");

describe("isPassValid", () => {
  it("accepts a pass that has not expired", () => {
    expect(isPassValid(future, now)).toBe(true);
  });

  it.each([
    ["missing", null],
    ["empty", ""],
    ["expired", past],
    ["expiring right now", pass("2026-10-31T12:00:00Z")],
    ["not json", "{"],
    ["no expiry", JSON.stringify({ token: "t" })],
    ["no token", JSON.stringify({ expiresAt: "2026-11-02T12:00:00Z" })],
    ["a date that is not a date", pass("tomorrow")],
    ["expiry of the wrong type", JSON.stringify({ token: "t", expiresAt: 4102444800000 })],
  ])("rejects %s", (_name, raw) => {
    expect(isPassValid(raw, now)).toBe(false);
  });
});

describe("hasValidOfflinePass", () => {
  afterEach(() => vi.resetAllMocks());

  it("is true when any team has a valid pass", async () => {
    db.query.mockResolvedValue([{ value: past }, { value: future }]);
    await expect(hasValidOfflinePass(now)).resolves.toBe(true);
  });

  it("is false when every pass is expired or there are none", async () => {
    db.query.mockResolvedValue([{ value: past }]);
    await expect(hasValidOfflinePass(now)).resolves.toBe(false);
    db.query.mockResolvedValue([]);
    await expect(hasValidOfflinePass(now)).resolves.toBe(false);
  });
});

describe("resolveOfflineSession", () => {
  beforeEach(() => {
    db.localDatabaseExists.mockResolvedValue(true);
    db.openDatabase.mockResolvedValue(undefined);
    db.closeDatabase.mockResolvedValue(undefined);
  });
  afterEach(() => {
    vi.resetAllMocks();
    window.localStorage.clear();
  });

  it("opens the last user's database when a pass is valid", async () => {
    writeLastUserId(userId);
    db.query.mockResolvedValue([{ value: future }]);
    await expect(resolveOfflineSession(now)).resolves.toEqual({
      authenticated: true,
      offline: true,
      user_id: userId,
      amr: [],
    });
    expect(db.openDatabase).toHaveBeenCalledWith(userId);
    expect(db.closeDatabase).not.toHaveBeenCalled();
  });

  it("shows nothing and closes the database when there is no valid pass, without deleting it", async () => {
    writeLastUserId(userId);
    db.query.mockResolvedValue([{ value: past }]);
    await expect(resolveOfflineSession(now)).resolves.toBeNull();
    expect(db.closeDatabase).toHaveBeenCalled();
    expect(db.deleteUserDatabase).not.toHaveBeenCalled();
  });

  it("does nothing without a last user", async () => {
    await expect(resolveOfflineSession(now)).resolves.toBeNull();
    expect(db.openDatabase).not.toHaveBeenCalled();
  });

  it("does not open (or create) a database for a user that has none", async () => {
    writeLastUserId(userId);
    db.localDatabaseExists.mockResolvedValue(false);
    await expect(resolveOfflineSession(now)).resolves.toBeNull();
    expect(db.openDatabase).not.toHaveBeenCalled();
  });

  it("shows nothing when the database cannot be read", async () => {
    writeLastUserId(userId);
    db.query.mockRejectedValue(new Error("corrupt"));
    await expect(resolveOfflineSession(now)).resolves.toBeNull();
    expect(db.closeDatabase).toHaveBeenCalled();
  });
});
