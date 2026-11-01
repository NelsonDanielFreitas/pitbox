import { describe, expect, it } from "vitest";

import { parseTailnetHost } from "../../tailnet-host";

describe("parseTailnetHost", () => {
  it("is off when unset or empty", () => {
    expect(parseTailnetHost(undefined)).toBeUndefined();
    expect(parseTailnetHost("")).toBeUndefined();
  });

  it.each(["pc.tail1234.ts.net", "portatil-wsl.tail9f9359.ts.net", "a.b.c.ts.net"])(
    "accepts %s",
    (host) => {
      expect(parseTailnetHost(host)).toBe(host);
    },
  );

  it.each([
    "http://pc.tail1.ts.net",
    "https://pc.tail1.ts.net",
    "pc.tail1.ts.net:4173",
    "pc.tail1.ts.net/x",
    "ts.net",
    ".ts.net",
    "pc.ts.net.evil.example",
    "pc.example.com",
    "localhost",
    "evilts.net",
    "PC.tail1.ts.net",
    " pc.tail1.ts.net",
    "pc.tail1.ts.net\n",
    "pc..ts.net",
    "*.ts.net",
    "-pc.tail1.ts.net",
  ])("rejects %j", (host) => {
    expect(() => parseTailnetHost(host)).toThrow(/PITBOX_TAILNET_HOST/);
  });
});
