// The device key (ECDSA P-256) signs every operation and proves possession at registration.
// It is generated non-extractable: script code (and an XSS payload) can ask it to sign while
// the page is open, but can never read the private key out. On native (F2-06) the key moves
// to the Keychain/Keystore; the KeyStore interface is where that plugs in.

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
