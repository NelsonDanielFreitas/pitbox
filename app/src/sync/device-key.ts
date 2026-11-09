// The device key (ECDSA P-256) signs every operation and proves possession at registration.
// It is generated non-extractable: script code (and an XSS payload) can ask it to sign while
// the page is open, but can never read the private key out.
//
// The sync engine only sees `DeviceKeys` / `DeviceSigner` below: "give me the public key" and
// "sign these bytes". It never gets a key object, so the same engine works with a WebCrypto key
// in the browser (this file) and a hardware-backed Android Keystore key (device-key-native.ts,
// ADR 0033), where there is no key object to hand out at all.

export type DevicePlatform = "web" | "android";

/** Signs on behalf of one user of this device. The private key is not reachable from here. */
export interface DeviceSigner {
  /** SPKI DER (91 bytes for P-256): what RegisterDevice expects. */
  publicKey(): Promise<Uint8Array>;
  /** ECDSA P-256 / SHA-256 as IEEE P1363 (r||s, 64 bytes), the format of ADR 0025. */
  sign(message: Uint8Array): Promise<Uint8Array>;
}

export interface DeviceKeys {
  readonly platform: DevicePlatform;
  /** Shown to the owner in the device list. */
  readonly deviceName: string;
  /** The signer of this user on this device; creates the key the first time. */
  loadOrCreate(userHash: string): Promise<DeviceSigner>;
  /** Drops the key of this user (revoked device, new registration). Missing is not an error. */
  forget(userHash: string): Promise<void>;
}

export interface KeyStore {
  get(name: string): Promise<CryptoKeyPair | undefined>;
  put(name: string, pair: CryptoKeyPair): Promise<void>;
  delete(name: string): Promise<void>;
}

const dbName = "pitbox-device-keys";
const storeName = "keys";

function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error("indexeddb request failed"));
  });
}

export class IndexedDbKeyStore implements KeyStore {
  private opened: Promise<IDBDatabase> | undefined;

  private open(): Promise<IDBDatabase> {
    this.opened ??= new Promise((resolve, reject) => {
      const req = indexedDB.open(dbName, 1);
      req.onupgradeneeded = () => req.result.createObjectStore(storeName);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error ?? new Error("indexeddb open failed"));
    });
    return this.opened;
  }

  private async store(mode: IDBTransactionMode): Promise<IDBObjectStore> {
    return (await this.open()).transaction(storeName, mode).objectStore(storeName);
  }

  async get(name: string): Promise<CryptoKeyPair | undefined> {
    const value: unknown = await request((await this.store("readonly")).get(name));
    return isKeyPair(value) ? value : undefined;
  }

  async put(name: string, pair: CryptoKeyPair): Promise<void> {
    await request((await this.store("readwrite")).put(pair, name));
  }

  async delete(name: string): Promise<void> {
    await request((await this.store("readwrite")).delete(name));
  }
}

export class MemoryKeyStore implements KeyStore {
  private readonly keys = new Map<string, CryptoKeyPair>();
  get(name: string): Promise<CryptoKeyPair | undefined> {
    return Promise.resolve(this.keys.get(name));
  }
  put(name: string, pair: CryptoKeyPair): Promise<void> {
    this.keys.set(name, pair);
    return Promise.resolve();
  }
  delete(name: string): Promise<void> {
    this.keys.delete(name);
    return Promise.resolve();
  }
}

function isKeyPair(value: unknown): value is CryptoKeyPair {
  return (
    typeof value === "object" &&
    value !== null &&
    "privateKey" in value &&
    "publicKey" in value &&
    (value.privateKey as CryptoKey).type === "private" &&
    (value.publicKey as CryptoKey).type === "public"
  );
}

const algorithm = { name: "ECDSA", namedCurve: "P-256" } as const;

/** Returns the stored key of this user on this device, creating it on first use. */
export async function loadOrCreateDeviceKey(
  store: KeyStore,
  userHash: string,
): Promise<CryptoKeyPair> {
  const name = `device:${userHash}`;
  const existing = await store.get(name);
  if (existing) return existing;
  const pair = await crypto.subtle.generateKey(algorithm, false, ["sign", "verify"]);
  await store.put(name, pair);
  return pair;
}

export async function forgetDeviceKey(store: KeyStore, userHash: string): Promise<void> {
  await store.delete(`device:${userHash}`);
}

/** SPKI DER (91 bytes for P-256): what RegisterDevice expects. */
export async function exportPublicKey(pair: CryptoKeyPair): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.exportKey("spki", pair.publicKey));
}

/** ECDSA P-256 / SHA-256; WebCrypto already returns IEEE P1363 (r||s, 64 bytes). */
export async function signBytes(pair: CryptoKeyPair, message: Uint8Array): Promise<Uint8Array> {
  const signature = new Uint8Array(
    await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, pair.privateKey, message as BufferSource),
  );
  if (signature.length !== 64) throw new Error("unexpected signature length");
  return signature;
}

/** The browser: a non-extractable WebCrypto key pair kept in a `KeyStore` (IndexedDB). */
export class WebDeviceKeys implements DeviceKeys {
  readonly platform = "web";
  readonly deviceName = "Browser";

  constructor(private readonly store: KeyStore) {}

  async loadOrCreate(userHash: string): Promise<DeviceSigner> {
    const pair = await loadOrCreateDeviceKey(this.store, userHash);
    return {
      publicKey: () => exportPublicKey(pair),
      sign: (message) => signBytes(pair, message),
    };
  }

  forget(userHash: string): Promise<void> {
    return forgetDeviceKey(this.store, userHash);
  }
}
