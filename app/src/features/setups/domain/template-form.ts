import { normalizeMulti, templateFields, validateFieldValue, type Template, type TemplateField } from "./template";

// The text a person types, turned into the values of a template. Pure, so it is tested without
// a screen. Empty text means "omit the key"; decimals accept a comma or a dot.

export type TemplateTexts = Record<string, string>;

const NUMBER_RE = /^[+-]?\d+(?:[.,]\d+)?$/;

export function valueToText(value: unknown): string {
  if (typeof value === "number") return String(value).replace(".", ",");
  if (typeof value === "string") return value;
  if (typeof value === "boolean") return value ? "true" : "false";
  if (Array.isArray(value)) return JSON.stringify(value); // multi: the ticked options
  return "";
}

export function initialTemplateTexts(values: Record<string, unknown>): TemplateTexts {
  return Object.fromEntries(Object.entries(values).map(([k, v]) => [k, valueToText(v)]));
}

function parseText(field: TemplateField, text: string): { ok: true; value: unknown } | { ok: false } {
  const trimmed = field.type === "text" ? text : text.trim();
  if (trimmed.trim() === "") return { ok: true, value: undefined };
  switch (field.type) {
    case "number":
    case "int": {
      if (!NUMBER_RE.test(trimmed)) return { ok: false };
      const value = Number(trimmed.replace(",", "."));
      return Number.isFinite(value) ? { ok: true, value: value === 0 ? 0 : value } : { ok: false };
    }
    case "flag":
      return trimmed === "true" || trimmed === "false" ? { ok: true, value: trimmed === "true" } : { ok: false };
    case "multi": {
      try {
        const list: unknown = JSON.parse(trimmed);
        return Array.isArray(list) ? { ok: true, value: list.length === 0 ? undefined : list } : { ok: false };
      } catch {
        return { ok: false };
      }
    }
    case "choice":
    case "text":
      return { ok: true, value: trimmed };
  }
}

export interface TemplateFormResult {
  values: Record<string, unknown>;
  /** Field key → rule id (never the value). */
  issues: Record<string, string>;
}

export function applyTemplateTexts(template: Template, texts: TemplateTexts): TemplateFormResult {
  const values: Record<string, unknown> = {};
  const issues: Record<string, string> = {};
  for (const field of templateFields(template)) {
    const text = texts[field.key];
    if (text === undefined) continue;
    const parsed = parseText(field, text);
    if (!parsed.ok) {
      issues[field.key] = "setup.data.type";
      continue;
    }
    if (parsed.value === undefined) continue;
    const rule = validateFieldValue(field, parsed.value);
    if (rule !== null) issues[field.key] = rule;
    else values[field.key] = field.type === "multi" ? normalizedMulti(field, parsed.value) : parsed.value;
  }
  return { values, issues };
}

/** The ticked options of a multi field, from the text the form keeps (a JSON list; "" = none). */
export function multiFromText(text: string): string[] {
  try {
    const list: unknown = JSON.parse(text);
    return Array.isArray(list) ? list.filter((item): item is string => typeof item === "string") : [];
  } catch {
    return [];
  }
}

/** Ticks or unticks one option; the result is in the order of the options ("" when none is left). */
export function toggleMultiText(field: TemplateField, text: string, option: string): string {
  const current = new Set(multiFromText(text));
  if (current.has(option)) current.delete(option);
  else current.add(option);
  const next = (field.options ?? []).filter((o) => current.has(o));
  return next.length === 0 ? "" : JSON.stringify(next);
}

function normalizedMulti(field: TemplateField, value: unknown): unknown {
  const result = normalizeMulti(field, value);
  return result.ok ? result.value : value;
}

/** Keys whose value differs between two value maps (the "changes" counter). */
export function changedKeys(before: Record<string, unknown>, after: Record<string, unknown>): string[] {
  const keys = new Set([...Object.keys(before), ...Object.keys(after)]);
  // Arrays (multi) are compared by content: they are normalised, so equal ticks have equal JSON.
  const same = (a: unknown, b: unknown): boolean =>
    Array.isArray(a) && Array.isArray(b) ? JSON.stringify(a) === JSON.stringify(b) : a === b;
  return [...keys].filter((k) => !same(before[k], after[k])).sort();
}
