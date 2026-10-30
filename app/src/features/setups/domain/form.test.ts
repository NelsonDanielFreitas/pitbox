import { describe, expect, it } from "vitest";

import { fieldByPath } from "./catalogue";
import { applyTexts, changesPerGroup, initialTexts, textOf, totalChanges } from "./form";

const base = {
  diffs: { front_cst: 500 },
  geometry: { front_camber_deg: -1.5 },
  bars: { rear_removed: true },
  future: { thing: 1 },
};

const field = (path: string) => {
  const def = fieldByPath(path);
  if (!def) throw new Error(path);
  return def;
};

describe("initialTexts / textOf", () => {
  it("turns stored values into input text with a decimal comma", () => {
    const texts = initialTexts(base);
    expect(texts["diffs.front_cst"]).toBe("500");
    expect(texts["geometry.front_camber_deg"]).toBe("-1,5");
    expect(texts["bars.rear_removed"]).toBe("true");
    expect(texts["aero.body"]).toBe("");
  });

  it("reads a flag as empty when it is not true", () => {
    expect(textOf(field("bars.front_removed"), { bars: { front_removed: false } })).toBe("");
  });
});

describe("applyTexts", () => {
  it("returns the base unchanged for the initial texts", () => {
    const known = { diffs: base.diffs, geometry: base.geometry, bars: base.bars };
    const result = applyTexts(known, initialTexts(known));
    expect(result.data).toEqual(known);
    expect(result.issues).toEqual({});
  });

  it("reports a key the schema does not know, so it can never be saved by mistake", () => {
    expect(applyTexts(base, initialTexts(base)).issues).toEqual({ future: "setup.data.unknown" });
  });

  it("applies edits, drops cleared fields and keeps unknown keys", () => {
    const texts = { ...initialTexts(base), "diffs.front_cst": "", "shocks.front.oil_cst": "400", "aero.body": " Mugen " };
    const result = applyTexts(base, texts);
    expect(result.data).toEqual({
      geometry: { front_camber_deg: -1.5 },
      bars: { rear_removed: true },
      shocks: { front: { oil_cst: 400 } },
      aero: { body: "Mugen" },
      future: { thing: 1 },
    });
  });

  it("unticking a flag removes it instead of storing false", () => {
    const result = applyTexts(base, { ...initialTexts(base), "bars.rear_removed": "" });
    expect(result.data).not.toHaveProperty("bars");
  });

  it("reports unreadable text and keeps the stored value", () => {
    const result = applyTexts(base, { ...initialTexts(base), "diffs.front_cst": "abc" });
    expect(result.issues["diffs.front_cst"]).toBe("setup.data.type");
    expect(result.data).toEqual(base);
  });

  it("reports schema rules by path", () => {
    const result = applyTexts(base, {
      ...initialTexts(base),
      "geometry.front_camber_deg": "-9",
      "bars.rear_mm": "2",
      "aero.body": "x".repeat(81),
    });
    expect(result.issues).toMatchObject({
      "geometry.front_camber_deg": "setup.data.range",
      "bars.rear_mm": "setup.data.bars_conflict",
      "aero.body": "setup.data.length",
    });
  });

  it("does not modify the base", () => {
    const copy = structuredClone(base);
    applyTexts(base, { ...initialTexts(base), "diffs.front_cst": "600" });
    expect(base).toEqual(copy);
  });
});

describe("changesPerGroup", () => {
  it("counts changed leaves per group, splitting the shock sides", () => {
    const after = { ...base, diffs: { front_cst: 600, rear_cst: 300 }, shocks: { front: { oil_cst: 400 } } };
    const counts = changesPerGroup(base, after);
    expect(counts).toEqual({ diffs: 2, "shocks.front": 1 });
    expect(totalChanges(counts)).toBe(3);
  });

  it("is empty for equal data", () => {
    expect(totalChanges(changesPerGroup(base, structuredClone(base)))).toBe(0);
  });
});
