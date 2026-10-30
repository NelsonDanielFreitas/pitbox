import { setupFields, type FieldDef } from "./catalogue";
import { MAX_DATA_BYTES, MAX_DATA_DEPTH, SCHEMA_VERSION, type SetupData } from "./models";

/** Same rule ids as `backend/internal/setup/domain/errors.go`. */
export const ruleIds = {
  schemaVersion: "setup.schema_version",
  size: "setup.data.size",
  depth: "setup.data.depth",
  unknown: "setup.data.unknown",
  type: "setup.data.type",
  range: "setup.data.range",
  length: "setup.data.length",
  uuid: "setup.data.uuid",
  barsConflict: "setup.data.bars_conflict",
} as const;
export type RuleId = (typeof ruleIds)[keyof typeof ruleIds];

/** A violation: where and which rule, never the rejected value. */
export interface SetupIssue {
  path: string;
  ruleId: RuleId;
}

const fieldsByPath: ReadonlyMap<string, FieldDef> = new Map(setupFields.map((f) => [f.path, f]));
const groupPaths: ReadonlySet<string> = new Set(
  setupFields.flatMap((f) => {
    const parts = f.path.split(".").slice(0, -1);
    return parts.map((_, i) => parts.slice(0, i + 1).join("."));
  }),
);

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const NIL_UUID = "00000000-0000-0000-0000-000000000000";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function exceedsDepth(root: unknown, max: number): boolean {
  const stack: [unknown, number][] = [[root, 1]];
  for (let item = stack.pop(); item !== undefined; item = stack.pop()) {
    const [value, depth] = item;
    if (isRecord(value) || Array.isArray(value)) {
      if (depth > max) return true;
      for (const child of Object.values(value)) stack.push([child, depth + 1]);
    }
  }
  return false;
}

/**
 * Checks `data` against schema v1 with the same rules as the server. It returns every issue
 * (fields in key order, then bars), except for version, depth and size, which stop at the first.
 * Empty array = valid. Pure: `data` is not modified.
 */
export function validateSetupData(data: SetupData, schemaVersion: number = SCHEMA_VERSION): SetupIssue[] {
  if (schemaVersion !== SCHEMA_VERSION) return [{ path: "schema_version", ruleId: ruleIds.schemaVersion }];
  if (exceedsDepth(data, MAX_DATA_DEPTH)) return [{ path: "data", ruleId: ruleIds.depth }];
  let raw: string;
  try {
    raw = JSON.stringify(data);
  } catch {
    return [{ path: "data", ruleId: ruleIds.type }];
  }
  if (new TextEncoder().encode(raw).length > MAX_DATA_BYTES) return [{ path: "data", ruleId: ruleIds.size }];
  const issues: SetupIssue[] = [];
  walk("", data, issues);
  issues.push(...barsIssues(data));
  return issues;
}

function walk(prefix: string, node: Record<string, unknown>, issues: SetupIssue[]): void {
  for (const key of Object.keys(node).sort()) {
    const path = prefix === "" ? key : `${prefix}.${key}`;
    const value = node[key];
    if (groupPaths.has(path)) {
      if (!isRecord(value)) issues.push({ path, ruleId: ruleIds.type });
      else walk(path, value, issues);
      continue;
    }
    const field = fieldsByPath.get(path);
    if (!field) {
      issues.push({ path, ruleId: ruleIds.unknown });
      continue;
    }
    const issue = checkField(field, value);
    if (issue) issues.push({ path, ruleId: issue });
  }
}

function checkField(field: FieldDef, value: unknown): RuleId | null {
  if (value === null || value === undefined) return ruleIds.type;
  switch (field.kind) {
    case "int":
    case "number": {
      if (typeof value !== "number" || !Number.isFinite(value)) return ruleIds.type;
      if (field.kind === "int" && !Number.isInteger(value)) return ruleIds.type;
      if ((field.min !== undefined && value < field.min) || (field.max !== undefined && value > field.max)) {
        return ruleIds.range;
      }
      return null;
    }
    case "string":
      if (typeof value !== "string") return ruleIds.type;
      // Count code points, like Go's utf8.RuneCountInString.
      return field.maxLen !== undefined && [...value].length > field.maxLen ? ruleIds.length : null;
    case "bool":
      return typeof value === "boolean" ? null : ruleIds.type;
    case "uuid":
      if (typeof value !== "string") return ruleIds.type;
      return UUID_RE.test(value) && value !== NIL_UUID ? null : ruleIds.uuid;
  }
}

function barsIssues(data: SetupData): SetupIssue[] {
  const bars = data["bars"];
  if (!isRecord(bars)) return [];
  const issues: SetupIssue[] = [];
  for (const side of ["front", "rear"] as const) {
    if (bars[`${side}_removed`] === true && `${side}_mm` in bars) {
      issues.push({ path: `bars.${side}_mm`, ruleId: ruleIds.barsConflict });
    }
  }
  return issues;
}
