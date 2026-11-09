import { useTranslation } from "react-i18next";

import type { VersionView } from "../../domain/compare";
import { shownValue, localized } from "./text";

/** Read-only view of a version made on a template: the groups and order of the sheet, filled fields only. */
export function TemplateValues({ view, names }: { view: VersionView; names: ReadonlyMap<string, string> }) {
  const { t, i18n } = useTranslation();
  const yes = t("setups.template.flagOn");
  const no = t("setups.template.flagOff");
  const groups = view.template.groups
    .map((group) => ({ group, fields: group.fields.filter((f) => view.values[f.key] !== undefined) }))
    .filter((entry) => entry.fields.length > 0);
  const refs = Object.entries(view.refs).filter((entry): entry is [string, string] => typeof entry[1] === "string");

  if (groups.length === 0 && refs.length === 0) {
    return (
      <div className="rounded-2xl border border-dashed border-line bg-surface p-6 text-center">
        <h2 className="text-lg font-semibold text-fg">{t("setups.detail.empty")}</h2>
        <p className="mt-2 text-sm text-muted">{t("setups.detail.emptyBody")}</p>
      </div>
    );
  }
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted">{t("setups.template.sheet", { name: localized(view.template.name, i18n.language) })}</p>
      {groups.map(({ group, fields }) => (
        <section key={group.key} className="rounded-2xl border border-line bg-surface p-4">
          <h2 className="text-base font-semibold text-fg">{localized(group.label, i18n.language)}</h2>
          <dl className="mt-2">
            {fields.map((field) => (
              <div key={field.key} className="flex items-start justify-between gap-4 border-b border-line py-2 last:border-0">
                <dt className="text-sm text-muted">{localized(field.label, i18n.language)}</dt>
                <dd className="text-right text-sm font-medium text-fg">{shownValue(field, view.values[field.key], yes, no)}</dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
      {refs.length > 0 ? (
        <section className="rounded-2xl border border-line bg-surface p-4">
          <dl>
            {refs.map(([key, id]) => (
              <div key={key} className="flex items-start justify-between gap-4 border-b border-line py-2 last:border-0">
                <dt className="text-sm text-muted">{key === "engine_id" ? t("setups.fields.engine.engine_id") : t("setups.fields.tyres.tyre_set_id")}</dt>
                <dd className="text-right text-sm font-medium text-fg">{names.get(id) ?? `…${id.slice(-6)}`}</dd>
              </div>
            ))}
          </dl>
        </section>
      ) : null}
    </div>
  );
}
