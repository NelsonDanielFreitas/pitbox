import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { formatGearRatio, gearRatio, gearRatioOf } from "./transmission";

describe("gearRatio", () => {
  it("is spur over bell, rounded to two decimals", () => {
    expect(gearRatio(46, 13)).toBe(3.54);
    expect(gearRatio(50, 50)).toBe(1);
  });

  it("is null when a value is missing or not positive", () => {
    expect(gearRatio(undefined, 13)).toBeNull();
    expect(gearRatio(46, undefined)).toBeNull();
    expect(gearRatio(46, 0)).toBeNull();
    expect(gearRatio(Number.NaN, 13)).toBeNull();
  });

  it("reads the teeth from the setup data", () => {
    expect(gearRatioOf({ transmission: { spur_teeth: 46, clutch_bell_teeth: 13 } })).toBe(3.54);
    expect(gearRatioOf({ transmission: { spur_teeth: 46 } })).toBeNull();
    expect(gearRatioOf({})).toBeNull();
  });

  it("is positive and grows with the spur", () => {
    fc.assert(
      fc.property(fc.integer({ min: 8, max: 59 }), fc.integer({ min: 8, max: 60 }), (spur, bell) => {
        const lower = gearRatio(spur, bell);
        const higher = gearRatio(spur + 1, bell);
        expect(lower).not.toBeNull();
        expect(lower ?? 0).toBeGreaterThan(0);
        expect(higher ?? 0).toBeGreaterThanOrEqual(lower ?? 0);
      }),
    );
  });

  it("shrinks as the bell grows", () => {
    fc.assert(
      fc.property(fc.integer({ min: 8, max: 60 }), fc.integer({ min: 8, max: 59 }), (spur, bell) => {
        expect(gearRatio(spur, bell + 1) ?? 0).toBeLessThanOrEqual(gearRatio(spur, bell) ?? 0);
      }),
    );
  });

  it("formats with two decimals and a dash for null", () => {
    expect(formatGearRatio(3.5)).toBe("3,50");
    expect(formatGearRatio(3.5, "en")).toBe("3.50");
    expect(formatGearRatio(null)).toBe("—");
  });
});
