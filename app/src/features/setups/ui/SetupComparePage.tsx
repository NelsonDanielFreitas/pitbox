import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { GarageRepository } from "@/features/garage/data/garage-repository";
import { Alert } from "@/shared/ui/Alert";
import { ErrorState } from "@/shared/ui/ErrorState";
import { FormField } from "@/shared/ui/FormField";
import { SelectInput } from "@/shared/ui/SelectInput";

import { setupQueryOptions, versionsQueryOptions } from "../data/setup-queries";
import type { SetupRepository } from "../data/setup-repository";
import { compareSetupData, compareVersions } from "../domain/compare";
import { defaultTemplateRepository, type TemplateRepository } from "../data/template-repository";
import { SetupsLoading, SetupsNotFound, SetupsPageHeader } from "./common";
import { SetupChanges } from "./SetupChanges";
import { useGarageChoices } from "./use-choices";
import { useSetupLabels } from "./labels";
import { templateFields, type Template } from "../domain/template";
import { localized } from "./template/text";
import { makeDescriber } from "./template/describe";
import { useVersionViews } from "./template/use-version-views";

/** Two versions of one setup, with every difference listed as before → after. */
export function SetupComparePage({
  teamId,
  setupId,
  from,
  to,
  repository,
  garageRepository,
  templateRepository = defaultTemplateRepository,
}: {
  teamId: string;
  setupId: string;
  from?: number | undefined;
  to?: number | undefined;
  repository: SetupRepository;
  garageRepository: GarageRepository;
  templateRepository?: TemplateRepository;
}) {
  const { t, i18n } = useTranslation();
  const labels = useSetupLabels();
  const navigate = useNavigate();
  const setup = useQuery(setupQueryOptions(repository, teamId, setupId));
  const versions = useQuery(versionsQueryOptions(repository, teamId, setupId));
  const { names } = useGarageChoices(garageRepository, teamId);
  const views = useVersionViews(templateRepository, teamId, versions.data ?? []);

  if (setup.isPending || versions.isPending) return <SetupsLoading />;
  if (setup.isError || versions.isError) {
    return (
      <ErrorState
        title={t("setups.errorTitle")}
        body={t("setups.errorBody")}
        onRetry={() => {
          void setup.refetch();
          void versions.refetch();
        }}
      />
    );
  }
  if (!setup.data) return <SetupsNotFound />;

  const all = versions.data; // newest first
  const latest = all[0]?.version;
  const toVersion = to ?? latest;
  const fromVersion = from ?? (toVersion !== undefined ? toVersion - 1 : undefined);
  const before = all.find((v) => v.version === fromVersion);
  const after = all.find((v) => v.version === toVersion);
  const onTemplates = before?.schemaVersion === 2 || after?.schemaVersion === 2;
  const beforeView = before ? views.viewOf(before) : undefined;
  const afterView = after ? views.viewOf(after) : undefined;
  // v1 with v1 keeps the v1 comparison; anything on a template goes through its sheet, and two
  // different sheets are compared by their common (canonical) fields only.
  const result =
    before && after && !onTemplates
      ? { differences: compareSetupData(before.data, after.data), skipped: [] as string[] }
      : beforeView && afterView
        ? compareVersions(beforeView, afterView)
        : { differences: [], skipped: [] as string[] };
  const differences = result.differences;
  const describer = onTemplates ? makeDescriber(views.templates, labels, t, i18n.language) : undefined;
  const sheetMissing = onTemplates && !views.isPending && Boolean(before && after) && (!beforeView || !afterView);
  const differentSheets = Boolean(beforeView && afterView && beforeView.template.id !== afterView.template.id);

  const go = (nextFrom: number | undefined, nextTo: number | undefined) =>
    void navigate({
      to: "/teams/$teamId/setups/$setupId/compare",
      params: { teamId, setupId },
      search: { from: nextFrom, to: nextTo },
      replace: true,
    });

  return (
    <>
      <SetupsPageHeader title={t("setups.compare.title")} subtitle={setup.data.name} />
      {all.length < 2 ? (
        <Alert tone="info">{t("setups.compare.notEnough")}</Alert>
      ) : (
        <div className="space-y-5">
          <div className="grid grid-cols-2 gap-3">
            <FormField id="compare-from" label={t("setups.compare.from")}>
              <SelectInput
                id="compare-from"
                value={fromVersion ?? ""}
                onChange={(event) => go(Number(event.target.value), toVersion)}
              >
                {all.map((v) => (
                  <option key={v.version} value={v.version}>
                    {t("setups.compare.version", { version: v.version })}
                  </option>
                ))}
              </SelectInput>
            </FormField>
            <FormField id="compare-to" label={t("setups.compare.to")}>
              <SelectInput
                id="compare-to"
                value={toVersion ?? ""}
                onChange={(event) => go(fromVersion, Number(event.target.value))}
              >
                {all.map((v) => (
                  <option key={v.version} value={v.version}>
                    {t("setups.compare.version", { version: v.version })}
                  </option>
                ))}
              </SelectInput>
            </FormField>
          </div>
          {!before || !after || before.version === after.version ? (
            <Alert tone="warn">{t("setups.compare.same")}</Alert>
          ) : sheetMissing ? (
            <Alert tone="warn">{t("setups.template.missingTemplate")}</Alert>
          ) : differences.length === 0 ? (
            <p className="text-sm text-muted" role="status">
              {t("setups.compare.empty", { from: before.version, to: after.version })}
            </p>
          ) : (
            <>
              <p className="text-sm font-semibold text-fg" role="status">
                {t("setups.compare.summary", {
                  count: differences.length,
                  from: before.version,
                  to: after.version,
                })}
              </p>
              {differentSheets ? <Alert tone="info">{t("setups.template.comparedByCanonical")}</Alert> : null}
              <SetupChanges differences={differences} names={names} describer={describer} />
              {result.skipped.length > 0 ? (
                <p className="text-sm text-muted">
                  {t("setups.template.skipped", { fields: skippedLabels(result.skipped, views.templates, i18n.language) })}
                </p>
              ) : null}
            </>
          )}
        </div>
      )}
      <Link
        to="/teams/$teamId/setups/$setupId"
        params={{ teamId, setupId }}
        className="mt-6 inline-flex min-h-12 items-center font-semibold text-primary underline"
      >
        {t("setups.actions.back")}
      </Link>
    </>
  );
}

/** The labels of the fields that had no counterpart, in the sheet they belong to. */
function skippedLabels(keys: readonly string[], templates: readonly Template[], language: string): string {
  const labels = new Map<string, string>();
  for (const template of templates) {
    for (const field of templateFields(template)) labels.set(field.key, localized(field.label, language));
  }
  return keys.map((key) => labels.get(key) ?? key).join(", ");
}
