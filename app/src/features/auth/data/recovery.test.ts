import { afterEach, describe, expect, it, vi } from "vitest";

import { recoveryEmailSchema, requestRecovery } from "./recovery";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(status: number | Error) {
  const fetchMock = vi.fn().mockImplementation(() =>
    status instanceof Error ? Promise.reject(status) : Promise.resolve(new Response("{}", { status })),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("requestRecovery", () => {
  it("sends only the email, as JSON, with no credentials", async () => {
    const fetchMock = stubFetch(202);
    await expect(requestRecovery("  Ana@Example.com ")).resolves.toBe("accepted");
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/auth/recover");
    expect(init).toMatchObject({ method: "POST", credentials: "omit", cache: "no-store" });
    expect(init.headers).toEqual({ "Content-Type": "application/json" });
    expect(init.body).toBe(JSON.stringify({ email: "ana@example.com" }));
  });

  it.each([
    [202, "accepted"],
    [400, "invalid"],
    [429, "rateLimited"],
    [500, "unavailable"],
    [503, "unavailable"],
    [200, "unavailable"],
  ] as const)("maps %i to %s", async (status, outcome) => {
    stubFetch(status);
    await expect(requestRecovery("ana@example.com")).resolves.toBe(outcome);
  });

  it("is unavailable, not a crash, when there is no network", async () => {
    stubFetch(new TypeError("network"));
    await expect(requestRecovery("ana@example.com")).resolves.toBe("unavailable");
  });

  it("never asks the server about an invalid address", async () => {
    const fetchMock = stubFetch(202);
    for (const bad of ["", "ana", "ana@", "a b@example.com", "x".repeat(255) + "@example.com"]) {
      await expect(requestRecovery(bad)).resolves.toBe("invalid");
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("recoveryEmailSchema", () => {
  it("trims and lowercases", () => {
    expect(recoveryEmailSchema.parse(" Ana@Example.COM ")).toBe("ana@example.com");
  });

  it("sends the recovery code when there is one, and only then", async () => {
    const fetchMock = stubFetch(202);
    await requestRecovery("ana@example.com", " abcd-efgh ");
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.body).toBe(JSON.stringify({ email: "ana@example.com", code: "abcd-efgh" }));
    fetchMock.mockClear();
    await requestRecovery("ana@example.com", "   ");
    expect((fetchMock.mock.calls[0] as [string, RequestInit])[1].body).toBe(JSON.stringify({ email: "ana@example.com" }));
  });

  it("does not call the server for a code with characters a code never has", async () => {
    const fetchMock = stubFetch(202);
    expect(await requestRecovery("ana@example.com", "abc<script>")).toBe("invalid");
    expect(await requestRecovery("ana@example.com", "A".repeat(65))).toBe("invalid");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
