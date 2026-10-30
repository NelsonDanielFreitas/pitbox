import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { carsQueryOptions } from "@/features/garage/data/garage-queries";
import type { GarageRepository } from "@/features/garage/data/garage-repository";
import { formatDateTime } from "@/features/garage/domain/format";
import type { TeamRole } from "@/features/teams/domain/team";
import { Alert } from "@/shared/ui/Alert";
import { Badge } from "@/shared/ui/Badge";
import { ErrorState } from "@/shared/ui/ErrorState";

import { setupQueryOptions, versionQueryOptions, versionsQueryOptions } from "../data/setup-queries";
import type { SetupRepository } from "../data/setup-repository";
import { compareSetupData } from "../domain/compare";
import { canWriteSetups } from "../domain/roles";
import { SetupsLoading, SetupsNotFound, SetupsPageHeader } from "./common";
import { FeedbackSection } from "./FeedbackSection";
import { SetupChanges } from "./SetupChanges";
import { SetupValues } from "./SetupValues";
import { useGarageChoices } from "./use-choices";

export function SetupDetailPage({
  teamId,
  setupId,
  version,
  role,
  repository,
  garageRepository,
}: {
  teamId: string;
  setupId: string;
  /** A version to look at; the current one when absent. */
  version?: number | undefined;
  role: TeamRole;
  repository: SetupRepository;
  garageRepository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const setup = useQuery(setupQueryOptions(repository, teamId, setupId));
  const versions = useQuery(versionsQueryOptions(repository, teamId, setupId));
  const shown = useQuery(versionQueryOptions(repository, teamId, setupId, version));
  const cars = useQuery(carsQueryOptions(garageRepository, teamId));
  const { names } = useGarageChoices(garageRepository, teamId);
  const basedOnId = shown.data?.basedOn?.setupId;
  const source = useQuery({
    ...setupQueryOptions(repository, teamId, basedOnId ?? ""),
    enabled: basedOnId !== undefined,
  });

  if (setup.isPending || versions.isPending || shown.isPending) return <SetupsLoading />;
  if (setup.isError || versions.isError || shown.isError) {
    return (
      <ErrorState
        title={t("setups.errorTitle")}
        body={t("setups.errorBody")}
        onRetry={() => {
          void setup.refetch();
          void versions.refetch();
          void shown.refetch();
        }}
      />
    );
  }
  if (!setup.data || !shown.data) return <SetupsNotFound />;

  const current = setup.data.currentVersion;
  const viewing = shown.data;
  const previous = versions.data.find((v) => v.version === viewing.version - 1);
  const differences = previous ? compareSetupData(previous.data, viewing.data) : [];
  const car = cars.data?.find((c) => c.id === setup.data?.carId);
  const canEdit = canWriteSetups(role) && viewing.version === current;

  return (
    <>
      <SetupsPageHeader
        title={setup.data.name}
        subtitle={[car ? (car.nickname ?? `${car.make} ${car.model}`) : null, t("setups.detail.version", { version: viewing.version })]
          .filter(Boolean)
          .join(" · ")}
        action={
          canEdit ? (
            <Link
              to="/teams/$teamId/setups/$setupId/edit"
              params={{ teamId, setupId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("setups.actions.edit")}
            </Link>
          ) : null
        }
      />
      <div className="space-y-5">
        {!canWriteSetups(role) ? <Alert tone="info">{t("setups.readOnly")}</Alert> : null}
        {viewing.version !== current ? (
          <Alert tone="warn">
            <p>{t("setups.detail.viewing", { version: viewing.version })}</p>
            <Link
              to="/teams/$teamId/setups/$setupId"
              params={{ teamId, setupId }}
              className="mt-2 inline-flex min-h-11 items-center font-semibold underline"
            >
              {t("setups.detail.backToCurrent")}
            </Link>
          </Alert>
        ) : null}
        {setup.data.notes ? <p className="text-sm text-muted">{setup.data.notes}</p> : null}
        {viewing.changeSummary ? <p className="text-sm font-medium text-fg">{viewing.changeSummary}</p> : null}
        {viewing.basedOn ? (
          <p className="text-sm text-muted">
            {t("setups.detail.basedOn", {
              name: source.data?.name ?? t("setups.detail.unknownSetup"),
              version: viewing.basedOn.version,
            })}
          </p>
        ) : null}

        <section aria-labelledby="setup-changes-title">
          <h2 id="setup-changes-title" className="mb-2 text-lg font-semibold text-fg">
            {previous
              ? t("setups.detail.changesTitle", { previous: previous.version })
              : t("setups.detail.history")}
          </h2>
          {!previous ? (
            <p className="text-sm text-muted">{t("setups.detail.firstVersion")}</p>
          ) : differences.length === 0 ? (
            <p className="text-sm text-muted">{t("setups.detail.noChanges")}</p>
          ) : (
            <SetupChanges differences={differences} names={names} />
          )}
        </section>

        <SetupValues data={viewing.data} names={names} />

        <FeedbackSection
          teamId={teamId}
          setupId={setupId}
          version={viewing.version}
          role={role}
          repository={repository}
        />

        <section aria-labelledby="setup-history-title">
          <div className="mb-2 flex items-center justify-between gap-3">
            <h2 id="setup-history-title" className="text-lg font-semibold text-fg">
              {t("setups.detail.history")}
            </h2>
            {versions.data.length > 1 ? (
              <Link
                to="/teams/$teamId/setups/$setupId/compare"
                params={{ teamId, setupId }}
                search={
                  viewing.version > 1
                    ? { from: viewing.version - 1, to: viewing.version }
                    : { from: 1, to: current }
                }
                className="flex min-h-11 items-center font-semibold text-primary underline"
              >
                {t("setups.actions.compare")}
              </Link>
            ) : null}
          </div>
          <ul className="grid gap-2">
            {versions.data.map((v) => (
              <li key={v.version}>
                <Link
                  to="/teams/$teamId/setups/$setupId"
                  params={{ teamId, setupId }}
                  search={{ v: v.version === current ? undefined : v.version }}
                  aria-current={v.version === viewing.version ? "true" : undefined}
                  className="flex min-h-14 items-center gap-3 rounded-xl border border-line bg-surface px-4 py-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary aria-[current=true]:border-primary"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-semibold text-fg">
                      {t("setups.detail.historyLine", {
                        version: v.version,
                        date: formatDateTime(v.createdAt, i18n.language),
                      })}
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {v.changeSummary ?? t("setups.detail.noSummary")}
                    </span>
                  </span>
                  {v.version === current ? <Badge tone="ok">{t("setups.detail.current")}</Badge> : null}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </>
  );
}
