import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { tyreSetQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatDate } from "../domain/format";
import { canWriteGarage } from "../domain/roles";
import { DetailRow, GaragePageHeader } from "./common";
import { TyreStatusBadge } from "./status";

export function TyreDetailPage({
  teamId,
  tyreSetId,
  role,
  repository,
}: {
  teamId: string;
  tyreSetId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const tyre = useQuery(tyreSetQueryOptions(repository, teamId, tyreSetId));
  const canWrite = canWriteGarage(role);

  if (tyre.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (tyre.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => void tyre.refetch()}
      />
    );
  }
  if (!tyre.data) {
    return <EmptyState title={t("garage.notFound")} body={t("garage.errorBody")} />;
  }

  const value = tyre.data;
  return (
    <>
      <GaragePageHeader
        title={`${value.label} · ${value.make} ${value.model}`}
        subtitle={t("garage.tyre.detailTitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/tyres/$tyreSetId/edit"
              params={{ teamId, tyreSetId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl border border-line bg-surface-2 px-4 text-sm font-semibold text-fg"
            >
              {t("garage.actions.edit")}
            </Link>
          ) : null
        }
      />
      <Card>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-base font-semibold">{t("garage.sections.details")}</h2>
          <TyreStatusBadge status={value.status} />
        </div>
        <dl>
          <DetailRow label={t("garage.fields.label")} value={value.label} />
          <DetailRow label={t("garage.fields.make")} value={value.make} />
          <DetailRow label={t("garage.fields.model")} value={value.model} />
          <DetailRow label={t("garage.fields.compound")} value={value.compound ?? "—"} />
          <DetailRow label={t("garage.fields.insertType")} value={value.insertType ?? "—"} />
          <DetailRow
            label={t("garage.fields.gluedOn")}
            value={formatDate(value.gluedOn, i18n.language)}
          />
          <DetailRow label={t("garage.fields.wearNotes")} value={value.wearNotes ?? "—"} />
          {value.sessionsTotal === 0 ? (
            <DetailRow label={t("garage.fields.usage")} value={t("garage.tyre.noSessions")} />
          ) : (
            <DetailRow
              label={t("garage.tyre.sessionsLabel")}
              value={String(value.sessionsTotal)}
            />
          )}
        </dl>
      </Card>
    </>
  );
}
