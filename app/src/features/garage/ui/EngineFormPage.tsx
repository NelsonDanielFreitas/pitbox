import { useEffect, useRef } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Button } from "@/shared/ui/Button";
import { ErrorState } from "@/shared/ui/ErrorState";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { fieldDescribedBy } from "@/shared/ui/form-errors";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Spinner } from "@/shared/ui/Spinner";
import { TextArea } from "@/shared/ui/TextArea";
import { TextInput } from "@/shared/ui/TextInput";

import { engineQueryOptions, enginesQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { canWriteGarage } from "../domain/roles";
import {
  engineFormSchema,
  type EngineFormOutput,
  type EngineFormValues,
} from "../domain/schemas";
import { GarageForbidden, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";

const emptyValues: EngineFormValues = {
  make: "",
  model: "",
  serialNumber: "",
  purchasedOn: "",
  status: "active",
  notes: "",
};

export function EngineFormPage({
  teamId,
  role,
  repository,
  engineId,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
  engineId?: string;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canWrite = canWriteGarage(role);
  const isEdit = engineId !== undefined;
  const engine = useQuery({
    ...engineQueryOptions(repository, teamId, engineId ?? ""),
    enabled: isEdit,
  });
  const engines = useQuery({
    ...enginesQueryOptions(repository, teamId),
    enabled: !isEdit,
  });
  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<EngineFormValues, unknown, EngineFormOutput>({
    resolver: zodResolver(engineFormSchema),
    defaultValues: emptyValues,
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });
  const loadedRef = useRef<string | undefined>(undefined);
  const prefilledRef = useRef(false);

  useEffect(() => {
    if (engine.data && loadedRef.current !== engine.data.id) {
      loadedRef.current = engine.data.id;
      reset({
        make: engine.data.make,
        model: engine.data.model,
        serialNumber: engine.data.serialNumber ?? "",
        purchasedOn: engine.data.purchasedOn ?? "",
        status: engine.data.status,
        notes: engine.data.notes ?? "",
      });
    }
  }, [engine.data, reset]);

  useEffect(() => {
    const previous = engines.data?.[0];
    if (!isEdit && previous && !prefilledRef.current) {
      prefilledRef.current = true;
      reset({ ...emptyValues, make: previous.make });
    }
  }, [engines.data, isEdit, reset]);

  if (!canWrite) return <GarageForbidden />;
  if (isEdit && engine.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (isEdit && (engine.isError || !engine.data)) {
    return (
      <ErrorState
        title={t("garage.notFound")}
        body={t("garage.errorBody")}
        onRetry={() => void engine.refetch()}
      />
    );
  }

  const onSubmit = async (values: EngineFormOutput) => {
    try {
      const saved = engineId
        ? await repository.updateEngine(
            teamId,
            engineId,
            engine.data?.localRev ?? 0,
            values,
          )
        : await repository.createEngine(teamId, values);
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      await navigate({
        to: "/teams/$teamId/garage/engines/$engineId",
        params: { teamId, engineId: saved.id },
      });
    } catch (error) {
      setError("root", { message: garageErrorMessage(t, error) });
    }
  };

  return (
    <>
      <GaragePageHeader
        title={isEdit ? t("garage.engine.editTitle") : t("garage.engine.newTitle")}
        subtitle={t("garage.subtitle")}
      />
      <form noValidate onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <FormRootError error={errors.root?.message} />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField id="engine-make" label={t("garage.fields.make")} error={errors.make?.message}>
            <TextInput
              id="engine-make"
              invalid={Boolean(errors.make)}
              aria-describedby={fieldDescribedBy("engine-make", errors.make?.message, undefined)}
              {...register("make")}
            />
          </FormField>
          <FormField id="engine-model" label={t("garage.fields.model")} error={errors.model?.message}>
            <TextInput
              id="engine-model"
              invalid={Boolean(errors.model)}
              aria-describedby={fieldDescribedBy("engine-model", errors.model?.message, undefined)}
              {...register("model")}
            />
          </FormField>
          <FormField id="engine-status" label={t("garage.fields.status")} error={errors.status?.message}>
            <SelectInput
              id="engine-status"
              invalid={Boolean(errors.status)}
              aria-describedby={fieldDescribedBy("engine-status", errors.status?.message, undefined)}
              {...register("status")}
            >
              <option value="active">{t("garage.statuses.active")}</option>
              <option value="spare">{t("garage.statuses.spare")}</option>
              <option value="retired">{t("garage.statuses.retired")}</option>
            </SelectInput>
          </FormField>
        </div>
        <details className="rounded-2xl border border-line bg-surface p-4">
          <summary className="flex min-h-12 cursor-pointer items-center font-semibold text-fg">
            {t("garage.actions.moreDetails")}
          </summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <FormField id="engine-serial" label={t("garage.fields.serialNumber")} error={errors.serialNumber?.message}>
              <TextInput
                id="engine-serial"
                invalid={Boolean(errors.serialNumber)}
                aria-describedby={fieldDescribedBy("engine-serial", errors.serialNumber?.message, undefined)}
                {...register("serialNumber")}
              />
            </FormField>
            <FormField id="engine-purchased" label={t("garage.fields.purchasedOn")} error={errors.purchasedOn?.message}>
              <TextInput
                id="engine-purchased"
                type="date"
                invalid={Boolean(errors.purchasedOn)}
                aria-describedby={fieldDescribedBy("engine-purchased", errors.purchasedOn?.message, undefined)}
                {...register("purchasedOn")}
              />
            </FormField>
            <FormField id="engine-notes" label={t("garage.fields.notes")} error={errors.notes?.message} className="sm:col-span-2">
              <TextArea
                id="engine-notes"
                invalid={Boolean(errors.notes)}
                aria-describedby={fieldDescribedBy("engine-notes", errors.notes?.message, undefined)}
                {...register("notes")}
              />
            </FormField>
          </div>
        </details>
        <FormActions>
          <Button variant="secondary" full asChild>
            <Link to="/teams/$teamId/garage/engines" params={{ teamId }}>
              {t("garage.actions.cancel")}
            </Link>
          </Button>
          <Button type="submit" full disabled={isSubmitting}>
            {isSubmitting ? t("garage.actions.saving") : t("garage.actions.save")}
          </Button>
        </FormActions>
      </form>
    </>
  );
}
