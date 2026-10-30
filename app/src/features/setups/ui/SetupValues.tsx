import { useTranslation } from "react-i18next";

import { setupGroups } from "../domain/catalogue";
import { getPath } from "../domain/data";
import type { SetupData } from "../domain/models";
import { formatGearRatio, gearRatioOf } from "../domain/transmission";
import { useSetupLabels } from "./labels";

/** Read-only view of a version: only the groups and fields that have a value. */
export function SetupValues({
  data,
  names,
}: {
  data: SetupData;
  /** Display names for ids stored in the data (engine, tyre set). */
  names: ReadonlyMap<string, string>;
}) {
  const { t, i18n } = useTranslation();
  const labels = useSetupLabels();
  const groups = setupGroups
    .map((group) => ({
      group,
      fields: group.fields.filter((field) => getPath(data, field.path) !== undefined),
    }))
    .filter((entry) => entry.fields.length > 0);

  if (groups.length === 0) {
    return (
      <div className="rounded-2xl border border-dashed border-line bg-surface p-6 text-center">
        <h2 className="text-lg font-semibold text-fg">{t("setups.detail.empty")}</h2>
        <p className="mt-2 text-sm text-muted">{t("setups.detail.emptyBody")}</p>
      </div>
    );
  }

  const ratio = gearRatioOf(data);
  return (
    <div className="space-y-3">
      {groups.map(({ group, fields }) => (
        <section key={group.id} className="rounded-2xl border border-line bg-surface p-4">
          <h2 className="text-base font-semibold text-fg">{labels.group(group.id)}</h2>
          <dl className="mt-2">
            {fields.map((field) => {
              const value = getPath(data, field.path);
              const shown =
                field.kind === "uuid" && typeof value === "string"
                  ? (names.get(value) ?? `…${value.slice(-6)}`)
                  : labels.value(field, value);
              return (
                <div key={field.path} className="flex items-start justify-between gap-4 border-b border-line py-2 last:border-0">
                  <dt className="text-sm text-muted">{labels.field(field.path)}</dt>
                  <dd className="text-right text-sm font-medium text-fg">{shown}</dd>
                </div>
              );
            })}
            {group.id === "transmission" && ratio !== null ? (
              <div className="flex items-start justify-between gap-4 py-2">
                <dt className="text-sm text-muted">{t("setups.detail.gearRatio")}</dt>
                <dd className="text-right text-sm font-semibold text-fg">{formatGearRatio(ratio, i18n.language)}</dd>
              </div>
            ) : null}
          </dl>
        </section>
      ))}
    </div>
  );
}
