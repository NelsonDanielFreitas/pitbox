import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import { VitePWA } from "vite-plugin-pwa";

import { DEV_NONCE, securityHeaders } from "./security-headers.ts";

const apiTarget = process.env.PITBOX_API_URL ?? "http://127.0.0.1:8081";

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

export default defineConfig(({ command }) => {
  const development = command === "serve";

  return {
    plugins: [
      canonicalDevHost(),
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
      headers: securityHeaders("production"),
      proxy,
    },
    html: development ? { cspNonce: DEV_NONCE } : undefined,
    build: {
      sourcemap: false,
    },
  };
});
