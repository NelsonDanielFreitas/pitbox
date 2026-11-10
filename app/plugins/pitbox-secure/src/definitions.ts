/**
 * Where the private key lives. "software" means the Keystore fell back to software keys (some
 * emulators): the key is still not exportable through this API, but it is not hardware-backed.
 */
export type SecurityLevel = "strongbox" | "tee" | "software" | "unknown";

/** Stable error codes. Messages are generic on purpose: no key material, no internals. */
export type PitboxSecureErrorCode =
  | "KEYSTORE_UNAVAILABLE" // the Keystore failed or the key is gone
  | "INVALID_INPUT" // missing, malformed or oversized parameter
  | "BIOMETRIC_UNAVAILABLE" // no biometric and no device credential enrolled
  | "BIOMETRIC_CANCELLED" // the user dismissed the prompt
  | "BIOMETRIC_LOCKED_OUT" // too many attempts
  | "BIOMETRIC_FAILED" // any other prompt failure
  | "AUTH_TAB_UNAVAILABLE" // the system browser has no Auth Tab: login refuses (no fallback)
  | "LOGIN_CANCELLED" // the user closed the login tab
  | "LOGIN_FAILED"; // the browser returned something that is not our redirect

/**
 * Every key call names its user with `userHash` (64 lowercase hex characters, the SHA-256 of the
 * user id that the app already uses for the local database): one key per user of the phone, so a
 * logout or a revocation of one user never touches another's key. Anything else is refused with
 * `INVALID_INPUT`.
 */
export interface PitboxSecurePlugin {
  /**
   * Creates the P-256 signing key in the Android Keystore if it does not exist yet, and returns
   * its public half. Idempotent. The private key never leaves the Keystore.
   *
   * @returns `publicKey`: base64 of the SubjectPublicKeyInfo DER (91 bytes), the format the server
   * registers (ADR 0025).
   */
  ensureDeviceKey(options: {
    userHash: string;
  }): Promise<{ publicKey: string; securityLevel: SecurityLevel }>;

  /**
   * ECDSA-SHA256 with the device key.
   *
   * @param options.message base64 of the bytes to sign, 1 byte to 256 KiB.
   * @returns `signature`: base64 of the signature **in ASN.1 DER**, as the Keystore produces it.
   * Convert it to IEEE P1363 (r‖s, 64 bytes) with `derSignatureToP1363` before sending it.
   */
  signWithDeviceKey(options: {
    userHash: string;
    message: string;
  }): Promise<{ signature: string }>;

  /** Deletes the device key. Resolves also when there was none. */
  deleteDeviceKey(options: { userHash: string }): Promise<void>;

  /**
   * Shows the system biometric prompt (or the device credential as fallback). Resolves only after
   * a successful authentication; every other outcome rejects with a `PitboxSecureErrorCode`.
   * This is a UI lock. It does not unlock or derive any key (ADR 0033 §4).
   */
  authenticate(options: { title: string; subtitle?: string }): Promise<void>;

  /**
   * Creates the key of one login session (ADR 0032), distinct from the sync key. The plugin
   * chooses the random 128-bit id, so the alias can never be steered by the caller.
   *
   * @returns `keyId` (32 lowercase hex) and `publicKey` (base64 SPKI DER, 91 bytes).
   */
  createSessionKey(): Promise<{ keyId: string; publicKey: string; securityLevel: SecurityLevel }>;

  /** ECDSA-SHA256 with a session key; `message` is base64, 1 to 1024 bytes; DER signature back. */
  signWithSessionKey(options: { keyId: string; message: string }): Promise<{ signature: string }>;

  /** Deletes a session key. Resolves also when there was none. */
  deleteSessionKey(options: { keyId: string }): Promise<void>;

  /**
   * Opens `url` in the system browser's Auth Tab and resolves with the authorization `code` and
   * `state` of the redirect. The plugin only opens an https URL that names our own redirect URI
   * and only accepts a redirect of exactly that URI with exactly `code` and `state`. Without Auth
   * Tab support it rejects with `AUTH_TAB_UNAVAILABLE`.
   */
  loginWithBrowser(options: { url: string }): Promise<{ code: string; state: string }>;
}
