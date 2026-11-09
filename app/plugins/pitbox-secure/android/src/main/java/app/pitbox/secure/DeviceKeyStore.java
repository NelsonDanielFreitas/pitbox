package app.pitbox.secure;

import android.os.Build;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyInfo;
import android.security.keystore.KeyProperties;
import android.security.keystore.StrongBoxUnavailableException;
import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.KeyFactory;
import java.security.KeyPairGenerator;
import java.security.KeyStore;
import java.security.PrivateKey;
import java.security.Signature;
import java.security.cert.Certificate;
import java.security.spec.ECGenParameterSpec;

/**
 * The device signing key of the sync protocol (ADR 0025, ADR 0033): an ECDSA P-256 key pair that
 * lives in the Android Keystore and never leaves it. This class has no Capacitor dependency.
 *
 * Nothing here returns, logs or serialises private key material, and no method accepts a key.
 * The key is not bound to user authentication on purpose: sync runs without the user present.
 *
 * Keys are named by the user hash the app already uses for the local database. The alias is
 * built here from a validated hash, never from free text, so a caller cannot reach another alias.
 */
final class DeviceKeyStore {

    static final String PROVIDER = "AndroidKeyStore";
    /** One key per user of this phone: a logout or a revocation of one must not touch another. */
    private static final String ALIAS_PREFIX = "pitbox.device-key.v1.";
    private static final java.util.regex.Pattern USER_HASH =
        java.util.regex.Pattern.compile("^[0-9a-f]{64}$");
    /** Sync operations are small; this only stops the plugin from being fed arbitrary blobs. */
    static final int MAX_MESSAGE_BYTES = 256 * 1024;
    /** SubjectPublicKeyInfo DER of an uncompressed P-256 key (ADR 0025). */
    static final int SPKI_BYTES = 91;

    static final class PublicInfo {

        final byte[] spki;
        final String securityLevel;

        PublicInfo(byte[] spki, String securityLevel) {
            this.spki = spki;
            this.securityLevel = securityLevel;
        }
    }

    /** Creates the key if missing (idempotent) and returns its public half. */
    synchronized PublicInfo ensure(String userHash) throws GeneralSecurityException, IOException {
        String alias = aliasFor(userHash);
        KeyStore keyStore = load();
        if (!keyStore.containsAlias(alias)) {
            generate(alias);
        }
        Certificate certificate = keyStore.getCertificate(alias);
        if (certificate == null) {
            throw new GeneralSecurityException("device key has no certificate");
        }
        byte[] spki = certificate.getPublicKey().getEncoded();
        // Fail closed: anything but a P-256 SPKI would be rejected by the server anyway.
        if (spki == null || spki.length != SPKI_BYTES) {
            throw new GeneralSecurityException("unexpected public key encoding");
        }
        return new PublicInfo(spki, securityLevel(keyStore, alias));
    }

    /** ECDSA-SHA256. Returns the signature in ASN.1 DER; the JavaScript side converts it. */
    byte[] sign(String userHash, byte[] message) throws GeneralSecurityException, IOException {
        if (message == null || message.length == 0 || message.length > MAX_MESSAGE_BYTES) {
            throw new IllegalArgumentException("message size out of range");
        }
        KeyStore keyStore = load();
        PrivateKey privateKey = (PrivateKey) keyStore.getKey(aliasFor(userHash), null);
        if (privateKey == null) {
            throw new GeneralSecurityException("device key missing");
        }
        Signature signature = Signature.getInstance("SHA256withECDSA");
        signature.initSign(privateKey);
        signature.update(message);
        return signature.sign();
    }

    /** Deletes the key; a missing key is not an error. */
    synchronized void delete(String userHash) throws GeneralSecurityException, IOException {
        String alias = aliasFor(userHash);
        KeyStore keyStore = load();
        if (keyStore.containsAlias(alias)) {
            keyStore.deleteEntry(alias);
        }
    }

    /** Only a 64-character lowercase hex hash makes an alias; anything else is refused. */
    static String aliasFor(String userHash) {
        if (userHash == null || !USER_HASH.matcher(userHash).matches()) {
            throw new IllegalArgumentException("invalid user hash");
        }
        return ALIAS_PREFIX + userHash;
    }

    private static KeyStore load() throws GeneralSecurityException, IOException {
        KeyStore keyStore = KeyStore.getInstance(PROVIDER);
        keyStore.load(null);
        return keyStore;
    }

    private static void generate(String alias) throws GeneralSecurityException {
        KeyGenParameterSpec.Builder spec = new KeyGenParameterSpec.Builder(
            alias,
            KeyProperties.PURPOSE_SIGN
        )
            .setAlgorithmParameterSpec(new ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256);

        KeyPairGenerator generator = KeyPairGenerator.getInstance(
            KeyProperties.KEY_ALGORITHM_EC,
            PROVIDER
        );
        try {
            // StrongBox (a separate secure chip) when the phone has one, otherwise the TEE.
            spec.setIsStrongBoxBacked(true);
            generator.initialize(spec.build());
            generator.generateKeyPair();
        } catch (StrongBoxUnavailableException e) {
            spec.setIsStrongBoxBacked(false);
            generator.initialize(spec.build());
            generator.generateKeyPair();
        }
    }

    private static String securityLevel(KeyStore keyStore, String alias) {
        try {
            PrivateKey privateKey = (PrivateKey) keyStore.getKey(alias, null);
            KeyFactory factory = KeyFactory.getInstance(privateKey.getAlgorithm(), PROVIDER);
            KeyInfo info = factory.getKeySpec(privateKey, KeyInfo.class);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                switch (info.getSecurityLevel()) {
                    case KeyProperties.SECURITY_LEVEL_STRONGBOX:
                        return "strongbox";
                    case KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT:
                        return "tee";
                    case KeyProperties.SECURITY_LEVEL_SOFTWARE:
                        return "software";
                    default:
                        return "unknown";
                }
            }
            return info.isInsideSecureHardware() ? "tee" : "software";
        } catch (GeneralSecurityException | RuntimeException e) {
            return "unknown";
        }
    }
}
