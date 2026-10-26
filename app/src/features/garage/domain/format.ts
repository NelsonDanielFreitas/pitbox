import type { PartUnit } from "./models";

export function unitLabel(unit: PartUnit): string {
  return unit === "l" ? "L" : unit;
}

export function parseDecimalToCenti(value: string): number {
  return Math.round(Number(value.replace(",", ".")) * 100);
}

export function centiToInput(centi: number, unit: PartUnit): string {
  if (unit === "un") return String(Math.trunc(centi / 100));
  const value = (centi / 100).toFixed(2).replace(/\.?0+$/, "");
  return value.replace(".", ",");
}

export function formatQuantity(
  centi: number,
  unit: PartUnit,
  locale = "pt-PT",
): string {
  const value = centi / 100;
  const formatted = new Intl.NumberFormat(locale, {
    maximumFractionDigits: unit === "un" ? 0 : 2,
  }).format(value);
  return `${formatted} ${unitLabel(unit)}`;
}

export function formatDate(value: string | null, locale = "pt-PT"): string {
  if (!value) return "—";
  const date = new Date(value.length === 10 ? `${value}T00:00:00Z` : value);
  if (!Number.isFinite(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
}

export function formatDateTime(value: string, locale = "pt-PT"): string {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}
