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

/**
 * The API origin the native build may talk to (F2-06, ADR 0033). HTTPS only, and just an origin:
 * no credentials, path, query or fragment. A bad value stops the build instead of widening the
 * `connect-src` of an app that holds the user's data.
 */
export function parseApiOrigin(raw: string | undefined): string | undefined {
  if (raw === undefined || raw === "") return undefined;
  const fail = (): never => {
    throw new Error(
      "PITBOX_API_ORIGIN must be an https origin without path, e.g. https://pc.tail1234.ts.net",
    );
  };
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return fail();
  }
  // The URL parser lets "*" through in a host name, which would be a wildcard in the CSP.
  const clean =
    url.protocol === "https:" &&
    /^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/.test(url.hostname) &&
    url.username === "" &&
    url.password === "" &&
    url.pathname === "/" &&
    url.search === "" &&
    url.hash === "" &&
    !raw.endsWith("?") &&
    !raw.endsWith("#");
  return clean ? url.origin : fail();
}

/**
 * CSP for the Capacitor build. The WebView serves the bundle from https://localhost without our
 * HTTP headers, so the policy travels in a <meta> tag injected at build time. A <meta> CSP
 * ignores `frame-ancestors`, so that directive is left out. No inline script or style, no eval.
 */
export function nativeCSP(apiOrigin?: string): string {
  const connect = apiOrigin ? `connect-src 'self' ${apiOrigin}` : "connect-src 'self'";
  return [
    "default-src 'none'",
    "img-src 'self'",
    "font-src 'self'",
    "manifest-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "worker-src 'self'",
    "script-src 'self' 'wasm-unsafe-eval'",
    "style-src 'self'",
    connect,
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
