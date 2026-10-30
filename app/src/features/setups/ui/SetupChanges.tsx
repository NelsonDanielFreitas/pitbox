import { useTranslation } from "react-i18next";

import { Badge, type BadgeTone } from "@/shared/ui/Badge";

import { fieldByPath } from "../domain/catalogue";
import { groupOf } from "../domain/compare";
import type { Difference, DifferenceKind } from "../domain/models";
import { useSetupLabels, type SetupLabels } from "./labels";

const tones: Record<DifferenceKind, BadgeTone> = { added: "ok", removed: "crit", changed: "warn" };
const marks: Record<DifferenceKind, string> = { added: "+", removed: "−", changed: "≠" };

/** Differences grouped like the sheet. The kind is a word and a sign, never only a colour. */
export function SetupChanges({
  differences,
  names,
}: {
  differences: readonly Difference[];
  names: ReadonlyMap<string, string>;
}) {
  const { t } = useTranslation();
  const labels = useSetupLabels();
  const byGroup = new Map<string, Difference[]>();
  for (const difference of differences) {
    const group = groupOf(difference.path);
    byGroup.set(group, [...(byGroup.get(group) ?? []), difference]);
  }
  return (
    <div className="space-y-3">
      {[...byGroup.entries()].map(([group, items]) => (
        <section key={group} className="rounded-2xl border border-line bg-surface p-4">
          <h3 className="text-base font-semibold text-fg">{labels.group(group)}</h3>
          <ul className="mt-2 divide-y divide-line">
            {items.map((difference) => (
              <li key={difference.path} className="py-2">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm font-medium text-fg">{labels.field(difference.path)}</span>
                  <Badge tone={tones[difference.kind]}>
                    <span aria-hidden="true" className="mr-1">{marks[difference.kind]}</span>
                    {t(`setups.detail.kind.${difference.kind}`)}
                  </Badge>
                </div>
                <p className="mt-1 text-sm text-muted">
                  <span className="sr-only">{t("setups.detail.before")}: </span>
                  {show(labels, names, difference, difference.before)}
                  <span aria-hidden="true"> → </span>
                  <span className="sr-only">, {t("setups.detail.after")}: </span>
                  <span className="font-medium text-fg">{show(labels, names, difference, difference.after)}</span>
                </p>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function show(
  labels: SetupLabels,
  names: ReadonlyMap<string, string>,
  difference: Difference,
  value: unknown,
): string {
  const field = fieldByPath(difference.path);
  if (field?.kind === "uuid" && typeof value === "string") return names.get(value) ?? `…${value.slice(-6)}`;
  return labels.value(field, value);
}
