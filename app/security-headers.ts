const DEV_NONCE = "pitbox-dev";

function baseCSP(): string {
  return [
    "default-src 'none'",
    "img-src 'self'",
    "font-src 'self'",
    "manifest-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "worker-src 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
}

export function securityHeaders(mode: "development" | "production"): Record<string, string> {
  const script =
    mode === "development"
      ? `script-src 'self' 'nonce-${DEV_NONCE}' 'wasm-unsafe-eval'`
      : "script-src 'self' 'wasm-unsafe-eval'";
  const style =
    mode === "development" ? `style-src 'self' 'nonce-${DEV_NONCE}'` : "style-src 'self'";
  const connect =
    mode === "development"
      ? "connect-src 'self' ws://localhost:5173 ws://127.0.0.1:5173"
      : "connect-src 'self'";

  return {
    "Content-Security-Policy": [baseCSP(), script, style, connect].join("; "),
    "Cross-Origin-Opener-Policy": "same-origin",
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "same-origin",
    "Cache-Control": "no-cache",
  };
}

export { DEV_NONCE };
