import type { PartKind, PartSpecs } from "./models";

/** Longest text value allowed in a part spec (ADR 0021). */
export const MAX_SPEC_TEXT = 40;

export type SpecKey =
  | "viscosity_cst"
  | "nitro_pct"
  | "oil_pct"
  | "heat_range"
  | "rate"
  | "compound"
  | "insert_type";

export interface SpecField {
  key: SpecKey;
  type: "number" | "text";
  /** Unit shown next to the field (cSt, %); the value is stored without it. */
  unit?: string;
  min?: number;
  max?: number;
  /** For numbers: the minimum itself is not allowed. */
  exclusiveMin?: boolean;
}

const oil: SpecField[] = [
  { key: "viscosity_cst", type: "number", unit: "cSt", min: 0, max: 100_000, exclusiveMin: true },
];

/**
 * Allowed spec keys per part kind. This mirrors the backend (ADR 0021 and 0022): the
 * server refuses unknown keys, so the app must never send one.
 */
const fields: Partial<Record<PartKind, SpecField[]>> = {
  shock_oil: oil,
  diff_oil: oil,
  fuel: [
    { key: "nitro_pct", type: "number", unit: "%", min: 0, max: 40 },
    { key: "oil_pct", type: "number", unit: "%", min: 0, max: 30 },
  ],
  glow_plug: [{ key: "heat_range", type: "text" }],
  spring: [{ key: "rate", type: "text" }],
  tyre: [{ key: "compound", type: "text" }],
  insert: [{ key: "insert_type", type: "text" }],
};

export function specFieldsFor(kind: PartKind): readonly SpecField[] {
  return fields[kind] ?? [];
}

export type SpecError = { key: SpecKey };

/**
 * Turns the text typed in the spec inputs into the specs object stored with the part.
 * Empty inputs are dropped; a value outside the rules returns the offending key.
 */
export function parseSpecInputs(
  kind: PartKind,
  raw: Readonly<Record<string, string>>,
): { specs: PartSpecs; error?: undefined } | { specs?: undefined; error: SpecError } {
  const specs: PartSpecs = {};
  for (const field of specFieldsFor(kind)) {
    const text = (raw[field.key] ?? "").trim();
    if (text === "") continue;
    if (field.type === "text") {
      if ([...text].length > MAX_SPEC_TEXT) return { error: { key: field.key } };
      specs[field.key] = text;
      continue;
    }
    if (!/^\d+([.,]\d+)?$/.test(text)) return { error: { key: field.key } };
    const value = Number(text.replace(",", "."));
    const tooLow =
      field.min !== undefined && (field.exclusiveMin ? value <= field.min : value < field.min);
    if (!Number.isFinite(value) || tooLow || (field.max !== undefined && value > field.max)) {
      return { error: { key: field.key } };
    }
    specs[field.key] = value;
  }
  return { specs };
}

/** The text shown in the spec inputs for the stored specs of a part. */
export function specInputsFrom(kind: PartKind, specs: PartSpecs): Record<string, string> {
  const out: Record<string, string> = {};
  for (const field of specFieldsFor(kind)) {
    const value = specs[field.key];
    if (value !== undefined) out[field.key] = String(value).replace(".", ",");
  }
  return out;
}

/** Keeps only the keys the kind allows (used when the kind changes in the form). */
export function pruneSpecs(kind: PartKind, specs: PartSpecs): PartSpecs {
  const allowed = new Set<string>(specFieldsFor(kind).map((f) => f.key));
  return Object.fromEntries(Object.entries(specs).filter(([key]) => allowed.has(key)));
}
