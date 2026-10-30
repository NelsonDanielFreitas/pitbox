// Byte-for-byte mirror of backend/internal/sync/domain/signing.go (ADR 0025). The shared
// test vectors in signing.test.ts fail on either side if one of them drifts.

export const OpKind = { create: 1, patch: 2, append: 3, delete: 4 } as const;
export type OpKindValue = (typeof OpKind)[keyof typeof OpKind];

const opTag = "pitbox-sync-op-v1";
const registrationTag = "pitbox-sync-device-v1";

export interface SigningOp {
  opId: string;
  entity: string;
  entityId: string;
  kind: OpKindValue;
  fieldsJson: Uint8Array;
  baseVersion: number | null;
  clientTsMs: number;
}

export function uuidBytes(id: string): Uint8Array {
  const hex = id.replaceAll("-", "");
  if (!/^[0-9a-f]{32}$/i.test(hex)) throw new Error("invalid uuid");
  const out = new Uint8Array(16);
  for (let i = 0; i < 16; i += 1) out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}

class Writer {
  private readonly chunks: Uint8Array[] = [];

  put(bytes: Uint8Array): void {
    const length = new Uint8Array(4);
    new DataView(length.buffer).setUint32(0, bytes.length, false);
    this.chunks.push(length, bytes);
  }

  putText(text: string): void {
    this.put(new TextEncoder().encode(text));
  }

  // 8 bytes, big endian, two's complement: -1 marks an absent base_version.
  putInt(value: number): void {
    const buffer = new Uint8Array(8);
    new DataView(buffer.buffer).setBigInt64(0, BigInt(value), false);
    this.put(buffer);
  }

  bytes(): Uint8Array {
    const total = this.chunks.reduce((sum, chunk) => sum + chunk.length, 0);
    const out = new Uint8Array(total);
    let offset = 0;
    for (const chunk of this.chunks) {
      out.set(chunk, offset);
      offset += chunk.length;
    }
    return out;
  }
}

async function sha256(bytes: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", bytes as BufferSource));
}

export async function signingMessage(
  teamId: string,
  deviceId: string,
  op: SigningOp,
): Promise<Uint8Array> {
  const w = new Writer();
  w.putText(opTag);
  w.put(uuidBytes(op.opId));
  w.put(uuidBytes(teamId));
  w.put(uuidBytes(deviceId));
  w.putText(op.entity);
  w.put(uuidBytes(op.entityId));
  w.putInt(op.kind);
  w.putInt(op.baseVersion ?? -1);
  w.putInt(op.clientTsMs);
  w.put(await sha256(op.fieldsJson));
  return w.bytes();
}

export function registrationMessage(
  teamId: string,
  userId: string,
  publicKey: Uint8Array,
): Uint8Array {
  const w = new Writer();
  w.putText(registrationTag);
  w.put(uuidBytes(teamId));
  w.put(uuidBytes(userId));
  w.put(publicKey);
  return w.bytes();
}

export function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}
