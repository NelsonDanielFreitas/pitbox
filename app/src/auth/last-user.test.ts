import { afterEach, describe, expect, it } from "vitest";

import { clearLastUserId, lastUserKey, readLastUserId, writeLastUserId } from "./last-user";

const userId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";

describe("last user", () => {
  afterEach(() => window.localStorage.clear());

  it("stores only the user id", () => {
    writeLastUserId(userId);
    expect(window.localStorage.getItem(lastUserKey)).toBe(JSON.stringify({ user_id: userId }));
    expect(readLastUserId()).toBe(userId);
  });

  it("returns null when nothing was stored", () => {
    expect(readLastUserId()).toBeNull();
  });

  it.each([
    ["not json", "{nope"],
    ["not a uuid", JSON.stringify({ user_id: "../../etc" })],
    ["an extra field (a token, say)", JSON.stringify({ user_id: userId, token: "x" })],
    ["a number", JSON.stringify({ user_id: 7 })],
    ["a bare string", JSON.stringify(userId)],
  ])("rejects a tampered value (%s) and drops it", (_name, raw) => {
    window.localStorage.setItem(lastUserKey, raw);
    expect(readLastUserId()).toBeNull();
    expect(window.localStorage.getItem(lastUserKey)).toBeNull();
  });

  it("clears the value", () => {
    writeLastUserId(userId);
    clearLastUserId();
    expect(readLastUserId()).toBeNull();
  });

  it("survives storage that throws", () => {
    const broken = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
      removeItem: () => {
        throw new Error("blocked");
      },
    } as unknown as Storage;
    expect(readLastUserId(broken)).toBeNull();
    expect(() => writeLastUserId(userId, broken)).not.toThrow();
    expect(() => clearLastUserId(broken)).not.toThrow();
  });
});
