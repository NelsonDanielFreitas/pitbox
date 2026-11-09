import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import { VitePWA } from "vite-plugin-pwa";

import { DEV_NONCE, nativeCSP, parseApiOrigin, securityHeaders } from "./security-headers.ts";
import { parseTailnetHost } from "./tailnet-host.ts";

const apiTarget = process.env.PITBOX_API_URL ?? "http://127.0.0.1:8081";

// Phone profile (ADR 0029): `make serve-phone` exposes this server on the tailnet through
// "tailscale serve". Vite refuses unknown Host headers (DNS rebinding), so the one tailnet name
// is allowed explicitly. A bad value stops the server instead of widening the allow-list.
const tailnetHost = parseTailnetHost(process.env.PITBOX_TAILNET_HOST);

const proxy = {
  "/auth": { target: apiTarget, changeOrigin: false },
  "/pitbox.": { target: apiTarget, changeOrigin: false },
};

function canonicalDevHost(): Plugin {
  return {
    name: "pitbox-canonical-dev-host",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const host = req.headers.host ?? "";
        if (!host.startsWith("127.0.0.1")) {
          next();
          return;
        }
        const port = host.split(":")[1] ?? "5173";
        res.statusCode = 307;
        res.setHeader("Location", `http://localhost:${port}${req.url ?? "/"}`);
        res.end();
      });
    },
  };
}

// Capacitor build (`vite build --mode android`, F2-06, ADR 0033). The WebView serves the bundle
// without our HTTP headers, so the CSP goes into a <meta>, and the service worker is not built:
// the bundle ships inside the APK and updating means a new APK.
function nativeCspMeta(apiOrigin: string | undefined): Plugin {
  return {
    name: "pitbox-native-csp",
    transformIndexHtml: () => [
      {
        tag: "meta",
        attrs: { "http-equiv": "Content-Security-Policy", content: nativeCSP(apiOrigin) },
        injectTo: "head-prepend",
      },
    ],
  };
}

export default defineConfig(({ command, mode }) => {
  const development = command === "serve";
  const native = mode === "android";
  // One validated https origin (or none). It feeds both the CSP and the API client of the native build.
  const apiOrigin = native ? parseApiOrigin(process.env.PITBOX_API_ORIGIN) : undefined;

  return {
    define: {
      "import.meta.env.VITE_API_ORIGIN": JSON.stringify(apiOrigin ?? ""),
      // Only a build made on purpose for it (make android-diagnostics) has the diagnostics page.
      "import.meta.env.VITE_DIAGNOSTICS": JSON.stringify(
        native && process.env.PITBOX_DIAGNOSTICS === "1" ? "1" : "",
      ),
    },
    plugins: [
      canonicalDevHost(),
      ...(native ? [nativeCspMeta(apiOrigin)] : []),
      tanstackRouter({
        target: "react",
        autoCodeSplitting: true,
        routesDirectory: "./src/app/routes",
        generatedRouteTree: "./src/app/routeTree.gen.ts",
      }),
      react(),
      tailwindcss(),
      // ADR 0028: the service worker only pre-caches the build (hashed files). No
      // runtimeCaching, so nothing from /auth/* or /pitbox.* can reach the Cache Storage, and
      // no skipWaiting: a new version waits until the user accepts it.
      VitePWA({
        disable: native, // no sw.js inside the APK; the virtual module stays as a no-op
        strategies: "generateSW",
        registerType: "prompt",
        injectRegister: false,
        manifest: false, // app/public/manifest.webmanifest, linked from index.html
        workbox: {
          globPatterns: ["**/*.{html,js,css,wasm,woff2,png,webmanifest}"],
          navigateFallback: "index.html",
          navigateFallbackDenylist: [/^\/auth\//, /^\/pitbox\./],
          cleanupOutdatedCaches: true,
          skipWaiting: false,
          clientsClaim: false,
        },
      }),
    ],
    resolve: {
      alias: {
        "@": fileURLToPath(new URL("./src", import.meta.url)),
      },
    },
    server: {
      host: "127.0.0.1",
      port: 5173,
      strictPort: true,
      headers: securityHeaders("development"),
      proxy,
    },
    preview: {
      host: "127.0.0.1",
      port: 4173,
      strictPort: true,
      allowedHosts: tailnetHost ? [tailnetHost] : [],
      headers: securityHeaders("production"),
      proxy,
    },
    html: development ? { cspNonce: DEV_NONCE } : undefined,
    build: {
      sourcemap: false,
    },
  };
});
