// @vitest-environment node

import { describe, expect, it } from "vitest";

import { FakeKeystorePlugin, FakePluginError } from "@/test/fake-keystore-plugin";

import { WebDeviceKeys, MemoryKeyStore } from "./device-key";
import { DeviceKeyUnavailableError, NativeDeviceKeys } from "./device-key-native";
import { InvalidDerSignatureError } from "./ecdsa-der";

const ALICE = "a".repeat(64);
const BOB = "b".repeat(64);

const p256 = { name: "ECDSA", namedCurve: "P-256" } as const;

async function verifies(spki: Uint8Array, message: Uint8Array, signature: Uint8Array): Promise<boolean> {
  const key = await crypto.subtle.importKey("spki", spki as BufferSource, p256, false, ["verify"]);
  return crypto.subtle.verify(
    { name: "ECDSA", hash: "SHA-256" },
    key,
    signature as BufferSource,
    message as BufferSource,
  );
}

describe("NativeDeviceKeys", () => {
  it("returns a P-256 public key and signatures the server's verifier accepts", async () => {
    const keys = new NativeDeviceKeys(new FakeKeystorePlugin());
    const signer = await keys.loadOrCreate(ALICE);
    const spki = await signer.publicKey();
    expect(spki).toHaveLength(91);

    const message = new TextEncoder().encode("operation bytes");
    const signature = await signer.sign(message);
    expect(signature).toHaveLength(64); // P1363, not DER
    expect(await verifies(spki, message, signature)).toBe(true);
    expect(await verifies(spki, new TextEncoder().encode("other"), signature)).toBe(false);
  });

  it("signs a message at the plugin's 256 KiB limit without overflowing the stack", async () => {
    const signer = await new NativeDeviceKeys(new FakeKeystorePlugin()).loadOrCreate(ALICE);
    const message = new Uint8Array(256 * 1024).fill(7);
    expect(await verifies(await signer.publicKey(), message, await signer.sign(message))).toBe(true);
  });

  it("keeps the same key between calls and a different key per user", async () => {
    const plugin = new FakeKeystorePlugin();
    const keys = new NativeDeviceKeys(plugin);
    const first = await (await keys.loadOrCreate(ALICE)).publicKey();
    const again = await (await keys.loadOrCreate(ALICE)).publicKey();
    const other = await (await keys.loadOrCreate(BOB)).publicKey();
    expect(again).toEqual(first);
    expect(other).not.toEqual(first);
  });

  it("forgets only the key of the user asked, and a new one is created afterwards", async () => {
    const plugin = new FakeKeystorePlugin();
    const keys = new NativeDeviceKeys(plugin);
    const aliceBefore = await (await keys.loadOrCreate(ALICE)).publicKey();
    await keys.loadOrCreate(BOB);

    await keys.forget(ALICE);
    expect(plugin.hasKey(ALICE)).toBe(false);
    expect(plugin.hasKey(BOB)).toBe(true);
    await keys.forget(ALICE); // missing is not an error

    const aliceAfter = await (await keys.loadOrCreate(ALICE)).publicKey();
    expect(aliceAfter).not.toEqual(aliceBefore);
  });

  it("identifies itself as an Android device", () => {
    const keys = new NativeDeviceKeys(new FakeKeystorePlugin());
    expect([keys.platform, keys.deviceName]).toEqual(["android", "Android"]);
    const web = new WebDeviceKeys(new MemoryKeyStore());
    expect([web.platform, web.deviceName]).toEqual(["web", "Browser"]);
  });

  describe("hardware", () => {
    it.each(["software", "unknown"] as const)("refuses a %s key unless allowed", async (level) => {
      const plugin = new FakeKeystorePlugin();
      plugin.securityLevel = level;
      await expect(new NativeDeviceKeys(plugin).loadOrCreate(ALICE)).rejects.toBeInstanceOf(
        DeviceKeyUnavailableError,
      );
      await expect(
        new NativeDeviceKeys(plugin, { allowSoftwareKeys: true }).loadOrCreate(ALICE),
      ).resolves.toBeDefined();
    });

    it.each(["strongbox", "tee"] as const)("accepts a %s key", async (level) => {
      const plugin = new FakeKeystorePlugin();
      plugin.securityLevel = level;
      await expect(new NativeDeviceKeys(plugin).loadOrCreate(ALICE)).resolves.toBeDefined();
    });
  });

  describe("does not trust the bridge", () => {
    const load = (plugin: FakeKeystorePlugin) => new NativeDeviceKeys(plugin).loadOrCreate(ALICE);

    it("rejects a public key that is not a P-256 SPKI", async () => {
      const plugin = new FakeKeystorePlugin();
      for (const tamper of [
        (b64: string) => b64.slice(0, 40), // too short
        () => Buffer.alloc(91, 1).toString("base64"), // right size, wrong content
        (b64: string) => {
          const bytes = Buffer.from(b64, "base64");
          bytes[26] = 0x02; // compressed-point marker instead of 0x04
          return bytes.toString("base64");
        },
        () => "not base64 !!!",
        () => "",
      ]) {
        plugin.tamperPublicKey = tamper;
        await expect(load(plugin)).rejects.toBeInstanceOf(DeviceKeyUnavailableError);
      }
    });

    it("rejects a signature that is not canonical DER", async () => {
      const plugin = new FakeKeystorePlugin();
      const signer = await load(plugin);
      const message = new Uint8Array([1, 2, 3]);
      for (const tamper of [
        (b64: string) => Buffer.concat([Buffer.from(b64, "base64"), Buffer.from([0])]).toString("base64"),
        () => Buffer.alloc(64, 1).toString("base64"), // a raw P1363 where DER is expected
        () => "",
      ]) {
        plugin.tamperSignature = tamper;
        await expect(signer.sign(message)).rejects.toBeInstanceOf(InvalidDerSignatureError);
      }
    });

    it("wraps plugin failures in one generic error that keeps the cause", async () => {
      const plugin = new FakeKeystorePlugin();
      const signer = await load(plugin);
      plugin.failWith = new FakePluginError("KEYSTORE_UNAVAILABLE");

      const errors = await Promise.all([
        load(plugin).catch((e: unknown) => e),
        signer.sign(new Uint8Array([1])).catch((e: unknown) => e),
        new NativeDeviceKeys(plugin).forget(ALICE).catch((e: unknown) => e),
      ]);
      for (const error of errors) {
        expect(error).toBeInstanceOf(DeviceKeyUnavailableError);
        expect((error as Error).message).toBe("device key unavailable");
        expect((error as Error).cause).toBe(plugin.failWith);
      }
    });

    it("passes the user hash through unchanged and lets the plugin refuse a bad one", async () => {
      const plugin = new FakeKeystorePlugin();
      await expect(new NativeDeviceKeys(plugin).loadOrCreate("../../etc")).rejects.toBeInstanceOf(
        DeviceKeyUnavailableError,
      );
    });
  });
});
