const COORDINATE_BYTES = 32; // P-256
export const P1363_SIGNATURE_BYTES = 2 * COORDINATE_BYTES;

const SEQUENCE = 0x30;
const INTEGER = 0x02;

export class InvalidDerSignatureError extends Error {
  constructor() {
    // Generic on purpose: the input is a signature, never echo it or say where it broke.
    super("invalid ECDSA DER signature");
    this.name = "InvalidDerSignatureError";
  }
}

/**
 * Converts the ASN.1 DER signature the Android Keystore produces (`SEQUENCE { INTEGER r,
 * INTEGER s }`) to the IEEE P1363 form the server expects (r‖s, 32 bytes each, ADR 0025).
 *
 * The parser is strict, canonical DER only, and fails closed: a signature is attacker-adjacent
 * input that ends up in a security protocol, so anything unusual (long-form lengths, trailing
 * bytes, negative or non-minimal integers, zero, more than 32 significant bytes) is rejected
 * instead of being "fixed". It does not check that r and s are below the curve order: the
 * server verifies the signature.
 */
export function derSignatureToP1363(der: Uint8Array): Uint8Array {
  // The longest valid P-256 signature is 72 bytes, so every length fits the short form (< 128).
  if (der.length < 8 || der.length > 72) throw new InvalidDerSignatureError();
  if (der[0] !== SEQUENCE || der[1] !== der.length - 2) throw new InvalidDerSignatureError();

  const out = new Uint8Array(P1363_SIGNATURE_BYTES);
  let offset = 2;
  for (let part = 0; part < 2; part++) {
    offset = readInteger(der, offset, out, part * COORDINATE_BYTES);
  }
  if (offset !== der.length) throw new InvalidDerSignatureError(); // trailing bytes
  return out;
}

/** Reads one INTEGER at `offset`, writes it left-padded into `out[at..at+32]`, returns the end. */
function readInteger(der: Uint8Array, offset: number, out: Uint8Array, at: number): number {
  if (der[offset] !== INTEGER) throw new InvalidDerSignatureError();
  const length = der[offset + 1];
  if (length === undefined || length < 1 || length > COORDINATE_BYTES + 1) {
    throw new InvalidDerSignatureError();
  }
  const start = offset + 2;
  const end = start + length;
  if (end > der.length) throw new InvalidDerSignatureError();

  const first = der[start];
  if (first === undefined || first >= 0x80) throw new InvalidDerSignatureError(); // negative
  if (first === 0x00) {
    // A leading zero is only legal to keep a value with the high bit set positive. This also
    // rejects the value zero (a lone 0x00, or 0x00 followed by a small byte): r, s are in [1, n-1].
    const second = der[start + 1];
    if (second === undefined || second < 0x80) throw new InvalidDerSignatureError();
  }

  const value = first === 0x00 ? der.subarray(start + 1, end) : der.subarray(start, end);
  if (value.length > COORDINATE_BYTES) throw new InvalidDerSignatureError();

  out.set(value, at + COORDINATE_BYTES - value.length);
  return end;
}
