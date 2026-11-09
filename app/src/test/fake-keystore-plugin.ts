import { generateKeyPairSync, sign, type KeyObject } from "node:crypto";

import type { SecurityLevel } from "pitbox-secure";

import type { DeviceKeyPlugin } from "@/sync/device-key-native";

const USER_HASH = /^[0-9a-f]{64}$/;

/** What the plugin rejects with: a Capacitor error carries a `code`. */
export class FakePluginError extends Error {
  constructor(readonly code: string) {
    super("fake plugin error");
  }
}

/**
 * Stands in for the Java plugin in tests. Like the real Keystore it keeps one key pair per user
 * hash, never gives the private key out, and signs in ASN.1 DER (not P1363). It validates the
 * hash the same way `DeviceKeyStore.aliasFor` does. The `tamper*` hooks let a test play a plugin
 * that misbehaves.
 */
export class FakeKeystorePlugin implements DeviceKeyPlugin {
  securityLevel: SecurityLevel = "tee";
  tamperPublicKey: ((base64: string) => string) | undefined;
  tamperSignature: ((base64: string) => string) | undefined;
  failWith: Error | undefined;
  readonly calls: string[] = [];
  private readonly pairs = new Map<string, { privateKey: KeyObject; publicKey: KeyObject }>();

  hasKey(userHash: string): boolean {
    return this.pairs.has(userHash);
  }

  private check(userHash: string): void {
    if (this.failWith) throw this.failWith;
    if (!USER_HASH.test(userHash)) throw new FakePluginError("INVALID_INPUT");
  }

  // Each method throws inside `then`, so a refusal becomes a rejected promise like the bridge's.
  ensureDeviceKey({ userHash }: { userHash: string }): Promise<{ publicKey: string; securityLevel: SecurityLevel }> {
    return Promise.resolve().then(() => {
      this.calls.push(`ensure:${userHash.slice(0, 4)}`);
      this.check(userHash);
      let pair = this.pairs.get(userHash);
      if (!pair) {
        pair = generateKeyPairSync("ec", { namedCurve: "P-256" });
        this.pairs.set(userHash, pair);
      }
      const spki = pair.publicKey.export({ type: "spki", format: "der" }).toString("base64");
      return {
        publicKey: this.tamperPublicKey ? this.tamperPublicKey(spki) : spki,
        securityLevel: this.securityLevel,
      };
    });
  }

  signWithDeviceKey({ userHash, message }: { userHash: string; message: string }): Promise<{ signature: string }> {
    return Promise.resolve().then(() => {
      this.calls.push(`sign:${userHash.slice(0, 4)}`);
      this.check(userHash);
      const pair = this.pairs.get(userHash);
      if (!pair) throw new FakePluginError("KEYSTORE_UNAVAILABLE");
      const der = sign("sha256", Buffer.from(message, "base64"), { key: pair.privateKey, dsaEncoding: "der" });
      const signature = der.toString("base64");
      return { signature: this.tamperSignature ? this.tamperSignature(signature) : signature };
    });
  }

  deleteDeviceKey({ userHash }: { userHash: string }): Promise<void> {
    return Promise.resolve().then(() => {
      this.calls.push(`delete:${userHash.slice(0, 4)}`);
      this.check(userHash);
      this.pairs.delete(userHash);
    });
  }
}
