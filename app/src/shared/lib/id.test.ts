import { describe, expect, it } from "vitest";

import { newId } from "./id";

describe("newId", () => {
  it("returns UUIDv7 values in time order", () => {
    const first = newId();
    const second = newId();
    expect(first).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(second > first).toBe(true);
  });
});
