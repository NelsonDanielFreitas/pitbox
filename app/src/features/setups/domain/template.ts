import { z } from "zod";

import { MAX_DATA_BYTES, MAX_DATA_DEPTH } from "./models";
import { ruleIds as v1Rules } from "./validate";

// Sheet templates (ADR 0030). A port of backend/internal/setup/domain/{template,data_v2}.go:
// both sides pass the same vectors (backend/internal/setup/domain/testdata/template/).
// Errors carry a path and a rule id, never the value.

export const templateRules = {
  ...v1Rules,
  templateUnknown: "setup.data.template_unknown",
  optionUnknown: "setup.data.option_unknown",
  step: "setup.data.step",
  templateInvalid: "setup.template.invalid",
  templateLimits: "setup.template.limits",
  keyDuplicate: "setup.template.key_duplicate",
  tableLayout: "setup.template.table_layout",
} as const;

export const templateLimits = {
  groups: 40,
  fields: 400,
  options: 20,
  minOptions: 2,
  optionLen: 40,
  labelLen: 80,
  helpLen: 300,
  partRefLen: 40,
  textLen: 500,
  templateBytes: 256 << 10,
  tableColumns: { min: 2, max: 4 },
  tableRows: 30,
} as const;

export const SCHEMA_VERSION_V2 = 2;

const keyPattern = /^[a-z][a-z0-9_]{1,62}$/;
const MAX_INTEGER_MAGNITUDE = 2 ** 53;

const runeLen = (s: string): number => [...s].length;
// Control characters (Cc). Newline and tab are allowed where text may wrap.
const hasControl = (s: string, allowBreaks: boolean): boolean =>
  [...s].some((c) => /\p{Cc}/u.test(c) && !(allowBreaks && (c === "\n" || c === "\t")));
const plainText = (s: string, min: number, max: number): boolean => {
  const n = runeLen(s);
  return n >= min && n <= max && s.trim() !== "" && !hasControl(s, false);
};

const localText = (max: number) =>
  z.strictObject({
    pt: z.string().refine((s) => plainText(s, 1, max)),
    en: z.string().refine((s) => plainText(s, 1, max)).optional(),
  });

const helpText = z.strictObject({
  pt: z.string().refine((s) => s.trim() !== "" && runeLen(s) <= templateLimits.helpLen && !hasControl(s, true)),
  en: z.string().refine((s) => runeLen(s) <= templateLimits.helpLen && !hasControl(s, true)).optional(),
});

const diagramSchema = z.strictObject({
  kind: z.enum(["hole_row", "hole_grid", "angle", "length", "part"]),
  count: z.number().int().optional(),
  rows: z.number().int().optional(),
  cols: z.number().int().optional(),
  orientation: z.enum(["horizontal", "vertical"]).optional(),
  axis: z.enum(["camber", "caster", "toe", "kickup", "anti_squat"]).optional(),
  measure: z.enum(["ride_height", "droop", "shock_length", "limiter"]).optional(),
  shape: z.enum(["disk", "brace", "spacer", "weight", "other"]).optional(),
});
export type Diagram = z.infer<typeof diagramSchema>;

const finite = z.number().refine((n) => Number.isFinite(n) && Math.abs(n) <= MAX_INTEGER_MAGNITUDE);

const fieldSchema = z.strictObject({
  key: z.string().regex(keyPattern),
  label: localText(templateLimits.labelLen),
  type: z.enum(["number", "int", "choice", "flag", "multi", "text"]),
  unit: z.string().refine((s) => plainText(s, 1, 20)).optional(),
  min: finite.optional(),
  max: finite.optional(),
  step: finite.optional(),
  options: z.array(z.string().refine((s) => plainText(s, 1, templateLimits.optionLen))).max(templateLimits.options).optional(),
  max_len: z.number().int().min(1).max(templateLimits.textLen).optional(),
  part_ref: z.string().refine((s) => plainText(s, 1, templateLimits.partRefLen)).optional(),
  kit: z.union([z.number(), z.string(), z.boolean(), z.array(z.string())]).optional(),
  canonical: z.string().max(100).optional(),
  help: helpText.optional(),
  diagram: diagramSchema.optional(),
});
export type TemplateField = z.infer<typeof fieldSchema>;
export type FieldType = TemplateField["type"];

// A presentation hint: the fields of a group in a grid (front/rear...). Cells hold field keys,
// "" is an empty cell. It never carries values.
const tableSchema = z.strictObject({
  columns: z.array(localText(templateLimits.labelLen)),
  rows: z.array(z.strictObject({ label: localText(templateLimits.labelLen), cells: z.array(z.string()) })),
});
export type GroupTable = z.infer<typeof tableSchema>;

const groupSchema = z.strictObject({
  key: z.string().regex(keyPattern),
  label: localText(templateLimits.labelLen),
  help: helpText.optional(),
  table: tableSchema.optional(),
  fields: z.array(fieldSchema).min(1),
});
export type TemplateGroup = z.infer<typeof groupSchema>;

const templateSchema = z.strictObject({
  format: z.literal(1),
  id: z.uuid(),
  slug: z.string().regex(/^[a-z0-9][a-z0-9-]{1,62}$/),
  version: z.number().int().min(1),
  make: z.string().refine((s) => plainText(s, 1, 80)),
  model: z.string().refine((s) => plainText(s, 1, 80)),
  sheet_revision: z.string().refine((s) => plainText(s, 1, 40)).optional(),
  source_url: z.string().max(500).startsWith("https://").optional(),
  scale: z.string().refine((s) => plainText(s, 1, 20)),
  powertrain: z.string().refine((s) => plainText(s, 1, 20)),
  name: localText(120),
  groups: z.array(groupSchema).min(1).max(templateLimits.groups),
});
export type Template = z.infer<typeof templateSchema>;

export interface TemplateIssue {
  path: string;
  ruleId: string;
}

function diagramOk(f: TemplateField): boolean {
  const d = f.diagram;
  if (!d) return true;
  const only = (...allowed: (keyof Diagram)[]): boolean =>
    (Object.keys(d) as (keyof Diagram)[]).every((k) => k === "kind" || allowed.includes(k));
  const n = f.options?.length ?? 0;
  switch (d.kind) {
    case "hole_row":
      return f.type === "choice" && only("count", "orientation") && (d.count ?? 0) >= 2 && (d.count ?? 0) <= 8 && d.count === n;
    case "hole_grid":
      return (
        f.type === "choice" && only("rows", "cols") && (d.rows ?? 0) >= 1 && (d.rows ?? 0) <= 4 &&
        (d.cols ?? 0) >= 2 && (d.cols ?? 0) <= 6 && (d.rows ?? 0) * (d.cols ?? 0) === n
      );
    case "angle":
      return f.type === "number" && only("axis") && d.axis !== undefined;
    case "length":
      return (f.type === "number" || f.type === "int") && only("measure") && d.measure !== undefined;
    case "part":
      return f.type === "flag" && only("shape") && d.shape !== undefined;
  }
}

function fieldAttributesOk(f: TemplateField): boolean {
  const numeric = f.type === "number" || f.type === "int";
  if (!numeric && (f.unit !== undefined || f.min !== undefined || f.max !== undefined)) return false;
  if (f.type !== "number" && f.step !== undefined) return false;
  if (f.type !== "choice" && f.type !== "multi" && f.options !== undefined) return false;
  if (f.type !== "text" && f.max_len !== undefined) return false;
  if (f.part_ref !== undefined && (f.type === "int" || f.type === "text" || f.type === "multi")) return false;
  if (numeric) {
    if (f.min === undefined || f.max === undefined || f.min > f.max) return false;
    if (f.type === "int" && (!Number.isInteger(f.min) || !Number.isInteger(f.max))) return false;
    if (f.step !== undefined && (f.step <= 0 || (f.max - f.min) / f.step > 1e6)) return false;
  }
  if (f.type === "choice" || f.type === "multi") {
    const o = f.options ?? [];
    if (o.length < templateLimits.minOptions || new Set(o).size !== o.length) return false;
  }
  if (f.type === "text" && f.max_len === undefined) return false;
  return true;
}

/** Go's `validateTable`: every field of the group sits in exactly one cell, none of another group. */
function tableOk(g: TemplateGroup): boolean {
  const tb = g.table;
  if (tb === undefined) return true;
  const { tableColumns, tableRows } = templateLimits;
  if (tb.columns.length < tableColumns.min || tb.columns.length > tableColumns.max) return false;
  if (tb.rows.length < 1 || tb.rows.length > tableRows) return false;
  const inGroup = new Set(g.fields.map((f) => f.key));
  const used = new Set<string>();
  for (const row of tb.rows) {
    if (row.cells.length !== tb.columns.length) return false;
    for (const key of row.cells) {
      if (key === "") continue;
      if (!inGroup.has(key) || used.has(key)) return false;
      used.add(key);
    }
  }
  return used.size === inGroup.size;
}

/**
 * Parses a template definition coming from the server or the local database. The server has
 * already validated it; this is the app's own check at the boundary (the local copy could be
 * damaged), with the same limits. Returns null for anything that does not hold.
 */
export function parseTemplate(raw: unknown): Template | null {
  const parsed = templateSchema.safeParse(raw);
  if (!parsed.success) return null;
  const t = parsed.data;
  const groupKeys = new Set<string>();
  const fieldKeys = new Set<string>();
  const canonical = new Set<string>();
  let total = 0;
  for (const g of t.groups) {
    if (groupKeys.has(g.key)) return null;
    groupKeys.add(g.key);
    total += g.fields.length;
    if (total > templateLimits.fields) return null;
    if (!tableOk(g)) return null;
    for (const f of g.fields) {
      if (fieldKeys.has(f.key)) return null;
      fieldKeys.add(f.key);
      if (!fieldAttributesOk(f) || !diagramOk(f)) return null;
      if (f.kit !== undefined && validateFieldValue(f, f.kit) !== null) return null;
      if (f.canonical !== undefined) {
        if (canonical.has(f.canonical)) return null;
        canonical.add(f.canonical);
      }
    }
  }
  return t;
}

export function templateFields(t: Template): TemplateField[] {
  return t.groups.flatMap((g) => g.fields);
}

type MultiResult = { ok: true; value: string[] } | { ok: false; rule: string };

/**
 * Go's `normalizeMulti`: a non-empty list of distinct options of the field, returned in the order
 * of the options, so two devices that tick the same boxes in another order store the same value.
 */
export function normalizeMulti(f: TemplateField, v: unknown): MultiResult {
  const options = f.options ?? [];
  if (!Array.isArray(v)) return { ok: false, rule: templateRules.type };
  if (v.length === 0 || v.length > options.length) return { ok: false, rule: templateRules.length };
  const chosen = new Set<string>();
  for (const item of v) {
    if (typeof item !== "string") return { ok: false, rule: templateRules.type };
    if (chosen.has(item)) return { ok: false, rule: templateRules.optionUnknown };
    chosen.add(item);
  }
  const value = options.filter((o) => chosen.has(o));
  if (value.length !== chosen.size) return { ok: false, rule: templateRules.optionUnknown };
  return { ok: true, value };
}

/** Validates one value against its field; returns the rule id, or null when it is valid. */
export function validateFieldValue(f: TemplateField, v: unknown): string | null {
  switch (f.type) {
    case "int":
    case "number": {
      if (typeof v !== "number" || !Number.isFinite(v) || (f.type === "int" && !Number.isInteger(v))) {
        return templateRules.type;
      }
      if (f.min === undefined || f.max === undefined || v < f.min || v > f.max) return templateRules.range;
      if (f.step !== undefined) {
        const q = (v - f.min) / f.step;
        if (Math.abs(q - Math.round(q)) > 1e-9) return templateRules.step;
      }
      return null;
    }
    case "choice":
      if (typeof v !== "string") return templateRules.type;
      return f.options?.includes(v) ? null : templateRules.optionUnknown;
    case "multi": {
      const multi = normalizeMulti(f, v);
      return multi.ok ? null : multi.rule;
    }
    case "flag":
      return typeof v === "boolean" ? null : templateRules.type;
    case "text":
      if (typeof v !== "string" || hasControl(v, true)) return templateRules.type;
      return runeLen(v) > (f.max_len ?? 0) ? templateRules.length : null;
  }
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

/**
 * A Zod schema generated from a template: the values of a sheet. Each issue carries the rule
 * id as its message and the field key as its path (the unknown key is never echoed).
 */
export function templateToZod(t: Template): z.ZodType<Record<string, unknown>> {
  const index = new Map(templateFields(t).map((f) => [f.key, f]));
  return z.record(z.string(), z.unknown()).superRefine((values, ctx) => {
    for (const key of Object.keys(values).sort()) {
      const f = index.get(key);
      if (!f) {
        ctx.addIssue({ code: "custom", message: templateRules.unknown, path: [] });
        continue;
      }
      const rule = validateFieldValue(f, values[key]);
      if (rule !== null) ctx.addIssue({ code: "custom", message: rule, path: [key] });
    }
  });
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

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const NIL_UUID = "00000000-0000-0000-0000-000000000000";
const refKeys = new Set(["engine_id", "tyre_set_id"]);

export type V2Result = { ok: true; data: Record<string, unknown> } | { ok: false; issue: TemplateIssue };

const fail = (path: string, ruleId: string): V2Result => ({ ok: false, issue: { path, ruleId } });

/**
 * Checks setup data against one template version: only the keys template, values and refs
 * (canonical is computed by the server). Same result as Go's `ValidateV2`.
 */
export function validateSetupV2(t: Template, data: Record<string, unknown>): V2Result {
  if (exceedsDepth(data, MAX_DATA_DEPTH)) return fail("data", templateRules.depth);
  if (new TextEncoder().encode(JSON.stringify(data)).length > MAX_DATA_BYTES) return fail("data", templateRules.size);
  for (const k of Object.keys(data).sort()) {
    if (k !== "template" && k !== "values" && k !== "refs") return fail("data", templateRules.unknown);
  }
  const ref = data.template;
  if (!isRecord(ref)) return fail("template", templateRules.templateUnknown);
  for (const k of Object.keys(ref).sort()) {
    if (k !== "id" && k !== "version") return fail("template", templateRules.unknown);
  }
  if (ref.id !== t.id || ref.version !== t.version) return fail("template", templateRules.templateUnknown);

  const values: Record<string, unknown> = {};
  if ("values" in data) {
    const input = data.values;
    if (!isRecord(input)) return fail("values", templateRules.type);
    const index = new Map(templateFields(t).map((f) => [f.key, f]));
    for (const k of Object.keys(input).sort()) {
      const f = index.get(k);
      if (!f) return fail("values", templateRules.unknown);
      const rule = validateFieldValue(f, input[k]);
      if (rule !== null) return fail(`values.${k}`, rule);
      const multi = f.type === "multi" ? normalizeMulti(f, input[k]) : undefined;
      values[k] = multi?.ok ? multi.value : input[k];
    }
  }
  const out: Record<string, unknown> = { template: { id: t.id, version: t.version }, values };
  if ("refs" in data) {
    const input = data.refs;
    if (!isRecord(input)) return fail("refs", templateRules.type);
    const refs: Record<string, unknown> = {};
    for (const k of Object.keys(input).sort()) {
      if (!refKeys.has(k)) return fail("refs", templateRules.unknown);
      const s = input[k];
      if (typeof s !== "string") return fail(`refs.${k}`, templateRules.type);
      if (!UUID_RE.test(s) || s === NIL_UUID) return fail(`refs.${k}`, templateRules.uuid);
      refs[k] = s;
    }
    if (Object.keys(refs).length > 0) out.refs = refs;
  }
  return { ok: true, data: out };
}

/**
 * Copies a template for a team under a new id (Go's `DuplicateTemplate`): the slug is derived
 * from the source slug and the LAST 8 hex digits of the new id, and the version restarts at 1.
 * The phone computes the same slug offline, so a duplicate made without network equals the one
 * the server stores.
 */
export function duplicateTemplate(src: Template, newId: string): Template | null {
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(newId)) return null;
  let base = src.slug;
  if (base.length > 54) base = base.slice(0, 54).replace(/-+$/, "");
  return parseTemplate({ ...src, id: newId, slug: `${base}-${newId.replaceAll("-", "").slice(24)}`, version: 1 });
}

/** The template version a v2 version names, or undefined if the data does not name one. */
export function templateRef(data: Record<string, unknown>): { id: string; version: number } | undefined {
  const ref = data.template;
  if (typeof ref !== "object" || ref === null) return undefined;
  const id: unknown = "id" in ref ? ref.id : undefined;
  const version: unknown = "version" in ref ? ref.version : undefined;
  return typeof id === "string" && typeof version === "number" ? { id, version } : undefined;
}
