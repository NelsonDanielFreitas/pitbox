import type { PartWithStock, StockMovement, StockReason } from "./models";
import { MAX_STOCK_CENTI } from "./models";

/** +1 adds stock, -1 removes it, 0 lets the user choose (adjustments). */
export const reasonSign: Record<StockReason, 1 | -1 | 0> = {
  purchase: 1,
  returned: 1,
  consumed: -1,
  installed: -1,
  lost: -1,
  adjustment: 0,
};

export function signMatchesReason(reason: StockReason, qtyDeltaCenti: number): boolean {
  if (qtyDeltaCenti === 0 || Math.abs(qtyDeltaCenti) > MAX_STOCK_CENTI) return false;
  const sign = reasonSign[reason];
  return sign === 0 || Math.sign(qtyDeltaCenti) === sign;
}

/** Quantity to buy: up to "repor até", or up to the minimum when it is not set. */
export function suggestedQtyCenti(
  part: Pick<PartWithStock, "qtyCenti" | "minStockCenti" | "reorderToCenti">,
): number {
  return Math.max(0, (part.reorderToCenti ?? part.minStockCenti) - part.qtyCenti);
}

export function crossedMinimum(change: {
  beforeCenti: number;
  afterCenti: number;
  minStockCenti: number;
}): boolean {
  return change.beforeCenti >= change.minStockCenti && change.afterCenti < change.minStockCenti;
}

export function canUndo(movement: Pick<StockMovement, "revertsMovementId" | "undone">): boolean {
  return movement.revertsMovementId === null && !movement.undone;
}
