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
import { compareSetupData } from "../domain/compare";
import { SetupsLoading, SetupsNotFound, SetupsPageHeader } from "./common";
import { SetupChanges } from "./SetupChanges";
import { useGarageChoices } from "./use-choices";

/** Two versions of one setup, with every difference listed as before → after. */
export function SetupComparePage({
  teamId,
  setupId,
  from,
  to,
  repository,
  garageRepository,
}: {
  teamId: string;
  setupId: string;
  from?: number | undefined;
  to?: number | undefined;
  repository: SetupRepository;
  garageRepository: GarageRepository;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const setup = useQuery(setupQueryOptions(repository, teamId, setupId));
  const versions = useQuery(versionsQueryOptions(repository, teamId, setupId));
  const { names } = useGarageChoices(garageRepository, teamId);

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
  const differences = before && after ? compareSetupData(before.data, after.data) : [];

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
              <SetupChanges differences={differences} names={names} />
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
