import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { carsQueryOptions } from "@/features/garage/data/garage-queries";
import type { GarageRepository } from "@/features/garage/data/garage-repository";
import { formatDate } from "@/features/garage/domain/format";
import type { TeamRole } from "@/features/teams/domain/team";
import { ErrorState } from "@/shared/ui/ErrorState";
import { SelectInput } from "@/shared/ui/SelectInput";

import { setupsQueryOptions } from "../data/setup-queries";
import type { SetupRepository } from "../data/setup-repository";
import { canWriteSetups } from "../domain/roles";
import { Chevron, SetupsLoading, SetupsPageHeader } from "./common";

export function SetupListPage({
  teamId,
  role,
  repository,
  garageRepository,
}: {
  teamId: string;
  role: TeamRole;
  repository: SetupRepository;
  garageRepository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const [carId, setCarId] = useState("");
  const setups = useQuery(setupsQueryOptions(repository, teamId, carId || undefined));
  const cars = useQuery(carsQueryOptions(garageRepository, teamId));
  const canWrite = canWriteSetups(role);
  const carNames = new Map((cars.data ?? []).map((c) => [c.id, c.nickname ?? `${c.make} ${c.model}`] as const));

  return (
    <>
      <SetupsPageHeader
        title={t("setups.title")}
        subtitle={t("setups.subtitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/setups/new"
              params={{ teamId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("setups.actions.new")}
            </Link>
          ) : null
        }
      />
      {(cars.data?.length ?? 0) > 1 ? (
        <div className="mb-4">
          <label htmlFor="setup-car-filter" className="mb-1.5 block text-sm font-semibold text-fg">
            {t("setups.list.filterLabel")}
          </label>
          <SelectInput id="setup-car-filter" value={carId} onChange={(event) => setCarId(event.target.value)}>
            <option value="">{t("setups.list.filterAll")}</option>
            {(cars.data ?? []).map((car) => (
              <option key={car.id} value={car.id}>
                {carNames.get(car.id)}
              </option>
            ))}
          </SelectInput>
        </div>
      ) : null}
      {setups.isPending ? (
        <SetupsLoading />
      ) : setups.isError ? (
        <ErrorState
          title={t("setups.errorTitle")}
          body={t("setups.errorBody")}
          onRetry={() => void setups.refetch()}
        />
      ) : setups.data.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-line bg-surface p-6 text-center">
          <h2 className="text-lg font-semibold text-fg">{t("setups.list.empty")}</h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-muted">{t("setups.list.emptyBody")}</p>
          {canWrite ? (
            <Link
              to="/teams/$teamId/setups/new"
              params={{ teamId }}
              className="mt-4 inline-flex min-h-12 items-center justify-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("setups.actions.new")}
            </Link>
          ) : null}
        </div>
      ) : (
        <ul className="grid gap-3">
          {setups.data.map((setup) => (
            <li key={setup.id}>
              <Link
                to="/teams/$teamId/setups/$setupId"
                params={{ teamId, setupId: setup.id }}
                className="flex min-h-20 items-center gap-3 rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold text-fg">{setup.name}</span>
                  <span className="block truncate text-xs text-muted">
                    {carNames.get(setup.carId) ?? "—"} ·{" "}
                    {t("setups.list.updated", { date: formatDate(setup.updatedAt, i18n.language) })}
                  </span>
                </span>
                <span className="shrink-0 rounded-md border border-line bg-surface-2 px-2 py-1 text-xs font-semibold text-muted">
                  {t("setups.list.version", { version: setup.currentVersion })}
                </span>
                <Chevron />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
