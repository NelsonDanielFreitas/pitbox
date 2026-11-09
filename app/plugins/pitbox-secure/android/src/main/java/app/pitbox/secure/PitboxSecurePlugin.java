package app.pitbox.secure;

import android.util.Log;
import androidx.appcompat.app.AppCompatActivity;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
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

    private final DeviceKeyStore deviceKey = new DeviceKeyStore();
    private final AtomicBoolean promptOpen = new AtomicBoolean(false);

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

    private static void fail(PluginCall call, String code, String message, Exception e) {
        Log.w(TAG, code + ": " + e.getClass().getSimpleName());
        call.reject(message, code);
    }
}
