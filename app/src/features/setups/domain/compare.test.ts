// @vitest-environment node

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { compareSetupData, groupOf } from "./compare";
import type { Difference } from "./models";

interface Vector {
  name: string;
  before: Record<string, unknown>;
  after: Record<string, unknown>;
  differences: Difference[];
}

const vectorsUrl = new URL(
  "../../../../../backend/internal/setup/domain/testdata/compare/v1.json",
  import.meta.url,
);
const vectors = JSON.parse(readFileSync(fileURLToPath(vectorsUrl), "utf8")) as Vector[];

describe("compareSetupData: shared vectors with the server", () => {
  it("has vectors", () => {
    expect(vectors.length).toBeGreaterThan(0);
  });

  it.each(vectors.map((v) => [v.name, v] as const))("%s", (_name, v) => {
    expect(compareSetupData(v.before, v.after)).toEqual(v.differences);
  });
});

describe("compareSetupData", () => {
  it("lists leaves ordered by path, like the server test", () => {
    const a = { diffs: { front_cst: 500, rear_cst: 300 }, bars: { front_mm: 3 } };
    const b = { diffs: { front_cst: 600, center_cst: 400 }, bars: { rear_mm: 2 } };
    const diffs = compareSetupData(a, b);
    expect(diffs.map((d) => [d.path, d.kind])).toEqual([
      ["bars.front_mm", "removed"],
      ["bars.rear_mm", "added"],
      ["diffs.center_cst", "added"],
      ["diffs.front_cst", "changed"],
      ["diffs.rear_cst", "removed"],
    ]);
  });

  it("ignores empty groups and compares numbers by value", () => {
    expect(compareSetupData({ bars: {} }, {})).toEqual([]);
    expect(compareSetupData({ n: 5000 }, { n: 5000.0 })).toEqual([]);
  });

  it("flattens an added or removed group into its leaves", () => {
    expect(compareSetupData({}, { shocks: { front: { oil_cst: 400 } } })).toEqual([
      { path: "shocks.front.oil_cst", kind: "added", after: 400 },
    ]);
  });

  it("compares arrays by index", () => {
    expect(compareSetupData({ a: [1, 2] }, { a: [1, 3, 4] })).toEqual([
      { path: "a[1]", kind: "changed", before: 2, after: 3 },
      { path: "a[2]", kind: "added", after: 4 },
    ]);
  });

  it("treats a changed type as a change", () => {
    expect(compareSetupData({ a: 1 }, { a: "1" })).toEqual([{ path: "a", kind: "changed", before: 1, after: "1" }]);
  });

  const data = fc.dictionary(fc.string({ maxLength: 4 }), fc.jsonValue({ maxDepth: 2 }), { maxKeys: 5 });

  it("finds no difference between a value and itself", () => {
    fc.assert(
      fc.property(data, (d) => {
        expect(compareSetupData(d, d)).toEqual([]);
      }),
    );
  });

  it("is symmetric: swapping the sides swaps added and removed", () => {
    const swap = (d: Difference): Difference => {
      if (d.kind === "added") return { path: d.path, kind: "removed", before: d.after };
      if (d.kind === "removed") return { path: d.path, kind: "added", after: d.before };
      return { path: d.path, kind: "changed", before: d.after, after: d.before };
    };
    fc.assert(
      fc.property(data, data, (a, b) => {
        expect(compareSetupData(b, a)).toEqual(compareSetupData(a, b).map(swap));
      }),
    );
  });

  it("returns paths in order", () => {
    fc.assert(
      fc.property(data, data, (a, b) => {
        const paths = compareSetupData(a, b).map((d) => d.path);
        expect(paths).toEqual([...paths].sort());
      }),
    );
  });
});

describe("groupOf", () => {
  it("keeps the shock side and uses the top-level key otherwise", () => {
    expect(groupOf("shocks.front.oil_cst")).toBe("shocks.front");
    expect(groupOf("diffs.front_cst")).toBe("diffs");
    expect(groupOf("future")).toBe("future");
  });
});
