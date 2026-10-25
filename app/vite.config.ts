import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

import { DEV_NONCE, securityHeaders } from "./security-headers.ts";

const apiTarget = process.env.PITBOX_API_URL ?? "http://127.0.0.1:8081";

const proxy = {
  "/auth": { target: apiTarget, changeOrigin: false },
  "/pitbox.": { target: apiTarget, changeOrigin: false },
};

export default defineConfig(({ command }) => {
  const development = command === "serve";

  return {
    plugins: [
      tanstackRouter({
        target: "react",
        autoCodeSplitting: true,
        routesDirectory: "./src/app/routes",
        generatedRouteTree: "./src/app/routeTree.gen.ts",
      }),
      react(),
      tailwindcss(),
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
