import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { fieldByPath } from "./catalogue";
import { fieldToText, getPath, normalizeData, parseFieldText, setPath } from "./data";

const field = (path: string) => {
  const def = fieldByPath(path);
  if (!def) throw new Error(`no field ${path}`);
  return def;
};

describe("getPath / setPath", () => {
  it("reads nested values and returns undefined for missing ones", () => {
    const data = { shocks: { front: { oil_cst: 400 } } };
    expect(getPath(data, "shocks.front.oil_cst")).toBe(400);
    expect(getPath(data, "shocks.rear.oil_cst")).toBeUndefined();
    expect(getPath(data, "diffs.front_cst")).toBeUndefined();
  });

  it("sets a value without touching the input or other keys", () => {
    const data = { diffs: { front_cst: 500 }, future: { thing: [1, 2] } };
    const next = setPath(data, "diffs.rear_cst", 300);
    expect(next).toEqual({ diffs: { front_cst: 500, rear_cst: 300 }, future: { thing: [1, 2] } });
    expect(data).toEqual({ diffs: { front_cst: 500 }, future: { thing: [1, 2] } });
  });

  it("creates missing groups", () => {
    expect(setPath({}, "shocks.front.oil_cst", 400)).toEqual({ shocks: { front: { oil_cst: 400 } } });
  });

  it("removes a key and the groups it leaves empty", () => {
    expect(setPath({ shocks: { front: { oil_cst: 400 } }, diffs: { front_cst: 1 } }, "shocks.front.oil_cst", undefined)).toEqual({
      diffs: { front_cst: 1 },
    });
  });

  it("setting then removing returns the original data", () => {
    fc.assert(
      fc.property(fc.integer({ min: 100, max: 2000 }), (value) => {
        const base = { diffs: { front_cst: 500 } };
        expect(setPath(setPath(base, "shocks.rear.oil_cst", value), "shocks.rear.oil_cst", undefined)).toEqual(base);
      }),
    );
  });
});

describe("normalizeData", () => {
  it("drops empty groups at any depth and copies the rest", () => {
    const data = { bars: {}, shocks: { front: {}, rear: { oil_cst: 300 } }, diffs: { front_cst: 1 } };
    expect(normalizeData(data)).toEqual({ shocks: { rear: { oil_cst: 300 } }, diffs: { front_cst: 1 } });
    expect(data.bars).toEqual({});
  });

  it("is idempotent", () => {
    fc.assert(
      fc.property(fc.dictionary(fc.string(), fc.jsonValue()), (data) => {
        const once = normalizeData(data);
        expect(normalizeData(once)).toEqual(once);
      }),
    );
  });
});

describe("parseFieldText", () => {
  it("omits empty text", () => {
    expect(parseFieldText(field("diffs.front_cst"), "  ")).toEqual({ ok: true, value: undefined });
  });

  it.each([
    ["geometry.front_camber_deg", "-1,5", -1.5],
    ["geometry.front_camber_deg", "-1.5", -1.5],
    ["diffs.front_cst", "500", 500],
    ["engine.high_needle_turns", " 2,25 ", 2.25],
  ])("reads %s = %s", (path, text, value) => {
    expect(parseFieldText(field(path), text)).toEqual({ ok: true, value });
  });

  it.each([
    ["diffs.front_cst", "500,5"],
    ["diffs.front_cst", "abc"],
    ["diffs.front_cst", "1e3"],
    ["geometry.caster_deg", "1,2,3"],
    ["geometry.caster_deg", "Infinity"],
  ])("refuses %s = %s as a type error", (path, text) => {
    expect(parseFieldText(field(path), text)).toEqual({ ok: false, ruleId: "setup.data.type" });
  });

  it("trims text and ids", () => {
    expect(parseFieldText(field("aero.body"), "  Mugen  ")).toEqual({ ok: true, value: "Mugen" });
  });

  it("round-trips numbers through the input text", () => {
    fc.assert(
      fc.property(fc.double({ min: -5, max: 2, noNaN: true }), (value) => {
        const rounded = Math.round(value * 100) / 100 + 0; // + 0 turns -0 into 0
        expect(parseFieldText(field("geometry.front_camber_deg"), fieldToText(rounded))).toEqual({
          ok: true,
          value: rounded,
        });
      }),
    );
  });
});

describe("fieldToText", () => {
  it("uses a decimal comma and empty text for missing values", () => {
    expect(fieldToText(2.5)).toBe("2,5");
    expect(fieldToText("x")).toBe("x");
    expect(fieldToText(undefined)).toBe("");
    expect(fieldToText(true)).toBe("");
  });
});
