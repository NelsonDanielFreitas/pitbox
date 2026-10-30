import { getPath } from "./data";
import type { SetupData } from "./models";

/** Final drive ratio: spur teeth over clutch bell teeth, rounded to 2 decimals. */
export function gearRatio(spurTeeth: number | undefined, bellTeeth: number | undefined): number | null {
  if (spurTeeth === undefined || bellTeeth === undefined) return null;
  if (!Number.isFinite(spurTeeth) || !Number.isFinite(bellTeeth) || spurTeeth <= 0 || bellTeeth <= 0) return null;
  return Math.round((spurTeeth / bellTeeth) * 100) / 100;
}

export function gearRatioOf(data: SetupData): number | null {
  const spur = getPath(data, "transmission.spur_teeth");
  const bell = getPath(data, "transmission.clutch_bell_teeth");
  return gearRatio(typeof spur === "number" ? spur : undefined, typeof bell === "number" ? bell : undefined);
}

export function formatGearRatio(ratio: number | null, locale = "pt-PT"): string {
  if (ratio === null) return "—";
  return new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(ratio);
}
