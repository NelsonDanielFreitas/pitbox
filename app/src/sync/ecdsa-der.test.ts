import { generateKeyPairSync, sign, verify } from "node:crypto";

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  derSignatureToP1363,
  InvalidDerSignatureError,
  P1363_SIGNATURE_BYTES,
} from "./ecdsa-der";

/** Canonical DER encoding of one non-negative integer, as a Keystore would produce it. */
function derInteger(value: bigint): number[] {
  let hex = value.toString(16);
  if (hex.length % 2 === 1) hex = `0${hex}`;
  const bytes = hex.match(/../g)?.map((pair) => parseInt(pair, 16)) ?? [0];
  if ((bytes[0] ?? 0) >= 0x80) bytes.unshift(0);
  return [0x02, bytes.length, ...bytes];
}

function derSignature(r: bigint, s: bigint): Uint8Array {
  const body = [...derInteger(r), ...derInteger(s)];
  return Uint8Array.from([0x30, body.length, ...body]);
}

function p1363(r: bigint, s: bigint): Uint8Array {
  const out = new Uint8Array(P1363_SIGNATURE_BYTES);
  const hex = (v: bigint) => v.toString(16).padStart(64, "0");
  const bytes = (hex(r) + hex(s)).match(/../g) ?? [];
  bytes.forEach((byte, i) => (out[i] = parseInt(byte, 16)));
  return out;
}

const bigint256 = fc
  .uint8Array({ minLength: 32, maxLength: 32 })
  .map((b) => b.reduce((acc, byte) => (acc << 8n) | BigInt(byte), 0n))
  .filter((v) => v > 0n);

describe("derSignatureToP1363", () => {
  it("converts real P-256 signatures that the P1363 verifier accepts", () => {
    // Signatures from a real implementation (Node/OpenSSL). With 300 draws the leading-zero and
    // short-integer cases (about 1 in 128 per integer) are hit as well.
    const message = Buffer.from("pitbox operation bytes");
    for (let i = 0; i < 300; i++) {
      const { privateKey, publicKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });
      const der = sign("sha256", message, { key: privateKey, dsaEncoding: "der" });
      const converted = derSignatureToP1363(new Uint8Array(der));
      expect(converted).toHaveLength(64);
      expect(
        verify("sha256", message, { key: publicKey, dsaEncoding: "ieee-p1363" }, converted),
      ).toBe(true);
    }
  });

  it("matches the reference encoding for any r and s (property)", () => {
    fc.assert(
      fc.property(bigint256, bigint256, (r, s) => {
        expect(derSignatureToP1363(derSignature(r, s))).toEqual(p1363(r, s));
      }),
      { numRuns: 5000 },
    );
  });

  it("pads short integers on the left and drops the sign byte of long ones", () => {
    expect(derSignatureToP1363(derSignature(1n, 2n))).toEqual(p1363(1n, 2n));
    const high = (1n << 255n) | 5n; // high bit set: DER adds 0x00, so 33 bytes
    const der = derSignature(high, high);
    expect(der).toHaveLength(2 + 2 * (2 + 33));
    expect(derSignatureToP1363(der)).toEqual(p1363(high, high));
    const max = (1n << 256n) - 1n;
    expect(derSignatureToP1363(derSignature(max, max))).toEqual(p1363(max, max));
  });

  const valid = Array.from(derSignature(0x0102n, 0x0304n));
  const rejected: [string, number[]][] = [
    ["empty", []],
    ["too short", [0x30, 0x00]],
    ["not a SEQUENCE", [0x31, ...valid.slice(1)]],
    ["SEQUENCE length too big", [0x30, valid.length, ...valid.slice(2)]],
    ["SEQUENCE length too small", [0x30, valid.length - 3, ...valid.slice(2)]],
    ["trailing byte", [0x30, valid.length - 1, ...valid.slice(2), 0x00]],
    ["long-form SEQUENCE length", [0x30, 0x81, valid.length - 2, ...valid.slice(2)]],
    ["first item not INTEGER", [0x30, 0x06, 0x04, 0x02, 0x01, 0x02, 0x02, 0x00]],
    ["INTEGER length 0", [0x30, 0x04, 0x02, 0x00, 0x02, 0x00]],
    ["INTEGER runs past the end", [0x30, 0x07, 0x02, 0x05, 0x01, 0x02, 0x02, 0x01, 0x01]],
    ["negative r", [0x30, 0x06, 0x02, 0x01, 0x80, 0x02, 0x01, 0x01]],
    ["negative s", [0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0xff]],
    ["zero r", [0x30, 0x06, 0x02, 0x01, 0x00, 0x02, 0x01, 0x01]],
    ["zero s", [0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x00]],
    ["non-minimal leading zero in r", [0x30, 0x07, 0x02, 0x02, 0x00, 0x01, 0x02, 0x01, 0x01]],
    ["non-minimal leading zero in s", [0x30, 0x07, 0x02, 0x01, 0x01, 0x02, 0x02, 0x00, 0x7f]],
  ];
  it.each(rejected)("rejects %s", (_name, bytes) => {
    expect(() => derSignatureToP1363(Uint8Array.from(bytes))).toThrow(InvalidDerSignatureError);
  });

  it("rejects an integer with more than 32 significant bytes", () => {
    const body = [0x02, 0x22, 0x01, ...new Array<number>(33).fill(0x11), 0x02, 0x01, 0x01];
    expect(() => derSignatureToP1363(Uint8Array.from([0x30, body.length, ...body]))).toThrow(
      InvalidDerSignatureError,
    );
  });

  it("rejects a signature over the 72-byte maximum", () => {
    expect(() => derSignatureToP1363(new Uint8Array(73).fill(0x30))).toThrow(
      InvalidDerSignatureError,
    );
  });

  it("never throws anything but InvalidDerSignatureError on arbitrary bytes (property)", () => {
    fc.assert(
      fc.property(fc.uint8Array({ maxLength: 100 }), (bytes) => {
        try {
          expect(derSignatureToP1363(bytes)).toHaveLength(64);
        } catch (error) {
          expect(error).toBeInstanceOf(InvalidDerSignatureError);
        }
      }),
      { numRuns: 20000 },
    );
  });

  it("uses one constant message, so nothing about the input leaks", () => {
    const messages = new Set<string>();
    for (const [, bytes] of rejected) {
      try {
        derSignatureToP1363(Uint8Array.from(bytes));
      } catch (error) {
        messages.add(error instanceof Error ? error.message : "not an Error");
      }
    }
    expect([...messages]).toEqual(["invalid ECDSA DER signature"]);
  });
});
