import { describe, expect, it } from "vitest";

import { normalizeSearch } from "./normalize-search";

describe("normalizeSearch", () => {
  it("ignores case, accents and surrounding spaces", () => {
    expect(normalizeSearch("  Combustível ")).toBe("combustivel");
    expect(normalizeSearch("ÓLEO")).toBe("oleo");
  });
});
