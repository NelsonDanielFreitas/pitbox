import { PitboxSecure } from "pitbox-secure";
import { describe, expect, it } from "vitest";

const HASH = "a".repeat(64);

describe("pitbox-secure outside the Capacitor shell", () => {
  // The web keeps its own WebCrypto key (ADR 0025). If the native plugin were ever silently
  // "available" in a browser, the app could believe it has a Keystore key when it has none.
  it("fails closed: every call rejects instead of pretending to work", async () => {
    await expect(PitboxSecure.ensureDeviceKey({ userHash: HASH })).rejects.toBeDefined();
    await expect(PitboxSecure.signWithDeviceKey({ userHash: HASH, message: "AA==" })).rejects.toBeDefined();
    await expect(PitboxSecure.deleteDeviceKey({ userHash: HASH })).rejects.toBeDefined();
    await expect(PitboxSecure.authenticate({ title: "PitBox" })).rejects.toBeDefined();
  });
});
