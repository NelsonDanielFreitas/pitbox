import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Alert } from "@/shared/ui/Alert";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import {
  carsQueryOptions,
  engineQueryOptions,
  enginesQueryOptions,
  mountsQueryOptions,
} from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatDate, formatDateTime } from "../domain/format";
import { canWriteGarage } from "../domain/roles";
import type { EngineWithMount } from "../domain/models";
import { DetailRow, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";
import { PickCarSheet } from "./PickCarSheet";
import { RetireDialog } from "./RetireDialog";
import { AssetStatusBadge, EngineMountBadge } from "./status";

function engineInput(engine: EngineWithMount) {
  return {
    make: engine.make,
    model: engine.model,
    serialNumber: engine.serialNumber,
    purchasedOn: engine.purchasedOn,
    status: engine.status,
    notes: engine.notes,
  };
}

export function EngineDetailPage({
  teamId,
  engineId,
  role,
  repository,
}: {
  teamId: string;
  engineId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const engine = useQuery(engineQueryOptions(repository, teamId, engineId));
  const mounts = useQuery(mountsQueryOptions(repository, teamId, engineId));
  const cars = useQuery(carsQueryOptions(repository, teamId));
  const engines = useQuery(enginesQueryOptions(repository, teamId));
  const [pickerOpen, setPickerOpen] = useState(false);
  const [retireOpen, setRetireOpen] = useState(false);
  const [error, setError] = useState<string>();
  const canWrite = canWriteGarage(role);

  if (engine.isPending || mounts.isPending || cars.isPending || engines.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (engine.isError || mounts.isError || cars.isError || engines.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => {
          void engine.refetch();
          void mounts.refetch();
          void cars.refetch();
          void engines.refetch();
        }}
      />
    );
  }
  if (!engine.data) {
    return <EmptyState title={t("garage.notFound")} body={t("garage.errorBody")} />;
  }

  const value = engine.data;
  const currentMount = value.currentCar;
  const occupiedCarIds = new Set(
    engines.data
      .filter((candidate) => candidate.id !== value.id && candidate.currentCar !== null)
      .map((candidate) => candidate.currentCar!.id),
  );

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
  };

  const moveOrMount = async (carId: string) => {
    setError(undefined);
    try {
      if (currentMount) {
        await repository.moveEngine(teamId, value.id, carId);
      } else {
        await repository.mountEngine(teamId, value.id, carId);
      }
      setPickerOpen(false);
      await refresh();
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
    }
  };

  const unmount = async () => {
    setError(undefined);
    try {
      await repository.unmountEngine(teamId, value.id);
      await refresh();
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
    }
  };

  const retire = async () => {
    if (currentMount) {
      setRetireOpen(true);
      return;
    }
    setError(undefined);
    try {
      await repository.updateEngine(teamId, value.id, value.localRev, {
        ...engineInput(value),
        status: "retired",
      });
      await refresh();
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
    }
  };

  return (
    <>
      <GaragePageHeader
        title={`${value.make} ${value.model}`}
        subtitle={t("garage.engine.detailTitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/engines/$engineId/edit"
              params={{ teamId, engineId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl border border-line bg-surface-2 px-4 text-sm font-semibold text-fg"
            >
              {t("garage.actions.edit")}
            </Link>
          ) : null
        }
      />
      {error ? (
        <Alert tone="crit" className="mb-4">
          {error}
        </Alert>
      ) : null}

      <Card className="mb-4">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-base font-semibold text-fg">{t("garage.sections.engine")}</h2>
          {value.status === "retired" ? (
            <AssetStatusBadge status="retired" />
          ) : (
            <EngineMountBadge mounted={currentMount !== null} />
          )}
        </div>
        {currentMount ? (
          <p className="text-sm text-muted">
            {t("garage.engine.mountedIn", {
              name: currentMount.nickname ?? `${currentMount.make} ${currentMount.model}`,
            })}
          </p>
        ) : (
          <p className="text-sm text-muted">{t("garage.engine.available")}</p>
        )}
        {canWrite && value.status !== "retired" ? (
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={() => setPickerOpen(true)}>
              {currentMount ? t("garage.actions.move") : t("garage.actions.mount")}
            </Button>
            {currentMount ? (
              <Button variant="secondary" onClick={() => void unmount()}>
                {t("garage.actions.unmount")}
              </Button>
            ) : null}
            <Button variant="danger" onClick={() => void retire()}>
              {t("garage.actions.retire")}
            </Button>
          </div>
        ) : null}
      </Card>

      <Card className="mb-4">
        <h2 className="mb-2 text-base font-semibold text-fg">{t("garage.sections.details")}</h2>
        <dl>
          <DetailRow label={t("garage.fields.make")} value={value.make} />
          <DetailRow label={t("garage.fields.model")} value={value.model} />
          <DetailRow label={t("garage.fields.serialNumber")} value={value.serialNumber ?? "—"} />
          <DetailRow
            label={t("garage.fields.purchasedOn")}
            value={formatDate(value.purchasedOn, i18n.language)}
          />
          <DetailRow label={t("garage.fields.notes")} value={value.notes ?? "—"} />
          {value.sessionsTotal === 0 ? (
            <DetailRow label={t("garage.fields.usage")} value={t("garage.engine.noSessions")} />
          ) : (
            <>
              <DetailRow
                label={t("garage.engine.sessionsLabel")}
                value={String(value.sessionsTotal)}
              />
              <DetailRow label={t("garage.engine.tanksLabel")} value={String(value.tanksTotal)} />
              <DetailRow
                label={t("garage.engine.runTimeLabel")}
                value={`${Math.round(value.runSecondsTotal / 60)} min`}
              />
            </>
          )}
        </dl>
      </Card>

      <Card>
        <h2 className="mb-3 text-base font-semibold text-fg">{t("garage.sections.history")}</h2>
        {mounts.data.length === 0 ? (
          <p className="text-sm text-muted">{t("garage.engine.noMounts")}</p>
        ) : (
          <ol className="space-y-3">
            {mounts.data.map((mount) => (
              <li key={mount.id} className="flex gap-3">
                <span
                  aria-hidden="true"
                  className={
                    mount.unmountedAt === null
                      ? "mt-1.5 size-2.5 shrink-0 rounded-full bg-ok"
                      : "mt-1.5 size-2.5 shrink-0 rounded-full border border-line bg-surface-2"
                  }
                />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-semibold text-fg">
                    {mount.car.nickname ?? `${mount.car.make} ${mount.car.model}`}
                  </p>
                  <p className="text-xs text-muted">
                    {formatDateTime(mount.mountedAt, i18n.language)}
                    {mount.unmountedAt
                      ? ` → ${formatDateTime(mount.unmountedAt, i18n.language)}`
                      : ` · ${t("garage.statuses.mounted")}`}
                  </p>
                </div>
                {mount.unmountedAt === null ? <Badge tone="ok">{t("garage.statuses.mounted")}</Badge> : null}
              </li>
            ))}
          </ol>
        )}
      </Card>

      <PickCarSheet
        open={pickerOpen}
        cars={cars.data}
        currentCarId={currentMount?.id}
        occupiedCarIds={occupiedCarIds}
        loading={false}
        onSelect={(carId) => void moveOrMount(carId)}
        onClose={() => setPickerOpen(false)}
      />
      <RetireDialog
        open={retireOpen}
        type="engine"
        onClose={() => setRetireOpen(false)}
        onUnmount={() => {
          setRetireOpen(false);
          void unmount();
        }}
      />
    </>
  );
}
