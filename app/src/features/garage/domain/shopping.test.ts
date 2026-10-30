import { describe, expect, it } from "vitest";

import type { ShoppingLine } from "./models";
import { groupByBrand, lineCostCents, shoppingShareText, totalCostCents } from "./shopping";

const line = (over: Partial<ShoppingLine>): ShoppingLine => ({
  partId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
  kind: "bearing",
  description: "Rolamento",
  brand: "Mugen",
  reference: "B1",
  unit: "un",
  stockCenti: 100,
  qtyCenti: 300,
  suggestedQtyCenti: 300,
  belowMin: true,
  note: null,
  unitCostCents: 250,
  ...over,
});

const labels = {
  heading: "Lista de compras",
  noBrand: "Sem marca",
  total: "Total",
  locale: "pt-PT",
  formatMoney: (cents: number) => `${(cents / 100).toFixed(2)} €`,
};

describe("shopping list text", () => {
  it("multiplies quantity by unit cost and ignores lines without a cost", () => {
    expect(lineCostCents(line({}))).toBe(750);
    expect(lineCostCents(line({ unitCostCents: null }))).toBeNull();
    const hidden = line({});
    delete hidden.unitCostCents;
    expect(lineCostCents(hidden)).toBeNull();
    expect(totalCostCents([line({}), line({ unitCostCents: null })])).toBe(750);
  });

  it("groups by brand with the lines without brand last", () => {
    const groups = groupByBrand(
      [line({ brand: null }), line({ brand: "Zeta" }), line({ brand: "Alfa" })],
      "Sem marca",
      "pt-PT",
    );
    expect(groups.map(([brand]) => brand)).toEqual(["Alfa", "Zeta", "Sem marca"]);
  });

  it("includes costs only when allowed", () => {
    const lines = [line({}), line({ brand: null, reference: null, description: "Cola", unit: "ml", qtyCenti: 250 })];
    const withCosts = shoppingShareText(lines, labels, true);
    expect(withCosts).toContain("- Rolamento (B1) · 3 un · 7.50 €");
    expect(withCosts).toContain("Total: 13.75 €");
    const pilot = shoppingShareText(lines, labels, false);
    expect(pilot).toContain("- Rolamento (B1) · 3 un");
    expect(pilot).toContain("- Cola · 2,5 ml");
    expect(pilot).not.toMatch(/€|Total/);
  });
});
