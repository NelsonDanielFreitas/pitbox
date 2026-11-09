package app.pitbox.secure;

import android.content.Intent;
import android.net.Uri;
import android.util.Log;
import androidx.activity.result.ActivityResultLauncher;
import androidx.appcompat.app.AppCompatActivity;
import androidx.browser.auth.AuthTabIntent;
import androidx.browser.customtabs.CustomTabsClient;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.security.SecureRandom;
import java.util.Base64;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Bridge between the WebView and the Android Keystore (ADR 0033). The surface is deliberately
 * tiny and has no method that returns private key material.
 *
 * Errors: only stable codes and generic messages cross the bridge. Exceptions are logged by
 * class name alone, never by message, because a message can carry key or input details.
 */
@CapacitorPlugin(name = "PitboxSecure")
public class PitboxSecurePlugin extends Plugin {

    private static final String TAG = "PitboxSecure";
    private static final int MAX_TITLE = 64;
    private static final int MAX_SUBTITLE = 128;
    /** Login proofs are a few hundred bytes. */
    private static final int MAX_PROOF_BYTES = 1024;

    private final DeviceKeyStore deviceKey = new DeviceKeyStore();
    private final DeviceKeyStore sessionKey = new DeviceKeyStore(
        DeviceKeyStore.SESSION_PREFIX,
        DeviceKeyStore.SESSION_NAME_PATTERN
    );
    private final SecureRandom random = new SecureRandom();
    private final AtomicBoolean promptOpen = new AtomicBoolean(false);
    private final AtomicBoolean loginOpen = new AtomicBoolean(false);
    private volatile PluginCall pendingLogin;
    private ActivityResultLauncher<Intent> authTabLauncher;

    /**
     * The Auth Tab launcher has to be registered before the activity starts; Capacitor calls
     * load() while the activity is being created. There is no intent filter for the redirect:
     * the browser hands it straight back to this launcher, so no other app can receive it.
     */
    @Override
    public void load() {
        AppCompatActivity activity = getActivity();
        if (activity != null) {
            authTabLauncher = AuthTabIntent.registerActivityResultLauncher(activity, this::onAuthResult);
        }
    }

    @PluginMethod
    public void ensureDeviceKey(PluginCall call) {
        try {
            DeviceKeyStore.PublicInfo info = deviceKey.ensure(call.getString("userHash"));
            JSObject result = new JSObject();
            result.put("publicKey", Base64.getEncoder().encodeToString(info.spki));
            result.put("securityLevel", info.securityLevel);
            call.resolve(result);
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
        } catch (Exception e) {
            fail(call, "KEYSTORE_UNAVAILABLE", "Device key unavailable", e);
        }
    }

    @PluginMethod
    public void signWithDeviceKey(PluginCall call) {
        String encoded = call.getString("message");
        if (encoded == null || encoded.isEmpty()) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        byte[] message;
        try {
            // The strict decoder rejects characters outside the base64 alphabet and bad padding.
            message = Base64.getDecoder().decode(encoded);
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        if (message.length == 0 || message.length > DeviceKeyStore.MAX_MESSAGE_BYTES) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        try {
            JSObject result = new JSObject();
            result.put(
                "signature",
                Base64.getEncoder().encodeToString(deviceKey.sign(call.getString("userHash"), message))
            );
            call.resolve(result);
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
        } catch (Exception e) {
            fail(call, "KEYSTORE_UNAVAILABLE", "Device key unavailable", e);
        }
    }

    @PluginMethod
    public void deleteDeviceKey(PluginCall call) {
        try {
            deviceKey.delete(call.getString("userHash"));
            call.resolve();
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
        } catch (Exception e) {
            fail(call, "KEYSTORE_UNAVAILABLE", "Device key unavailable", e);
        }
    }

    @PluginMethod
    public void authenticate(final PluginCall call) {
        String title = call.getString("title");
        String subtitle = call.getString("subtitle");
        if (
            title == null ||
            title.isEmpty() ||
            title.length() > MAX_TITLE ||
            (subtitle != null && subtitle.length() > MAX_SUBTITLE)
        ) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        AppCompatActivity activity = getActivity();
        if (activity == null) {
            call.reject("Authentication failed", "BIOMETRIC_FAILED");
            return;
        }
        // One prompt at a time: a second call while one is open would stack dialogs.
        if (!promptOpen.compareAndSet(false, true)) {
            call.reject("Authentication failed", "BIOMETRIC_FAILED");
            return;
        }
        BiometricGate.authenticate(
            activity,
            title,
            subtitle,
            new BiometricGate.Callback() {
                @Override
                public void onSuccess() {
                    promptOpen.set(false);
                    call.resolve();
                }

                @Override
                public void onFailure(String code) {
                    promptOpen.set(false);
                    call.reject("Authentication failed", code);
                }
            }
        );
    }


    // --- login session key (ADR 0032): distinct from the sync key, one per login ---

    @PluginMethod
    public void createSessionKey(PluginCall call) {
        byte[] id = new byte[16];
        random.nextBytes(id);
        String keyId = toHex(id);
        try {
            DeviceKeyStore.PublicInfo info = sessionKey.ensure(keyId);
            JSObject result = new JSObject();
            result.put("keyId", keyId);
            result.put("publicKey", Base64.getEncoder().encodeToString(info.spki));
            result.put("securityLevel", info.securityLevel);
            call.resolve(result);
        } catch (Exception e) {
            fail(call, "KEYSTORE_UNAVAILABLE", "Session key unavailable", e);
        }
    }

    @PluginMethod
    public void signWithSessionKey(PluginCall call) {
        String encoded = call.getString("message");
        if (encoded == null || encoded.isEmpty()) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        byte[] message;
        try {
            message = Base64.getDecoder().decode(encoded);
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        if (message.length == 0 || message.length > MAX_PROOF_BYTES) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        try {
            JSObject result = new JSObject();
            result.put(
                "signature",
                Base64.getEncoder().encodeToString(sessionKey.sign(call.getString("keyId"), message))
            );
            call.resolve(result);
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
        } catch (Exception e) {
            fail(call, "KEYSTORE_UNAVAILABLE", "Session key unavailable", e);
        }
    }

    @PluginMethod
    public void deleteSessionKey(PluginCall call) {
        try {
            sessionKey.delete(call.getString("keyId"));
            call.resolve();
        } catch (IllegalArgumentException e) {
            call.reject("Invalid input", "INVALID_INPUT");
        } catch (Exception e) {
            fail(call, "KEYSTORE_UNAVAILABLE", "Session key unavailable", e);
        }
    }

    // --- browser login (Auth Tab, mandatory: no fallback) ---

    @PluginMethod
    public void loginWithBrowser(PluginCall call) {
        String url = call.getString("url");
        if (!AuthRedirect.isAcceptableAuthorizationUrl(url)) {
            call.reject("Invalid input", "INVALID_INPUT");
            return;
        }
        AppCompatActivity activity = getActivity();
        if (activity == null || authTabLauncher == null) {
            call.reject("Login unavailable", "AUTH_TAB_UNAVAILABLE");
            return;
        }
        // Fail closed: without an Auth Tab the redirect would need an intent filter that any
        // other app could register too. There is no Custom Tab fallback.
        String browser = CustomTabsClient.getPackageName(activity, null, false);
        if (browser == null || !CustomTabsClient.isAuthTabSupported(activity, browser)) {
            call.reject("Login unavailable", "AUTH_TAB_UNAVAILABLE");
            return;
        }
        if (!loginOpen.compareAndSet(false, true)) {
            call.reject("Login already open", "LOGIN_FAILED");
            return;
        }
        // Not saved by Capacitor: the result arrives on the launcher, not through the bridge.
        pendingLogin = call;
        try {
            new AuthTabIntent.Builder().build().launch(authTabLauncher, Uri.parse(url), AuthRedirect.REDIRECT_SCHEME);
        } catch (RuntimeException e) {
            finishLogin("AUTH_TAB_UNAVAILABLE", "Login unavailable", null);
            Log.w(TAG, "AUTH_TAB_UNAVAILABLE: " + e.getClass().getSimpleName());
        }
    }

    private void onAuthResult(AuthTabIntent.AuthResult result) {
        if (result.resultCode == AuthTabIntent.RESULT_CANCELED) {
            finishLogin("LOGIN_CANCELLED", "Login cancelled", null);
            return;
        }
        // The URI is only looked at on RESULT_OK; every other code is a failure.
        AuthRedirect.Result parsed =
            result.resultCode == AuthTabIntent.RESULT_OK && result.resultUri != null
                ? AuthRedirect.parse(result.resultUri.toString())
                : null;
        if (parsed == null || parsed.code == null) {
            finishLogin("LOGIN_FAILED", "Login failed", null);
            return;
        }
        JSObject out = new JSObject();
        out.put("code", parsed.code);
        out.put("state", parsed.state);
        finishLogin(null, null, out);
    }

    private void finishLogin(String code, String message, JSObject out) {
        PluginCall call = pendingLogin;
        pendingLogin = null;
        loginOpen.set(false);
        if (call == null) {
            return;
        }
        if (out != null) {
            call.resolve(out);
        } else {
            call.reject(message, code);
        }
    }

    private static String toHex(byte[] bytes) {
        char[] digits = "0123456789abcdef".toCharArray();
        char[] out = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            out[i * 2] = digits[(bytes[i] >> 4) & 0xF];
            out[i * 2 + 1] = digits[bytes[i] & 0xF];
        }
        return new String(out);
    }

    private static void fail(PluginCall call, String code, String message, Exception e) {
        Log.w(TAG, code + ": " + e.getClass().getSimpleName());
        call.reject(message, code);
    }
}
