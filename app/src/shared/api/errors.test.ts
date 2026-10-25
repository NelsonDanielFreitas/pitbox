import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { isUnauthenticated } from "./errors";

describe("isUnauthenticated", () => {
  it("only accepts Connect UNAUTHENTICATED errors", () => {
    expect(isUnauthenticated(new ConnectError("no session", Code.Unauthenticated))).toBe(true);
    expect(isUnauthenticated(new ConnectError("other", Code.Internal))).toBe(false);
    expect(isUnauthenticated(new Error("unauthenticated"))).toBe(false);
  });
});
