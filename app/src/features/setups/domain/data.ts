import type { FieldDef } from "./catalogue";
import type { SetupData } from "./models";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function getPath(data: SetupData, path: string): unknown {
  let current: unknown = data;
  for (const key of path.split(".")) {
    if (!isRecord(current)) return undefined;
    current = current[key];
  }
  return current;
}

/**
 * Returns a copy of `data` with `path` set, or removed when `value` is `undefined`.
 * Groups left empty are dropped (the server normalises them away too), values are never `null`,
 * and every key outside `path` is kept as it was, including keys this app does not know.
 */
export function setPath(data: SetupData, path: string, value: unknown): SetupData {
  return setIn(data, path.split("."), value);
}

function setIn(node: Record<string, unknown>, keys: readonly string[], value: unknown): Record<string, unknown> {
  const [head, ...rest] = keys;
  if (head === undefined) return node;
  const next: Record<string, unknown> = { ...node };
  if (rest.length === 0) {
    if (value === undefined) delete next[head];
    else next[head] = value;
    return next;
  }
  const child = node[head];
  const updated = setIn(isRecord(child) ? child : {}, rest, value);
  if (Object.keys(updated).length === 0) delete next[head];
  else next[head] = updated;
  return next;
}

/** Drops empty groups, like the server's canonical form. Pure: the input is not modified. */
export function normalizeData(data: SetupData): SetupData {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(data)) {
    if (isRecord(value)) {
      const child = normalizeData(value);
      if (Object.keys(child).length > 0) out[key] = child;
    } else if (Array.isArray(value)) {
      const items: unknown[] = value;
      out[key] = items.map((item) => (isRecord(item) ? normalizeData(item) : item));
    } else {
      out[key] = value;
    }
  }
  return out;
}

export type FieldParse =
  | { ok: true; value: number | string | boolean | undefined }
  | { ok: false; ruleId: "setup.data.type" };

const NUMBER_RE = /^[+-]?\d+(?:[.,]\d+)?$/;

/**
 * Turns what the user typed into a value for the data. Empty text means "omit the key";
 * decimals accept a comma or a dot. Range and length are checked by `validateSetupData`.
 */
export function parseFieldText(field: FieldDef, text: string): FieldParse {
  const trimmed = text.trim();
  if (trimmed === "") return { ok: true, value: undefined };
  switch (field.kind) {
    case "int":
    case "number": {
      if (!NUMBER_RE.test(trimmed)) return { ok: false, ruleId: "setup.data.type" };
      const value = Number(trimmed.replace(",", "."));
      if (!Number.isFinite(value)) return { ok: false, ruleId: "setup.data.type" };
      if (field.kind === "int" && !Number.isInteger(value)) return { ok: false, ruleId: "setup.data.type" };
      // "-0" is typed as zero, never as negative zero.
      return { ok: true, value: value === 0 ? 0 : value };
    }
    case "string":
    case "uuid":
      return { ok: true, value: trimmed };
    case "bool":
      return { ok: true, value: trimmed === "true" };
  }
}

/** The text to show in an input for a stored value (decimal comma). */
export function fieldToText(value: unknown): string {
  if (typeof value === "number") return String(value).replace(".", ",");
  if (typeof value === "string") return value;
  return "";
}
