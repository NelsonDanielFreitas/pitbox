// @vitest-environment node

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { setupFields, setupGroups } from "./catalogue";

interface Rule {
  type: string;
  min?: number;
  max?: number;
  max_len?: number;
  fields?: Record<string, Rule>;
}

const schemaUrl = new URL("../../../../../backend/internal/setup/domain/schema_v1.json", import.meta.url);
const schema = JSON.parse(readFileSync(fileURLToPath(schemaUrl), "utf8")) as {
  version: number;
  fields: Record<string, Rule>;
};

function leaves(prefix: string, fields: Record<string, Rule>): Map<string, Rule> {
  const out = new Map<string, Rule>();
  for (const [key, rule] of Object.entries(fields)) {
    const path = prefix === "" ? key : `${prefix}.${key}`;
    if (rule.type === "group" && rule.fields) {
      for (const [p, r] of leaves(path, rule.fields)) out.set(p, r);
    } else {
      out.set(path, rule);
    }
  }
  return out;
}

describe("setup field catalogue", () => {
  const server = leaves("", schema.fields);

  it("targets schema version 1", () => {
    expect(schema.version).toBe(1);
  });

  it("has exactly the fields of the server schema (drift test)", () => {
    expect(setupFields.map((f) => f.path).sort()).toEqual([...server.keys()].sort());
  });

  it.each(setupFields.map((f) => [f.path, f] as const))("matches the server rule for %s", (path, field) => {
    const rule = server.get(path);
    expect(rule).toBeDefined();
    expect(field.kind).toBe(rule?.type);
    expect(field.min).toBe(rule?.min);
    expect(field.max).toBe(rule?.max);
    expect(field.maxLen).toBe(rule?.max_len);
  });

  it("has unique paths and a unit for every numeric field except counts", () => {
    expect(new Set(setupFields.map((f) => f.path)).size).toBe(setupFields.length);
    for (const field of setupFields) {
      if (field.kind === "number") expect(field.unit, field.path).toBeTruthy();
    }
  });

  it("splits shocks into front and rear", () => {
    expect(setupGroups.map((g) => g.id)).toEqual(
      expect.arrayContaining(["shocks.front", "shocks.rear", "diffs", "radio"]),
    );
  });
});
