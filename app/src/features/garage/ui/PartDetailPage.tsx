import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Badge } from "@/shared/ui/Badge";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { partQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatQuantity } from "../domain/format";
import { canWriteGarage } from "../domain/roles";
import { DetailRow, GaragePageHeader } from "./common";

export function PartDetailPage({
  teamId,
  partId,
  role,
  repository,
}: {
  teamId: string;
  partId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const part = useQuery(partQueryOptions(repository, teamId, partId));
  const canWrite = canWriteGarage(role);

  if (part.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (part.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => void part.refetch()}
      />
    );
  }
  if (!part.data) {
    return <EmptyState title={t("garage.notFound")} body={t("garage.errorBody")} />;
  }

  const value = part.data;
  return (
    <>
      <GaragePageHeader
        title={value.description}
        subtitle={t(`garage.kinds.${value.kind}`)}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/parts/$partId/edit"
              params={{ teamId, partId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl border border-line bg-surface-2 px-4 text-sm font-semibold text-fg"
            >
              {t("garage.actions.edit")}
            </Link>
          ) : null
        }
      />
      <Card>
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-base font-semibold">{t("garage.sections.stock")}</h2>
          {value.belowMin ? <Badge tone="warn">{t("garage.belowMinFilter")}</Badge> : null}
        </div>
        <p className="text-3xl font-black tabular-nums text-fg">
          {formatQuantity(value.qtyCenti, value.unit, i18n.language)}
        </p>
        <p className="mt-1 text-sm text-muted">
          {t("garage.part.minStock", {
            min: formatQuantity(value.minStockCenti, value.unit, i18n.language),
          })}
        </p>
        <dl className="mt-5">
          <DetailRow label={t("garage.fields.kind")} value={t(`garage.kinds.${value.kind}`)} />
          <DetailRow label={t("garage.fields.brand")} value={value.brand ?? "—"} />
          <DetailRow label={t("garage.fields.reference")} value={value.reference ?? "—"} />
          <DetailRow label={t("garage.fields.unit")} value={t(`garage.units.${value.unit}`)} />
          <DetailRow
            label={t("garage.fields.serviceLife")}
            value={value.hasServiceLife ? t("garage.fields.serviceLife") : "—"}
          />
        </dl>
      </Card>
    </>
  );
}
