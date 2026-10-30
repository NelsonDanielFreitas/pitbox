import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import type { TeamRole } from "@/features/teams/domain/team";
import { Alert } from "@/shared/ui/Alert";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Dialog } from "@/shared/ui/Dialog";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { partQueryOptions, stockMovementsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatDate, formatQuantity } from "../domain/format";
import type { StockMovement } from "../domain/models";
import { canSeeCosts, canWriteGarage } from "../domain/roles";
import { specFieldsFor } from "../domain/specs";
import { canUndo } from "../domain/stock";
import { todayIso } from "../domain/today";
import { DetailRow, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";
import { MountsSection } from "./MountsSection";
import { MovementSheet } from "./MovementSheet";

const euros = (cents: number, locale: string) =>
  new Intl.NumberFormat(locale, { style: "currency", currency: "EUR" }).format(cents / 100);

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
  const costs = canSeeCosts(role);
  const part = useQuery(partQueryOptions(repository, teamId, partId, { costs }));
  const movements = useQuery(stockMovementsQueryOptions(repository, teamId, partId, { costs }));
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const canWrite = canWriteGarage(role);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [movementOpen, setMovementOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const undo = async (movement: StockMovement) => {
    setActionError(null);
    try {
      await repository.undoMovement(teamId, movement.id, {
        occurredOn: todayIso(),
        note: t("garage.movement.undoNote", { date: formatDate(movement.occurredAt, i18n.language) }),
        createdBy: userId,
      });
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
    } catch (error) {
      setActionError(garageErrorMessage(t, error));
    }
  };

  const archive = async (localRev: number) => {
    setActionError(null);
    try {
      await repository.archivePart(teamId, partId, localRev);
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      await navigate({ to: "/teams/$teamId/garage/parts", params: { teamId } });
    } catch (error) {
      setArchiveOpen(false);
      setActionError(garageErrorMessage(t, error));
    }
  };

  const author = (movement: StockMovement) =>
    movement.createdBy === null
      ? t("garage.movement.authorUnknown")
      : movement.createdBy === userId
        ? t("garage.movement.authorSelf")
        : t("garage.movement.authorOther");

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
          {value.reorderToCenti !== null ? (
            <DetailRow
              label={t("garage.fields.reorderTo")}
              value={formatQuantity(value.reorderToCenti, value.unit, i18n.language)}
            />
          ) : null}
          {specFieldsFor(value.kind).flatMap((field) => {
            const spec = value.specs[field.key];
            return spec === undefined
              ? []
              : [
                  <DetailRow
                    key={field.key}
                    label={t(`garage.fields.spec.${field.key}`)}
                    value={`${String(spec)}${field.unit ? ` ${field.unit}` : ""}`}
                  />,
                ];
          })}
          {costs && value.unitCostCents != null ? (
            <>
              <DetailRow
                label={t("garage.partDetail.cost")}
                value={euros(value.unitCostCents, i18n.language)}
              />
              <DetailRow
                label={t("garage.partDetail.stockValue")}
                value={euros(Math.round((value.unitCostCents * value.qtyCenti) / 100), i18n.language)}
              />
            </>
          ) : null}
        </dl>
        {canWrite ? (
          <div className="mt-5 flex flex-wrap gap-2">
            <Button onClick={() => setMovementOpen(true)}>{t("garage.movement.add")}</Button>
            <Button variant="secondary" onClick={() => setArchiveOpen(true)}>
              {t("garage.partDetail.archive")}
            </Button>
          </div>
        ) : null}
      </Card>
      {actionError ? (
        <div className="mt-4">
          <Alert tone="crit">{actionError}</Alert>
        </div>
      ) : null}
      <section aria-labelledby="part-movements" className="mt-6">
        <h2 id="part-movements" className="mb-2 text-base font-semibold">
          {t("garage.movement.history")}
        </h2>
        {movements.data && movements.data.length > 0 ? (
          <ul className="grid gap-2">
            {movements.data.map((movement) => (
              <li key={movement.id} className="flex items-center gap-3 rounded-2xl border border-line bg-surface p-4">
                <div className="min-w-0 flex-1">
                  <p className="font-semibold text-fg">
                    {t(`garage.movement.reasons.${movement.reason}`)}
                    {movement.undone ? (
                      <span className="ml-2 text-xs font-normal text-muted">
                        {t("garage.movement.undone")}
                      </span>
                    ) : null}
                  </p>
                  <p className="truncate text-xs text-muted">
                    {[formatDate(movement.occurredAt, i18n.language), author(movement), movement.store, movement.note]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                  {costs && movement.unitCostCents != null ? (
                    <p className="text-xs text-muted">{euros(movement.unitCostCents, i18n.language)}</p>
                  ) : null}
                </div>
                <p className="shrink-0 font-bold tabular-nums text-fg">
                  {movement.qtyDeltaCenti > 0 ? "+" : ""}
                  {formatQuantity(movement.qtyDeltaCenti, value.unit, i18n.language)}
                </p>
                {canWrite && canUndo(movement) ? (
                  <Button variant="secondary" onClick={() => void undo(movement)}>
                    {t("garage.movement.undo")}
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted">{t("garage.movement.historyEmpty")}</p>
        )}
      </section>
      {value.hasServiceLife ? (
        <MountsSection
          teamId={teamId}
          repository={repository}
          subject={{ kind: "part", partId }}
          canWrite={canWrite}
        />
      ) : null}
      {canWrite ? (
        <>
          <MovementSheet
            open={movementOpen}
            onClose={() => setMovementOpen(false)}
            teamId={teamId}
            part={value}
            repository={repository}
            userId={userId}
            showCosts={costs}
          />
          <Dialog
            open={archiveOpen}
            title={t("garage.partDetail.archiveTitle")}
            description={t("garage.partDetail.archiveBody")}
            onClose={() => setArchiveOpen(false)}
          >
            <div className="flex gap-2">
              <Button variant="secondary" full onClick={() => setArchiveOpen(false)}>
                {t("garage.actions.cancel")}
              </Button>
              <Button full onClick={() => void archive(value.localRev)}>
                {t("garage.partDetail.archiveConfirm")}
              </Button>
            </div>
          </Dialog>
        </>
      ) : null}
    </>
  );
}
