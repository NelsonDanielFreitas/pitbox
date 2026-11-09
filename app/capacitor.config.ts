import type { CapacitorConfig } from "@capacitor/cli";

/**
 * Capacitor project for Android (F2-06, ADR 0033).
 *
 * Deliberately absent: `server.url`. The WebView only ever loads the bundled files from
 * `webDir`; a remote URL would turn the app into a browser for someone else's code.
 */
const config: CapacitorConfig = {
  appId: "app.pitbox.mobile",
  appName: "PitBox",
  // Its own folder: the web build (dist/) carries a service worker and must never ship in the APK.
  webDir: "dist-android",
  server: {
    // The default, stated on purpose: the bundle is served from https://localhost.
    androidScheme: "https",
  },
  // Capacitor logs every plugin call and result in verbose mode: the login code, the state and the
  // local database statements would end up in `adb logcat`. Only the debug build (the one with
  // DevTools) logs them; anything else logs nothing (ADR 0032).
  loggingBehavior: process.env.PITBOX_ANDROID_DEBUG === "1" ? "debug" : "none",
  android: {
    allowMixedContent: false,
    // Chrome DevTools on the WebView only in builds made with PITBOX_ANDROID_DEBUG=1.
    webContentsDebuggingEnabled: process.env.PITBOX_ANDROID_DEBUG === "1",
  },
  plugins: {
    CapacitorSQLite: {
      // SQLCipher; the passphrase lives inside the plugin, wrapped by the Android Keystore.
      androidIsEncryption: true,
      // The plugin's own biometric option only guards its master key creation. The app lock
      // is a separate, UI-level lock (ADR 0033 §4).
      androidBiometric: { biometricAuth: false },
    },
  },
};

export default config;
