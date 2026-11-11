import { afterEach, describe, expect, it, vi } from "vitest";

import { generateRecoveryCodes } from "./recovery-codes";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stub(status: number, body: unknown = {}) {
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status }));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("generateRecoveryCodes", () => {
  it("asks with the session cookie and returns the codes", async () => {
    const fetchMock = stub(200, { codes: ["AAAA-BBBB", "CCCC-DDDD"] });
    await expect(generateRecoveryCodes()).resolves.toEqual({ status: "ok", codes: ["AAAA-BBBB", "CCCC-DDDD"] });
    expect(fetchMock.mock.calls[0]).toEqual([
      "/auth/recovery-codes",
      expect.objectContaining({ method: "POST", credentials: "same-origin", cache: "no-store" }),
    ]);
  });

  it.each([
    [401, "unauthenticated"],
    [403, "stepUp"],
    [503, "unavailable"],
  ] as const)("maps %i to %s", async (status, outcome) => {
    stub(status);
    await expect(generateRecoveryCodes()).resolves.toEqual({ status: outcome });
  });

  it("refuses an answer that is not the contract", async () => {
    stub(200, { codes: [] });
    await expect(generateRecoveryCodes()).resolves.toEqual({ status: "unavailable" });
    stub(200, { codes: ["A"], extra: 1 });
    await expect(generateRecoveryCodes()).resolves.toEqual({ status: "unavailable" });
  });

  it("is unavailable when the network fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("network")));
    await expect(generateRecoveryCodes()).resolves.toEqual({ status: "unavailable" });
  });
});
