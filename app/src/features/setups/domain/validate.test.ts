import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { setupFields } from "./catalogue";
import { MAX_DATA_BYTES, MAX_DATA_DEPTH } from "./models";
import { ruleIds, validateSetupData, type SetupIssue } from "./validate";

const engineId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";

const valid = {
  diffs: { front_cst: 500, rear_cst: 300, front_ml: 12.5, fill_notes: "Óleo novo" },
  shocks: { front: { oil_cst: 400, piston_holes: 3 }, rear: { oil_cst: 350 } },
  geometry: { front_camber_deg: -2, rear_toe_deg: 1.5 },
  bars: { front_mm: 3, rear_removed: true },
  engine: { engine_id: engineId, high_needle_turns: 2.5 },
};

const first = (data: Record<string, unknown>): SetupIssue | undefined => validateSetupData(data)[0];

describe("validateSetupData: cases mirrored from the server tests", () => {
  it.each([
    ["empty", {}],
    ["valid", valid],
    ["empty group", { bars: {} }],
  ])("accepts %s", (_name, data) => {
    expect(validateSetupData(data)).toEqual([]);
  });

  it.each([
    ["unknown field", { nope: true }, "nope", ruleIds.unknown],
    ["unknown field in a group", { diffs: { nope: 1 } }, "diffs.nope", ruleIds.unknown],
    ["wrong type", { diffs: { front_cst: "500" } }, "diffs.front_cst", ruleIds.type],
    ["fractional int", { diffs: { front_cst: 500.5 } }, "diffs.front_cst", ruleIds.type],
    ["below range", { diffs: { front_cst: 99 } }, "diffs.front_cst", ruleIds.range],
    ["above range", { diffs: { front_cst: 1000001 } }, "diffs.front_cst", ruleIds.range],
    ["shock oil below", { shocks: { front: { oil_cst: 99 } } }, "shocks.front.oil_cst", ruleIds.range],
    ["shock oil above", { shocks: { front: { oil_cst: 2001 } } }, "shocks.front.oil_cst", ruleIds.range],
    ["camber below", { geometry: { front_camber_deg: -6 } }, "geometry.front_camber_deg", ruleIds.range],
    ["camber above", { geometry: { front_camber_deg: 2.1 } }, "geometry.front_camber_deg", ruleIds.range],
    ["null group", { diffs: null }, "diffs", ruleIds.type],
    ["null value", { diffs: { front_cst: null } }, "diffs.front_cst", ruleIds.type],
    ["group that is a number", { diffs: 3 }, "diffs", ruleIds.type],
    ["group that is an array", { diffs: [] }, "diffs", ruleIds.type],
    ["text too long", { diffs: { fill_notes: "x".repeat(501) } }, "diffs.fill_notes", ruleIds.length],
    ["bad uuid", { engine: { engine_id: "not-a-uuid" } }, "engine.engine_id", ruleIds.uuid],
    ["nil uuid", { engine: { engine_id: "00000000-0000-0000-0000-000000000000" } }, "engine.engine_id", ruleIds.uuid],
    ["NaN", { geometry: { caster_deg: Number.NaN } }, "geometry.caster_deg", ruleIds.type],
    ["Infinity", { geometry: { caster_deg: Number.POSITIVE_INFINITY } }, "geometry.caster_deg", ruleIds.type],
    [
      "bars conflict",
      { bars: { front_removed: true, front_mm: 3 } },
      "bars.front_mm",
      ruleIds.barsConflict,
    ],
  ])("refuses %s", (_name, data, path, ruleId) => {
    expect(first(data)).toEqual({ path, ruleId });
  });

  it("counts text length in code points, not UTF-16 units", () => {
    expect(validateSetupData({ shocks: { front: { spring_brand: "😀".repeat(40) } } })).toEqual([]);
    expect(first({ shocks: { front: { spring_brand: "😀".repeat(41) } } })?.ruleId).toBe(ruleIds.length);
  });

  it("accepts a removed bar without a size", () => {
    expect(validateSetupData({ bars: { front_removed: true, rear_removed: false, rear_mm: 2 } })).toEqual([]);
  });

  it("reports every field issue in key order, not only the first", () => {
    const issues = validateSetupData({ diffs: { rear_cst: 1, front_cst: 1 }, aero: { gurney_mm: 99 } });
    expect(issues.map((i) => i.path)).toEqual(["aero.gurney_mm", "diffs.front_cst", "diffs.rear_cst"]);
  });

  it("refuses another schema version", () => {
    expect(validateSetupData({}, 2)).toEqual([{ path: "schema_version", ruleId: ruleIds.schemaVersion }]);
  });
});

describe("validateSetupData: limits", () => {
  it("refuses data deeper than the maximum", () => {
    let deep: unknown = "x";
    for (let i = 0; i < MAX_DATA_DEPTH; i++) deep = { x: deep };
    expect(first({ diffs: deep })).toEqual({ path: "data", ruleId: ruleIds.depth });
  });

  it("refuses data larger than 64 KiB", () => {
    const huge = { diffs: { fill_notes: "x".repeat(MAX_DATA_BYTES) } };
    expect(first(huge)).toEqual({ path: "data", ruleId: ruleIds.size });
  });

  it("measures size in bytes, not characters", () => {
    const wide = { diffs: { fill_notes: "é".repeat(MAX_DATA_BYTES / 2) } };
    expect(first(wide)).toEqual({ path: "data", ruleId: ruleIds.size });
  });

  it("does not modify its input", () => {
    const data = structuredClone(valid);
    validateSetupData(data);
    expect(data).toEqual(valid);
  });
});

describe("validateSetupData: properties", () => {
  const inRange = fc.constantFrom(...setupFields.filter((f) => f.kind === "number" || f.kind === "int")).chain((f) =>
    f.kind === "int"
      ? fc.integer({ min: f.min ?? 0, max: f.max ?? 0 }).map((v) => [f.path, v] as const)
      : fc
          .double({ min: f.min ?? 0, max: f.max ?? 0, noNaN: true, noDefaultInfinity: true })
          .map((v) => [f.path, v] as const),
  );

  function nest(path: string, value: unknown): Record<string, unknown> {
    return path
      .split(".")
      .reverse()
      .reduce<unknown>((acc, key) => ({ [key]: acc }), value) as Record<string, unknown>;
  }

  it("accepts every in-range numeric value of every field", () => {
    fc.assert(
      fc.property(inRange, ([path, value]) => {
        expect(validateSetupData(nest(path, value))).toEqual([]);
      }),
    );
  });

  it("refuses any value outside the range of a bounded field", () => {
    const bounded = setupFields.filter((f) => f.min !== undefined && f.max !== undefined);
    fc.assert(
      fc.property(fc.constantFrom(...bounded), fc.boolean(), fc.integer({ min: 1, max: 1000 }), (f, below, d) => {
        const v = below ? (f.min ?? 0) - d : (f.max ?? 0) + d;
        const issues = validateSetupData(nest(f.path, v));
        expect(issues).toEqual([{ path: f.path, ruleId: ruleIds.range }]);
      }),
    );
  });

  it("never throws on arbitrary JSON objects", () => {
    fc.assert(
      fc.property(fc.dictionary(fc.string(), fc.jsonValue()), (data) => {
        expect(() => validateSetupData(data)).not.toThrow();
      }),
    );
  });
});
