import type { Difference } from "./models";
import { templateFields, type Template } from "./template";
import { computeCanonical } from "./values";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Leaf differences between two setup versions, ordered by path. A port of the server's
 * `Compare` (`backend/internal/setup/domain/compare.go`); both pass the same vectors.
 * It works on the JSON shape, so unknown keys compare too.
 */
export function compareSetupData(a: Record<string, unknown>, b: Record<string, unknown>): Difference[] {
  const out: Difference[] = [];
  compareMaps("", a, b, out);
  return out.sort((x, y) => (x.path < y.path ? -1 : x.path > y.path ? 1 : 0));
}

function join(parent: string, key: string): string {
  return parent === "" ? key : `${parent}.${key}`;
}

function compareMaps(
  path: string,
  a: Record<string, unknown>,
  b: Record<string, unknown>,
  out: Difference[],
): void {
  const keys = [...new Set([...Object.keys(a), ...Object.keys(b)])].sort();
  for (const key of keys) {
    const p = join(path, key);
    const inA = key in a;
    const inB = key in b;
    if (!inA) addLeaves(p, b[key], "added", out);
    else if (!inB) addLeaves(p, a[key], "removed", out);
    else compareValues(p, a[key], b[key], out);
  }
}

function compareValues(path: string, a: unknown, b: unknown, out: Difference[]): void {
  if (isRecord(a) && isRecord(b)) return compareMaps(path, a, b, out);
  if (Array.isArray(a) && Array.isArray(b)) return compareArrays(path, a, b, out);
  if (a !== b) out.push({ path, kind: "changed", before: a, after: b });
}

function compareArrays(path: string, a: unknown[], b: unknown[], out: Difference[]): void {
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const p = `${path}[${i}]`;
    if (i >= a.length) addLeaves(p, b[i], "added", out);
    else if (i >= b.length) addLeaves(p, a[i], "removed", out);
    else compareValues(p, a[i], b[i], out);
  }
}

function addLeaves(path: string, value: unknown, kind: "added" | "removed", out: Difference[]): void {
  if (isRecord(value)) {
    for (const key of Object.keys(value).sort()) addLeaves(join(path, key), value[key], kind, out);
  } else if (Array.isArray(value)) {
    value.forEach((child, i) => addLeaves(`${path}[${i}]`, child, kind, out));
  } else {
    out.push(kind === "added" ? { path, kind, after: value } : { path, kind, before: value });
  }
}

/** Groups differences by their top-level group (`shocks.front.oil_cst` → `shocks.front`). */
export function groupOf(path: string): string {
  const parts = path.split(".");
  return parts.length > 2 && parts[0] === "shocks" ? `shocks.${parts[1] ?? ""}` : (parts[0] ?? path);
}

/** What `compareVersions` needs of a setup version: its template and filled values. */
export interface VersionView {
  template: Template;
  values: Record<string, unknown>;
  refs: Record<string, unknown>;
}

export interface CompareResult {
  differences: Difference[];
  /** Keys that had no counterpart when two different templates were compared. */
  skipped: string[];
}

/**
 * Compares two versions. Same template id: by key, whatever the version number. Different
 * templates: by canonical path only, and `skipped` says what could not be compared. A port of
 * Go's `CompareVersions`; both pass `testdata/compare/v2.json`.
 */
export function compareVersions(a: VersionView, b: VersionView): CompareResult {
  let left: Record<string, unknown>;
  let right: Record<string, unknown>;
  const skipped = new Set<string>();
  const multiDiffs: Difference[] = [];
  if (a.template.id === b.template.id) {
    // A multi value is one value (the set of ticked options), not a list compared by position.
    const multi = new Set(
      [a, b].flatMap((v) => templateFields(v.template)).filter((f) => f.type === "multi").map((f) => f.key),
    );
    const without = (values: Record<string, unknown>) => Object.fromEntries(Object.entries(values).filter(([k]) => !multi.has(k)));
    left = without(a.values);
    right = without(b.values);
    for (const key of multi) {
      const before = a.values[key];
      const after = b.values[key];
      if (before !== undefined && after !== undefined) {
        if (JSON.stringify(before) !== JSON.stringify(after)) multiDiffs.push({ path: key, kind: "changed", before, after });
      } else if (after !== undefined) multiDiffs.push({ path: key, kind: "added", after });
      else if (before !== undefined) multiDiffs.push({ path: key, kind: "removed", before });
    }
  } else {
    left = computeCanonical(a.template, a.values);
    right = computeCanonical(b.template, b.values);
    for (const v of [a, b]) {
      const canon = new Set(templateFields(v.template).filter((f) => f.canonical !== undefined).map((f) => f.key));
      for (const k of Object.keys(v.values)) if (!canon.has(k)) skipped.add(k);
    }
  }
  const differences = [
    ...compareSetupData(left, right),
    ...multiDiffs,
    ...compareSetupData(a.refs, b.refs).map((d) => ({ ...d, path: `refs.${d.path}` })),
  ].sort((x, y) => (x.path < y.path ? -1 : x.path > y.path ? 1 : 0));
  return { differences, skipped: [...skipped].sort() };
}
