import type { PitboxSecurePlugin, SecurityLevel } from "pitbox-secure";

import type { DeviceKeys, DeviceSigner } from "./device-key";
import { derSignatureToP1363 } from "./ecdsa-der";

/** The only plugin methods this class may call: there is no way to ask for a private key. */
export type DeviceKeyPlugin = Pick<
  PitboxSecurePlugin,
  "ensureDeviceKey" | "signWithDeviceKey" | "deleteDeviceKey"
>;

/**
 * Header of a SubjectPublicKeyInfo for an uncompressed NIST P-256 key: SEQUENCE, AlgorithmIdentifier
 * (id-ecPublicKey, prime256v1), BIT STRING, uncompressed-point marker 0x04: 27 bytes, then 64 of X||Y.
 */
const SPKI_P256_HEADER = Uint8Array.from([
  0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a,
  0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00, 0x04,
]);
const SPKI_BYTES = 91;

export class DeviceKeyUnavailableError extends Error {
  constructor(message = "device key unavailable", options?: ErrorOptions) {
    super(message, options);
    this.name = "DeviceKeyUnavailableError";
  }
}

export interface NativeDeviceKeysOptions {
  /**
   * Accept a Keystore that only offers software-backed keys (some emulators). Off by default:
   * a device key that is not in secure hardware is not what TM-SYNC-01 relies on, so the engine
   * would rather not sync than sync with a weaker key.
   */
  allowSoftwareKeys?: boolean;
}

const HARDWARE: ReadonlySet<SecurityLevel> = new Set(["strongbox", "tee"]);

function fromBase64(text: string): Uint8Array {
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(text) || text.length % 4 !== 0) {
    throw new DeviceKeyUnavailableError("malformed base64 from the plugin");
  }
  return Uint8Array.from(atob(text), (char) => char.charCodeAt(0));
}

/** Chunked: spreading a 256 KiB message into String.fromCharCode would overflow the stack. */
function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x2000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x2000));
  }
  return btoa(binary);
}

/**
 * The Android Keystore (ADR 0033): an ECDSA P-256 key per user of the phone that is generated in
 * the Keystore and never leaves it. This class only ever holds the PUBLIC key and a way to ask the
 * Keystore to sign. Everything that comes back from the plugin is checked, because the plugin is
 * native code reached over a bridge: the public key must really be a P-256 SPKI, and the
 * signature must really be canonical DER (`derSignatureToP1363` rejects anything else).
 */
export class NativeDeviceKeys implements DeviceKeys {
  readonly platform = "android";
  readonly deviceName = "Android";

  constructor(
    private readonly plugin: DeviceKeyPlugin,
    private readonly options: NativeDeviceKeysOptions = {},
  ) {}

  async loadOrCreate(userHash: string): Promise<DeviceSigner> {
    let publicKey: string;
    let securityLevel: SecurityLevel;
    try {
      ({ publicKey, securityLevel } = await this.plugin.ensureDeviceKey({ userHash }));
    } catch (error) {
      throw new DeviceKeyUnavailableError(undefined, { cause: error });
    }
    if (!this.options.allowSoftwareKeys && !HARDWARE.has(securityLevel)) {
      throw new DeviceKeyUnavailableError("device key is not hardware-backed");
    }
    const spki = fromBase64(publicKey);
    if (spki.length !== SPKI_BYTES || !SPKI_P256_HEADER.every((byte, i) => spki[i] === byte)) {
      throw new DeviceKeyUnavailableError("public key is not a P-256 SPKI");
    }

    return {
      publicKey: () => Promise.resolve(spki.slice()),
      sign: async (message) => {
        let signature: string;
        try {
          ({ signature } = await this.plugin.signWithDeviceKey({
            userHash,
            message: toBase64(message),
          }));
        } catch (error) {
          throw new DeviceKeyUnavailableError(undefined, { cause: error });
        }
        return derSignatureToP1363(fromBase64(signature));
      },
    };
  }

  async forget(userHash: string): Promise<void> {
    try {
      await this.plugin.deleteDeviceKey({ userHash });
    } catch (error) {
      throw new DeviceKeyUnavailableError(undefined, { cause: error });
    }
  }
}
