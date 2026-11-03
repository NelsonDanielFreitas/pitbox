// @vitest-environment node

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { compareVersions } from "./compare";
import { parseTemplate, templateToZod, validateSetupV2, type Template } from "./template";
import { v1ToValues } from "./values";

const domainDir = "../../../../../backend/internal/setup/";
const read = (rel: string): unknown => JSON.parse(readFileSync(fileURLToPath(new URL(domainDir + rel, import.meta.url)), "utf8"));

interface ValidateVector {
  name: string;
  template: string;
  data: Record<string, unknown>;
  ok?: boolean;
  path?: string;
  rule?: string;
  out?: Record<string, unknown>;
}
interface V1Vector {
  name: string;
  data: Record<string, unknown>;
  values: Record<string, unknown>;
  refs: Record<string, unknown>;
}
const file = read("domain/testdata/template/vectors.json") as {
  templates: Record<string, unknown>;
  validate: ValidateVector[];
  v1_to_values: V1Vector[];
};
const generic = parseTemplate(read("templates/generic-1-8-nitro.v1.json"));

function templates(raw: Record<string, unknown>): Record<string, Template> {
  const out: Record<string, Template> = {};
  for (const [name, value] of Object.entries(raw)) {
    const t = parseTemplate(value);
    if (!t) throw new Error(`template ${name} does not parse`);
    out[name] = t;
  }
  return out;
}
const tpls = templates(file.templates);
const pick = (name: string): Template => {
  const t = tpls[name];
  if (!t) throw new Error(name);
  return t;
};

describe("shared vectors with the server (template values)", () => {
  it("parses the generic template that the server publishes", () => {
    expect(generic).not.toBeNull();
  });

  it.each(file.validate.map((v) => [v.name, v] as const))("validate: %s", (_name, v) => {
    const got = validateSetupV2(pick(v.template), v.data);
    if (got.ok) {
      expect(v.ok).toBe(true);
      expect(got.data).toEqual(v.out);
    } else {
      expect(v.ok ?? false).toBe(false);
      expect(got.issue.path).toBe(v.path);
      expect(got.issue.ruleId).toBe(v.rule);
    }
  });

  it.each(file.v1_to_values.map((v) => [v.name, v] as const))("v1ToValues: %s", (_name, v) => {
    if (!generic) throw new Error("no generic template");
    expect(v1ToValues(generic, v.data)).toEqual({ values: v.values, refs: v.refs });
  });
});

interface CompareCase {
  name: string;
  a: { template: string; values: Record<string, unknown>; refs: Record<string, unknown> };
  b: { template: string; values: Record<string, unknown>; refs: Record<string, unknown> };
  differences: unknown[];
  skipped: string[];
}
const compareFile = read("domain/testdata/compare/v2.json") as { templates: Record<string, unknown>; cases: CompareCase[] };
const compareTpls = templates(compareFile.templates);

describe("shared vectors with the server (compare v2)", () => {
  it.each(compareFile.cases.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const view = (s: CompareCase["a"]) => {
      const template = compareTpls[s.template];
      if (!template) throw new Error(s.template);
      return { template, values: s.values, refs: s.refs };
    };
    const got = compareVersions(view(c.a), view(c.b));
    expect(got.differences).toEqual(c.differences);
    expect(got.skipped).toEqual(c.skipped);
  });
});

describe("templateToZod", () => {
  it("reports the rule id and the field, never the value", () => {
    const schema = templateToZod(pick("a"));
    const r = schema.safeParse({ front_cst: 5, mystery: "secret-value" });
    expect(r.success).toBe(false);
    if (r.success) return;
    const issues = r.error.issues.map((i) => [i.path.join("."), i.message]);
    expect(issues).toContainEqual(["front_cst", "setup.data.range"]);
    expect(issues).toContainEqual(["", "setup.data.unknown"]);
    expect(JSON.stringify(r.error.issues)).not.toContain("secret-value");
  });

  it("accepts valid values", () => {
    expect(templateToZod(pick("a")).safeParse({ front_cst: 500, tower: "2" }).success).toBe(true);
  });
});

describe("parseTemplate", () => {
  const base = (): Record<string, unknown> => JSON.parse(JSON.stringify(file.templates.a)) as Record<string, unknown>;
  it("refuses unknown attributes", () => {
    expect(parseTemplate({ ...base(), extra: 1 })).toBeNull();
  });
  it("refuses a diagram outside the closed set", () => {
    const t = base();
    const groups = t.groups as { fields: { key: string; diagram?: unknown }[] }[];
    const tower = groups[0]?.fields.find((f) => f.key === "tower");
    if (tower) tower.diagram = { kind: "svg", href: "https://x" };
    expect(parseTemplate(t)).toBeNull();
  });
  it("refuses a hole row whose count differs from the options", () => {
    const t = base();
    const groups = t.groups as { fields: { key: string; diagram?: unknown }[] }[];
    const tower = groups[0]?.fields.find((f) => f.key === "tower");
    if (tower) tower.diagram = { kind: "hole_row", count: 5 };
    expect(parseTemplate(t)).toBeNull();
  });
});

describe("parseTemplate: table layout and multi (same rules as Go's validateTable)", () => {
  type Group = { table?: { columns: unknown[]; rows: { label: unknown; cells: string[] }[] }; fields: { key: string }[] };
  const mutated = (change: (shocks: Group) => void): Template | null => {
    const t = JSON.parse(JSON.stringify(file.templates.m)) as { groups: Group[] };
    const shocks = t.groups[1];
    if (!shocks) throw new Error("fixture changed");
    change(shocks);
    return parseTemplate(t);
  };
  const rows = (shocks: Group) => {
    if (!shocks.table) throw new Error("fixture changed");
    return shocks.table.rows;
  };

  it("accepts the shared template with a multi field and a table", () => {
    expect(mutated(() => undefined)).not.toBeNull();
  });
  it.each([
    ["one column", (g: Group) => { if (g.table) g.table.columns = g.table.columns.slice(0, 1); }],
    ["five columns", (g: Group) => { if (g.table) g.table.columns = Array.from({ length: 5 }, () => ({ pt: "x" })); }],
    ["no rows", (g: Group) => { if (g.table) g.table.rows = []; }],
    ["row width mismatch", (g: Group) => { const r = rows(g)[0]; if (r) r.cells = ["oil_front"]; }],
    ["unknown key", (g: Group) => { const r = rows(g)[1]; if (r) r.cells = ["spring_front", "nope"]; }],
    ["key used twice", (g: Group) => { const r = rows(g)[1]; if (r) r.cells = ["spring_front", "oil_front"]; }],
    ["a field left out", (g: Group) => { const r = rows(g)[1]; if (r) r.cells = ["", ""]; }],
    ["key of another group", (g: Group) => { const r = rows(g)[1]; if (r) r.cells = ["spring_front", "kingpin"]; }],
  ])("refuses a table with %s", (_name, change) => {
    expect(mutated(change)).toBeNull();
  });

  it("refuses a multi field with a part_ref, a diagram or one option", () => {
    const withField = (change: (f: Record<string, unknown>) => void): Template | null => {
      const t = JSON.parse(JSON.stringify(file.templates.m)) as { groups: { fields: Record<string, unknown>[] }[] };
      const f = t.groups[0]?.fields[0];
      if (!f) throw new Error("fixture changed");
      change(f);
      return parseTemplate(t);
    };
    expect(withField((f) => { f.part_ref = "T1"; })).toBeNull();
    expect(withField((f) => { f.diagram = { kind: "hole_row", count: 4 }; })).toBeNull();
    expect(withField((f) => { f.options = ["E0160"]; })).toBeNull();
    expect(withField((f) => { f.kit = ["E9999"]; })).toBeNull();
    expect(withField((f) => { f.kit = ["E0160", "E2146"]; })).not.toBeNull();
  });
});
