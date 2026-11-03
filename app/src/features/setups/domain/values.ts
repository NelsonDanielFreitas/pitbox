import { templateFields, type Template } from "./template";

// Values of a sheet template and the bridge to schema v1 (ADR 0030). Ports of Go's
// `ComputeCanonical` and `V1ToValues`.

const v1RefPaths: Readonly<Record<string, string>> = {
  "engine.engine_id": "engine_id",
  "tyres.tyre_set_id": "tyre_set_id",
};

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

/** Maps the filled values of a template to v1 paths, for the generated columns and comparisons. */
export function computeCanonical(t: Template, values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of templateFields(t)) {
    if (f.canonical !== undefined && f.key in values) out[f.canonical] = values[f.key];
  }
  return out;
}

/**
 * Reads stored v1 data as values of the generic template. Stored rows are never rewritten.
 * A path that is not a known v1 leaf keeps a "v1:" prefix, which is never a valid template key.
 */
export function v1ToValues(
  generic: Template,
  data: Record<string, unknown>,
): { values: Record<string, unknown>; refs: Record<string, unknown> } {
  const keyByPath = new Map<string, string>();
  for (const f of templateFields(generic)) if (f.canonical !== undefined) keyByPath.set(f.canonical, f.key);
  const groups = new Set<string>();
  for (const path of [...keyByPath.keys(), ...Object.keys(v1RefPaths)]) {
    const parts = path.split(".").slice(0, -1);
    parts.forEach((_, i) => groups.add(parts.slice(0, i + 1).join(".")));
  }

  const values: Record<string, unknown> = {};
  const refs: Record<string, unknown> = {};
  const walk = (prefix: string, m: Record<string, unknown>): void => {
    for (const [k, v] of Object.entries(m)) {
      const path = prefix === "" ? k : `${prefix}.${k}`;
      if (isRecord(v) && (Object.keys(v).length > 0 || groups.has(path))) {
        walk(path, v);
        continue;
      }
      const ref = v1RefPaths[path];
      const key = keyByPath.get(path);
      if (ref !== undefined) refs[ref] = v;
      else if (key !== undefined) values[key] = v;
      else values[`v1:${path}`] = v;
    }
  };
  walk("", data);
  return { values, refs };
}

/** v2 data without the server's `canonical` map: what the app validates, compares and sends. */
export function withoutCanonical(data: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(data).filter(([k]) => k !== "canonical"));
}
