import type { TFunction } from "i18next";

import { fieldByPath } from "../../domain/catalogue";
import { groupOf } from "../../domain/compare";
import type { Template, TemplateField } from "../../domain/template";
import type { SetupLabels } from "../labels";
import { localized, shownValue } from "./text";

/** How to name a difference path when the versions were made on templates. */
export interface Describer {
  group(path: string): { id: string; label: string };
  label(path: string): string;
  value(path: string, value: unknown, names: ReadonlyMap<string, string>): string;
}

const refPaths: Record<string, string> = { "refs.engine_id": "engine.engine_id", "refs.tyre_set_id": "tyres.tyre_set_id" };

/**
 * Same template: paths are field keys. Different templates: paths are v1 canonical paths, which
 * the v1 labels already name. `refs.*` (engine, tyre set) are named like their v1 counterparts.
 */
export function makeDescriber(templates: readonly Template[], labels: SetupLabels, t: TFunction, language: string): Describer {
  const byKey = new Map<string, { field: TemplateField; group: { id: string; label: string } }>();
  for (const template of templates) {
    for (const group of template.groups) {
      for (const field of group.fields) {
        if (!byKey.has(field.key)) byKey.set(field.key, { field, group: { id: group.key, label: localized(group.label, language) } });
      }
    }
  }
  const yes = t("setups.template.flagOn");
  const no = t("setups.template.flagOff");
  const mapped = (path: string) => refPaths[path] ?? path;
  return {
    group(path) {
      const hit = byKey.get(path);
      if (hit) return hit.group;
      const id = groupOf(mapped(path));
      return { id, label: labels.group(id) };
    },
    label(path) {
      const hit = byKey.get(path);
      return hit ? localized(hit.field.label, language) : labels.field(mapped(path));
    },
    value(path, value, names) {
      const hit = byKey.get(path);
      if (hit) return shownValue(hit.field, value, yes, no);
      const v1 = fieldByPath(mapped(path));
      if (v1?.kind === "uuid" && typeof value === "string") return names.get(value) ?? `…${value.slice(-6)}`;
      return labels.value(v1, value);
    },
  };
}

