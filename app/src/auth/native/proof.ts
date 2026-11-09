import type { PitboxSecurePlugin } from "pitbox-secure";

import { derSignatureToP1363 } from "@/sync/ecdsa-der";

/** The only plugin methods the login uses; there is no way to ask for a private key. */
export type SessionKeyPlugin = Pick<
  PitboxSecurePlugin,
  "createSessionKey" | "signWithSessionKey" | "deleteSessionKey" | "loginWithBrowser"
>;

/** Domain tags (ADR 0032): a signature made for one purpose is never valid for the other. */
export const PROOF_TOKEN = "pitbox-mobile-v1/token";
export const PROOF_REFRESH = "pitbox-mobile-v1/refresh";

const encoder = new TextEncoder();

/** The signed bytes: the domain tag, then every field, each with a 4-byte big-endian length. */
export function proofMessage(domain: string, fields: readonly string[]): Uint8Array {
  const parts = [domain, ...fields].map((text) => encoder.encode(text));
  const out = new Uint8Array(parts.reduce((n, part) => n + 4 + part.length, 0));
  const view = new DataView(out.buffer);
  let offset = 0;
  for (const part of parts) {
    view.setUint32(offset, part.length);
    out.set(part, offset + 4);
    offset += 4 + part.length;
  }
  return out;
}

export function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x2000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x2000));
  }
  return btoa(binary);
}

export function toBase64Url(bytes: Uint8Array): string {
  return toBase64(bytes).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function fromBase64(text: string): Uint8Array {
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(text) || text.length % 4 !== 0) {
    throw new Error("malformed base64 from the plugin");
  }
  return Uint8Array.from(atob(text), (char) => char.charCodeAt(0));
}

/**
 * Signs a proof with the session key in the Keystore and returns it as base64url r||s, the form
 * the server verifies. The Keystore's DER signature is converted by the strict parser.
 */
export async function signProof(
  plugin: SessionKeyPlugin,
  keyId: string,
  domain: string,
  fields: readonly string[],
): Promise<string> {
  const { signature } = await plugin.signWithSessionKey({
    keyId,
    message: toBase64(proofMessage(domain, fields)),
  });
  return toBase64Url(derSignatureToP1363(fromBase64(signature)));
}

/** PKCE (RFC 7636, S256): a random verifier and its challenge. */
export async function newPkce(): Promise<{ verifier: string; challenge: string }> {
  const verifier = toBase64Url(crypto.getRandomValues(new Uint8Array(32)));
  const digest = await crypto.subtle.digest("SHA-256", encoder.encode(verifier));
  return { verifier, challenge: toBase64Url(new Uint8Array(digest)) };
}
