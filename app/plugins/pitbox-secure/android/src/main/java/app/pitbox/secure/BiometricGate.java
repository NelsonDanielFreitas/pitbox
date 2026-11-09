package app.pitbox.secure;

import android.annotation.SuppressLint;
import android.os.Build;
import androidx.annotation.NonNull;
import androidx.biometric.BiometricManager;
import androidx.biometric.BiometricPrompt;
import androidx.core.content.ContextCompat;
import androidx.fragment.app.FragmentActivity;

/**
 * System biometric prompt with the device credential (PIN, pattern, password) as fallback.
 *
 * This is a UI lock for the app (ADR 0033 §4), not a key release: it unlocks nothing
 * cryptographically, and the success callback carries no secret. Everything except an explicit
 * success is a failure with a stable code.
 */
final class BiometricGate {

    interface Callback {
        void onSuccess();

        void onFailure(String code);
    }

    private BiometricGate() {}

    /** Must be called from any thread; the prompt is shown on the UI thread. */
    static void authenticate(
        final FragmentActivity activity,
        final String title,
        final String subtitle,
        final Callback callback
    ) {
        activity.runOnUiThread(() -> show(activity, title, subtitle, callback));
    }

    // Android 10 (API 29) cannot combine BIOMETRIC_STRONG with DEVICE_CREDENTIAL in
    // setAllowedAuthenticators; it needs the older setDeviceCredentialAllowed.
    @SuppressLint("WrongConstant")
    @SuppressWarnings("deprecation")
    private static void show(
        FragmentActivity activity,
        String title,
        String subtitle,
        Callback callback
    ) {
        BiometricPrompt.PromptInfo.Builder info = new BiometricPrompt.PromptInfo.Builder()
            .setTitle(title);
        if (subtitle != null) {
            info.setSubtitle(subtitle);
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            int authenticators =
                BiometricManager.Authenticators.BIOMETRIC_STRONG |
                BiometricManager.Authenticators.DEVICE_CREDENTIAL;
            int available = BiometricManager.from(activity).canAuthenticate(authenticators);
            if (available != BiometricManager.BIOMETRIC_SUCCESS) {
                callback.onFailure("BIOMETRIC_UNAVAILABLE");
                return;
            }
            info.setAllowedAuthenticators(authenticators);
        } else {
            info.setDeviceCredentialAllowed(true);
        }

        BiometricPrompt prompt = new BiometricPrompt(
            activity,
            ContextCompat.getMainExecutor(activity),
            new BiometricPrompt.AuthenticationCallback() {
                @Override
                public void onAuthenticationSucceeded(
                    @NonNull BiometricPrompt.AuthenticationResult result
                ) {
                    callback.onSuccess();
                }

                @Override
                public void onAuthenticationError(int errorCode, @NonNull CharSequence message) {
                    // The message is localised system text; only the stable code goes up.
                    callback.onFailure(codeFor(errorCode));
                }
                // onAuthenticationFailed (one wrong finger) is not terminal: the prompt stays up.
            }
        );
        prompt.authenticate(info.build());
    }

    private static String codeFor(int errorCode) {
        switch (errorCode) {
            case BiometricPrompt.ERROR_USER_CANCELED:
            case BiometricPrompt.ERROR_NEGATIVE_BUTTON:
            case BiometricPrompt.ERROR_CANCELED:
                return "BIOMETRIC_CANCELLED";
            case BiometricPrompt.ERROR_LOCKOUT:
            case BiometricPrompt.ERROR_LOCKOUT_PERMANENT:
                return "BIOMETRIC_LOCKED_OUT";
            case BiometricPrompt.ERROR_NO_BIOMETRICS:
            case BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL:
            case BiometricPrompt.ERROR_HW_NOT_PRESENT:
            case BiometricPrompt.ERROR_HW_UNAVAILABLE:
                return "BIOMETRIC_UNAVAILABLE";
            default:
                return "BIOMETRIC_FAILED";
        }
    }
}
