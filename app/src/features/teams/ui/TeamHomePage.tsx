import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  carsQueryOptions,
  enginesQueryOptions,
  partsQueryOptions,
  tyreSetsQueryOptions,
} from "@/features/garage/data/garage-queries";
import type { GarageRepository } from "@/features/garage/data/garage-repository";
import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Spinner } from "@/shared/ui/Spinner";

import type { Team } from "../domain/team";

export function TeamHomePage({
  team,
  repository,
}: {
  team: Team;
  repository: GarageRepository;
}) {
  const { t } = useTranslation();
  const cars = useQuery(carsQueryOptions(repository, team.id));
  const engines = useQuery(enginesQueryOptions(repository, team.id));
  const tyres = useQuery(tyreSetsQueryOptions(repository, team.id));
  const parts = useQuery(partsQueryOptions(repository, team.id));

  if (cars.isPending || engines.isPending || tyres.isPending || parts.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }

  if (cars.isError || engines.isError || tyres.isError || parts.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => {
          void cars.refetch();
          void engines.refetch();
          void tyres.refetch();
          void parts.refetch();
        }}
      />
    );
  }

  const lowStock = parts.data.filter((part) => part.belowMin).length;
  const total = cars.data.length + engines.data.length + tyres.data.length + parts.data.length;

  return (
    <div className="space-y-6">
      <PageHeader title={t("garage.summaryTitle")} subtitle={t("garage.summaryBody")} />
      {total === 0 ? (
        <p className="rounded-2xl border border-dashed border-line bg-surface p-6 text-center text-sm text-muted">
          {t("garage.summaryEmpty")}
        </p>
      ) : (
        <div className="grid grid-cols-2 gap-3">
          <SummaryCard label={t("garage.sections.cars")} value={cars.data.length} />
          <SummaryCard label={t("garage.sections.engines")} value={engines.data.length} />
          <SummaryCard label={t("garage.sections.tyres")} value={tyres.data.length} />
          <SummaryCard
            label={t("garage.sections.parts")}
            value={parts.data.length}
            alert={lowStock > 0 ? t("garage.belowMinFilter") : undefined}
          />
        </div>
      )}
      <Link
        to="/teams/$teamId/garage/cars"
        params={{ teamId: team.id }}
        className="flex min-h-14 items-center justify-center rounded-xl bg-primary px-5 text-base font-semibold text-on-primary"
      >
        {t("garage.openGarage")}
      </Link>
    </div>
  );
}

function SummaryCard({
  label,
  value,
  alert,
}: {
  label: string;
  value: number;
  alert?: string;
}) {
  return (
    <div className="rounded-2xl border border-line bg-surface p-4">
      <p className="text-sm text-muted">{label}</p>
      <p className="mt-1 text-2xl font-bold tabular-nums text-fg">{value}</p>
      {alert ? <p className="mt-1 text-xs font-semibold text-warn">{alert}</p> : null}
    </div>
  );
}
