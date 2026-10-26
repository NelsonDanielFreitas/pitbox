import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { carsQueryOptions, enginesQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { canWriteGarage } from "../domain/roles";
import { GarageEmpty, GaragePageHeader } from "./common";
import { AssetStatusBadge } from "./status";

export function CarListPage({
  teamId,
  role,
  repository,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t } = useTranslation();
  const cars = useQuery(carsQueryOptions(repository, teamId));
  const engines = useQuery(enginesQueryOptions(repository, teamId));
  const canWrite = canWriteGarage(role);
  const engineByCar = new Map(
    (engines.data ?? [])
      .filter((engine) => engine.currentCar !== null)
      .map((engine) => [engine.currentCar!.id, engine]),
  );

  return (
    <>
      <GaragePageHeader
        title={t("garage.sections.cars")}
        subtitle={t("garage.subtitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/cars/new"
              params={{ teamId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("garage.actions.newCar")}
            </Link>
          ) : null
        }
      />
      {cars.isPending ? (
        <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
          <Spinner />
          <span>{t("garage.loading")}</span>
        </div>
      ) : cars.isError ? (
        <ErrorState
          title={t("garage.errorTitle")}
          body={t("garage.errorBody")}
          onRetry={() => void cars.refetch()}
        />
      ) : cars.data.length === 0 ? (
        <GarageEmpty
          title={t("garage.empty.cars")}
          body={t("garage.empty.carsBody")}
          actionLabel={canWrite ? t("garage.actions.newCar") : undefined}
          to={canWrite ? "cars" : undefined}
          teamId={teamId}
        />
      ) : (
        <ul className="grid gap-3">
          {cars.data.map((car) => (
            <li key={car.id}>
              <Link
                to="/teams/$teamId/garage/cars/$carId"
                params={{ teamId, carId: car.id }}
                className="flex min-h-20 items-center gap-3 rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
              >
                <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-surface-2 text-muted">
                  <CarIcon />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold text-fg">
                    {car.nickname ?? `${car.make} ${car.model}`}
                  </span>
                  <span className="block truncate text-xs text-muted">
                    {car.make} {car.model}
                    {car.transponderNumber ? ` · ${car.transponderNumber}` : ""}
                  </span>
                  {engineByCar.get(car.id) ? (
                    <span className="block truncate text-xs font-medium text-ok">
                      {t("garage.car.engine", {
                        name: `${engineByCar.get(car.id)!.make} ${engineByCar.get(car.id)!.model}`,
                      })}
                    </span>
                  ) : null}
                </span>
                <AssetStatusBadge status={car.status} />
                <Chevron />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

function CarIcon() {
  return (
    <svg aria-hidden="true" className="size-6" viewBox="0 0 24 24" fill="none">
      <path d="M4 15h16M6 15l2-5h8l2 5M7 18h.01M17 18h.01" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function Chevron() {
  return (
    <svg aria-hidden="true" className="size-5 shrink-0 text-muted" viewBox="0 0 24 24" fill="none">
      <path d="m9 6 6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
