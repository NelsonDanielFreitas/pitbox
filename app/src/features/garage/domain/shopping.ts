import { formatQuantity } from "./format";
import type { ShoppingLine } from "./models";

/** Estimated cost of a line in cents, or `null` when the part has no cost (or it is hidden). */
export function lineCostCents(line: ShoppingLine): number | null {
  if (line.unitCostCents == null) return null;
  return Math.round((line.qtyCenti * line.unitCostCents) / 100);
}

export function totalCostCents(lines: readonly ShoppingLine[]): number {
  return lines.reduce((sum, line) => sum + (lineCostCents(line) ?? 0), 0);
}

/** Lines grouped by brand (without brand last), each group in list order. */
export function groupByBrand(
  lines: readonly ShoppingLine[],
  noBrand: string,
  locale: string,
): Array<[string, ShoppingLine[]]> {
  const groups = new Map<string, ShoppingLine[]>();
  for (const line of lines) {
    const key = line.brand ?? noBrand;
    groups.set(key, [...(groups.get(key) ?? []), line]);
  }
  return [...groups.entries()].sort(([a], [b]) => {
    if (a === noBrand) return 1;
    if (b === noBrand) return -1;
    return a.localeCompare(b, locale);
  });
}

export interface ShareLabels {
  heading: string;
  noBrand: string;
  total: string;
  locale: string;
  formatMoney: (cents: number) => string;
}

/**
 * Plain text for WhatsApp or email. It has the description, reference and quantity of each
 * line and, only when `withCosts`, the costs: a pilot's copy never carries them.
 */
export function shoppingShareText(
  lines: readonly ShoppingLine[],
  labels: ShareLabels,
  withCosts: boolean,
): string {
  const out: string[] = [labels.heading, ""];
  for (const [brand, group] of groupByBrand(lines, labels.noBrand, labels.locale)) {
    out.push(`*${brand}*`);
    for (const line of group) {
      const reference = line.reference ? ` (${line.reference})` : "";
      const cost = withCosts ? lineCostCents(line) : null;
      out.push(
        `- ${line.description}${reference} · ${formatQuantity(line.qtyCenti, line.unit, labels.locale)}${
          cost === null ? "" : ` · ${labels.formatMoney(cost)}`
        }`,
      );
    }
    out.push("");
  }
  if (withCosts) out.push(`${labels.total}: ${labels.formatMoney(totalCostCents(lines))}`);
  return out.join("\n").trim();
}
