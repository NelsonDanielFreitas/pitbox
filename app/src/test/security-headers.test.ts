import { describe, expect, it } from "vitest";

import { nativeCSP, parseApiOrigin, securityHeaders } from "../../security-headers.ts";

describe("parseApiOrigin", () => {
  it("accepts nothing or an https origin", () => {
    expect(parseApiOrigin(undefined)).toBeUndefined();
    expect(parseApiOrigin("")).toBeUndefined();
    expect(parseApiOrigin("https://pc.tail1234.ts.net")).toBe("https://pc.tail1234.ts.net");
    expect(parseApiOrigin("https://pc.tail1234.ts.net/")).toBe("https://pc.tail1234.ts.net");
    expect(parseApiOrigin("https://pc.tail1234.ts.net:8443")).toBe(
      "https://pc.tail1234.ts.net:8443",
    );
  });

  it.each([
    "http://pc.tail1234.ts.net",
    "https://user:pass@pc.tail1234.ts.net",
    "https://pc.tail1234.ts.net/api",
    "https://pc.tail1234.ts.net/?a=1",
    "https://pc.tail1234.ts.net?",
    "https://pc.tail1234.ts.net#x",
    "https://*.ts.net",
    "https://a.example; script-src *",
    "https://a.example https://b.example",
    "ftp://a.example",
    "pc.tail1234.ts.net",
    "*",
  ])("rejects %s", (raw) => {
    expect(() => parseApiOrigin(raw)).toThrow(/PITBOX_API_ORIGIN/);
  });
});

describe("nativeCSP", () => {
  it("is as strict as the web production policy on script and style", () => {
    const web = securityHeaders("production")["Content-Security-Policy"] ?? "";
    const native = nativeCSP();
    for (const directive of ["script-src 'self' 'wasm-unsafe-eval'", "style-src 'self'"]) {
      expect(web).toContain(directive);
      expect(native).toContain(directive);
    }
    expect(native).not.toMatch(/unsafe-inline|unsafe-eval(?!')|\*/);
    expect(native).toContain("default-src 'none'");
    expect(native).not.toContain("frame-ancestors");
  });

  it("adds the API origin to connect-src and nowhere else", () => {
    const csp = nativeCSP("https://pc.tail1234.ts.net");
    expect(csp).toContain("connect-src 'self' https://pc.tail1234.ts.net");
    expect(csp.split("https://pc.tail1234.ts.net")).toHaveLength(2);
    expect(nativeCSP()).toContain("connect-src 'self'");
    expect(nativeCSP()).not.toContain("https://");
  });
});
