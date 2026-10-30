import { describe, expect, it } from "vitest";

import { en } from "@/i18n/resources.en";
import { ptPT } from "@/i18n/resources.pt-PT";

import { setupFields, setupGroups } from "../domain/catalogue";

function lookup(tree: unknown, path: string): unknown {
  return path.split(".").reduce<unknown>((node, key) => {
    return typeof node === "object" && node !== null ? (node as Record<string, unknown>)[key] : undefined;
  }, tree);
}

describe.each([
  ["pt-PT", ptPT],
  ["en", en],
])("setup labels in %s", (_name, resources) => {
  it.each(setupFields.map((f) => f.path))("labels the field %s", (path) => {
    expect(lookup(resources, `setups.fields.${path}`), path).toEqual(expect.any(String));
  });

  it.each(setupGroups.map((g) => g.id))("titles the group %s", (id) => {
    expect(lookup(resources, `setups.groups.${id}`), id).toEqual(expect.any(String));
  });

  it("translates the two worded units", () => {
    const units = new Set(setupFields.flatMap((f) => (f.unit ? [f.unit] : [])));
    for (const unit of ["teeth", "turns"]) {
      if (units.has(unit)) expect(lookup(resources, `setups.units.${unit}`)).toEqual(expect.any(String));
    }
  });
});
