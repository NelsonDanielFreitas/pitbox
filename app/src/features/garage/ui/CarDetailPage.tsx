import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

import { carQueryOptions, enginesQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatDate, formatDateTime } from "../domain/format";
import { canWriteGarage } from "../domain/roles";
import type { CarInput } from "../domain/models";
import { GaragePageHeader, DetailRow } from "./common";
import { garageErrorMessage } from "./errors";
import { PickEngineSheet } from "./PickEngineSheet";
import { RetireDialog } from "./RetireDialog";
import { AssetStatusBadge } from "./status";

export function CarDetailPage({
  teamId,
  carId,
  role,
  repository,
}: {
  teamId: string;
  carId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const car = useQuery(carQueryOptions(repository, teamId, carId));
  const engines = useQuery(enginesQueryOptions(repository, teamId));
  const [pickerOpen, setPickerOpen] = useState(false);
  const [retireOpen, setRetireOpen] = useState(false);
  const [error, setError] = useState<string>();
  const canWrite = canWriteGarage(role);

  if (car.isPending || engines.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (car.isError || engines.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => {
          void car.refetch();
          void engines.refetch();
        }}
      />
    );
  }
  if (!car.data) {
    return <EmptyState title={t("garage.notFound")} body={t("garage.errorBody")} />;
  }

  const value = car.data;
  const currentEngine = engines.data.find((engine) => engine.currentCar?.id === value.id);
  const availableEngines = engines.data.filter(
    (engine) => engine.currentCar === null && engine.status !== "retired",
  );

  const mount = async (engineId: string) => {
    setError(undefined);
    try {
      if (currentEngine) {
        await repository.swapCarEngine(teamId, value.id, engineId);
      } else {
        await repository.mountEngine(teamId, engineId, value.id);
      }
      setPickerOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
    }
  };

  const unmount = async () => {
    if (!currentEngine) return;
    setError(undefined);
    try {
      await repository.unmountEngine(teamId, currentEngine.id);
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
      throw cause;
    }
  };

  const carInput = (status: CarInput["status"]): CarInput => ({
    make: value.make,
    model: value.model,
    modelYear: value.modelYear,
    serialNumber: value.serialNumber,
    nickname: value.nickname,
    transponderNumber: value.transponderNumber,
    status,
    notes: value.notes,
  });

  const retire = async () => {
    if (currentEngine) {
      setRetireOpen(true);
      return;
    }
    setError(undefined);
    try {
      await repository.updateCar(teamId, value.id, value.localRev, carInput("retired"));
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
    }
  };

  const retireAfterUnmount = async () => {
    setRetireOpen(false);
    try {
      if (currentEngine) await unmount();
      await repository.updateCar(teamId, value.id, value.localRev, carInput("retired"));
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
    } catch (cause) {
      setError(garageErrorMessage(t, cause));
    }
  };

  return (
    <>
      <GaragePageHeader
        title={value.nickname ?? `${value.make} ${value.model}`}
        subtitle={`${value.make} ${value.model}`}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/cars/$carId/edit"
              params={{ teamId, carId }}
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
          <AssetStatusBadge status={value.status} />
        </div>
        {currentEngine ? (
          <div className="space-y-4">
            <div>
              <p className="font-semibold text-fg">
                {currentEngine.make} {currentEngine.model}
              </p>
              <p className="text-xs text-muted">
                {currentEngine.mountedAt
                  ? formatDateTime(currentEngine.mountedAt, i18n.language)
                  : "—"}
              </p>
            </div>
            {canWrite && value.status !== "retired" ? (
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" onClick={() => setPickerOpen(true)}>
                  {t("garage.actions.change")}
                </Button>
                <Button variant="secondary" onClick={() => void unmount()}>
                  {t("garage.actions.unmount")}
                </Button>
                <Button variant="danger" onClick={() => void retire()}>
                  {t("garage.actions.retire")}
                </Button>
              </div>
            ) : null}
          </div>
        ) : (
          <div className="space-y-4">
            <p className="text-sm text-muted">{t("garage.car.noEngine")}</p>
            {canWrite && value.status !== "retired" ? (
              <div className="flex flex-wrap gap-2">
                <Button onClick={() => setPickerOpen(true)}>{t("garage.actions.mount")}</Button>
                <Button variant="danger" onClick={() => void retire()}>
                  {t("garage.actions.retire")}
                </Button>
              </div>
            ) : null}
          </div>
        )}
      </Card>

      <Card>
        <h2 className="mb-2 text-base font-semibold text-fg">{t("garage.sections.details")}</h2>
        <dl>
          <DetailRow label={t("garage.fields.make")} value={value.make} />
          <DetailRow label={t("garage.fields.model")} value={value.model} />
          <DetailRow label={t("garage.fields.nickname")} value={value.nickname ?? "—"} />
          <DetailRow
            label={t("garage.fields.transponderNumber")}
            value={value.transponderNumber ?? "—"}
          />
          <DetailRow
            label={t("garage.fields.modelYear")}
            value={value.modelYear?.toString() ?? "—"}
          />
          <DetailRow label={t("garage.fields.serialNumber")} value={value.serialNumber ?? "—"} />
          <DetailRow label={t("garage.fields.notes")} value={value.notes ?? "—"} />
          <DetailRow
            label={t("garage.fields.scale")}
            value={`${value.scale} · ${value.powertrain}`}
          />
          <DetailRow label={t("garage.fields.updatedAt")} value={formatDate(value.updatedAt, i18n.language)} />
        </dl>
      </Card>

      <RetireDialog
        open={retireOpen}
        type="car"
        onClose={() => setRetireOpen(false)}
        onUnmount={() => void retireAfterUnmount()}
      />
      <PickEngineSheet
        open={pickerOpen}
        engines={availableEngines}
        loading={false}
        onSelect={(engineId) => void mount(engineId)}
        onClose={() => setPickerOpen(false)}
      />
    </>
  );
}
