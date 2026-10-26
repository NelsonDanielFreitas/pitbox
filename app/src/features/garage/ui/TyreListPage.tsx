import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { tyreSetsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { canWriteGarage } from "../domain/roles";
import { GarageEmpty, GaragePageHeader } from "./common";
import { TyreStatusBadge } from "./status";

export function TyreListPage({
  teamId,
  role,
  repository,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t } = useTranslation();
  const tyres = useQuery(tyreSetsQueryOptions(repository, teamId));
  const canWrite = canWriteGarage(role);

  return (
    <>
      <GaragePageHeader
        title={t("garage.sections.tyres")}
        subtitle={t("garage.subtitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/tyres/new"
              params={{ teamId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("garage.actions.newTyreSet")}
            </Link>
          ) : null
        }
      />
      {tyres.isPending ? (
        <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
          <Spinner />
          <span>{t("garage.loading")}</span>
        </div>
      ) : tyres.isError ? (
        <ErrorState
          title={t("garage.errorTitle")}
          body={t("garage.errorBody")}
          onRetry={() => void tyres.refetch()}
        />
      ) : tyres.data.length === 0 ? (
        <GarageEmpty
          title={t("garage.empty.tyres")}
          body={t("garage.empty.tyresBody")}
          actionLabel={canWrite ? t("garage.actions.newTyreSet") : undefined}
          to={canWrite ? "tyres" : undefined}
          teamId={teamId}
        />
      ) : (
        <ul className="grid gap-3">
          {tyres.data.map((tyre) => (
            <li key={tyre.id}>
              <Link
                to="/teams/$teamId/garage/tyres/$tyreSetId"
                params={{ teamId, tyreSetId: tyre.id }}
                className="flex min-h-20 items-center gap-3 rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-primary"
              >
                <span className="flex size-12 shrink-0 items-center justify-center rounded-full border-2 border-line text-sm font-black text-fg">
                  {tyre.label}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold text-fg">
                    {tyre.make} {tyre.model}
                  </span>
                  <span className="block truncate text-xs text-muted">
                    {[tyre.compound, tyre.insertType].filter(Boolean).join(" · ") || "—"}
                  </span>
                </span>
                <TyreStatusBadge status={tyre.status} />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
