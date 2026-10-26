import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { enginesQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { canWriteGarage } from "../domain/roles";
import { GarageEmpty, GaragePageHeader } from "./common";
import { AssetStatusBadge, EngineMountBadge } from "./status";

export function EngineListPage({
  teamId,
  role,
  repository,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t } = useTranslation();
  const engines = useQuery(enginesQueryOptions(repository, teamId));
  const canWrite = canWriteGarage(role);

  return (
    <>
      <GaragePageHeader
        title={t("garage.sections.engines")}
        subtitle={t("garage.subtitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/engines/new"
              params={{ teamId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("garage.actions.newEngine")}
            </Link>
          ) : null
        }
      />
      {engines.isPending ? (
        <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
          <Spinner />
          <span>{t("garage.loading")}</span>
        </div>
      ) : engines.isError ? (
        <ErrorState
          title={t("garage.errorTitle")}
          body={t("garage.errorBody")}
          onRetry={() => void engines.refetch()}
        />
      ) : engines.data.length === 0 ? (
        <GarageEmpty
          title={t("garage.empty.engines")}
          body={t("garage.empty.enginesBody")}
          actionLabel={canWrite ? t("garage.actions.newEngine") : undefined}
          to={canWrite ? "engines" : undefined}
          teamId={teamId}
        />
      ) : (
        <ul className="grid gap-3">
          {engines.data.map((engine) => {
            const mounted = engine.currentCar !== null;
            return (
              <li key={engine.id}>
                <Link
                  to="/teams/$teamId/garage/engines/$engineId"
                  params={{ teamId, engineId: engine.id }}
                  className="flex min-h-20 items-center gap-3 rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-semibold text-fg">
                      {engine.make} {engine.model}
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {engine.currentCar
                        ? t("garage.engine.mountedIn", {
                            name:
                              engine.currentCar.nickname ??
                              `${engine.currentCar.make} ${engine.currentCar.model}`,
                          })
                        : t("garage.engine.available")}
                    </span>
                  </span>
                  {engine.status === "retired" ? (
                    <AssetStatusBadge status="retired" />
                  ) : (
                    <EngineMountBadge mounted={mounted} />
                  )}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
