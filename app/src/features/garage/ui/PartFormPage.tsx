import { useEffect, useRef } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Button } from "@/shared/ui/Button";
import { ErrorState } from "@/shared/ui/ErrorState";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { fieldDescribedBy } from "@/shared/ui/form-errors";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Spinner } from "@/shared/ui/Spinner";
import { TextInput } from "@/shared/ui/TextInput";

import { partQueryOptions, partsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { centiToInput } from "../domain/format";
import { canWriteGarage } from "../domain/roles";
import {
  partFormSchema,
  type PartFormOutput,
  type PartFormValues,
} from "../domain/schemas";
import { GarageForbidden, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";

const emptyValues: PartFormValues = {
  kind: "glow_plug",
  brand: "",
  reference: "",
  description: "",
  unit: "un",
  minStock: "0",
  initialQty: "",
  hasServiceLife: false,
};

export function PartFormPage({
  teamId,
  role,
  repository,
  partId,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
  partId?: string;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canWrite = canWriteGarage(role);
  const isEdit = partId !== undefined;
  const part = useQuery({
    ...partQueryOptions(repository, teamId, partId ?? ""),
    enabled: isEdit,
  });
  const parts = useQuery({
    ...partsQueryOptions(repository, teamId),
    enabled: !isEdit,
  });
  const {
    register,
    handleSubmit,
    reset,
    setError,
    control,
    formState: { errors, isSubmitting },
  } = useForm<PartFormValues, unknown, PartFormOutput>({
    resolver: zodResolver(partFormSchema),
    defaultValues: emptyValues,
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });
  const unit = useWatch({ control, name: "unit" });
  const loadedRef = useRef<string | undefined>(undefined);
  const prefilledRef = useRef(false);

  useEffect(() => {
    if (part.data && loadedRef.current !== part.data.id) {
      loadedRef.current = part.data.id;
      reset({
        kind: part.data.kind,
        brand: part.data.brand ?? "",
        reference: part.data.reference ?? "",
        description: part.data.description,
        unit: part.data.unit,
        minStock: centiToInput(part.data.minStockCenti, part.data.unit),
        initialQty: "",
        hasServiceLife: part.data.hasServiceLife,
      });
    }
  }, [part.data, reset]);

  useEffect(() => {
    const previous = parts.data?.find((candidate) => candidate.brand);
    if (!isEdit && previous && !prefilledRef.current) {
      prefilledRef.current = true;
      reset({ ...emptyValues, brand: previous.brand ?? "" });
    }
  }, [parts.data, isEdit, reset]);

  if (!canWrite) return <GarageForbidden />;
  if (isEdit && part.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (isEdit && (part.isError || !part.data)) {
    return (
      <ErrorState
        title={t("garage.notFound")}
        body={t("garage.errorBody")}
        onRetry={() => void part.refetch()}
      />
    );
  }

  const onSubmit = async (values: PartFormOutput) => {
    try {
      const saved = partId
        ? await repository.updatePart(teamId, partId, part.data?.localRev ?? 0, {
            kind: values.kind,
            brand: values.brand,
            reference: values.reference,
            description: values.description,
            unit: values.unit,
            minStockCenti: values.minStock,
            hasServiceLife: values.hasServiceLife,
          })
        : await repository.createPart(teamId, {
            kind: values.kind,
            brand: values.brand,
            reference: values.reference,
            description: values.description,
            unit: values.unit,
            minStockCenti: values.minStock,
            initialQtyCenti: values.initialQty,
            hasServiceLife: values.hasServiceLife,
          });
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      await navigate({
        to: "/teams/$teamId/garage/parts/$partId",
        params: { teamId, partId: saved.id },
      });
    } catch (error) {
      setError("root", { message: garageErrorMessage(t, error) });
    }
  };

  return (
    <>
      <GaragePageHeader
        title={isEdit ? t("garage.part.editTitle") : t("garage.part.newTitle")}
        subtitle={t("garage.subtitle")}
      />
      <form noValidate onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <FormRootError error={errors.root?.message} />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField id="part-kind" label={t("garage.fields.kind")} error={errors.kind?.message}>
            <SelectInput
              id="part-kind"
              invalid={Boolean(errors.kind)}
              aria-describedby={fieldDescribedBy("part-kind", errors.kind?.message, undefined)}
              {...register("kind")}
            >
              {(["glow_plug", "clutch_shoe", "clutch_spring", "clutch_bell", "bearing", "conrod", "piston_sleeve", "air_filter", "shock_oil", "diff_oil", "fuel", "spring", "tyre", "insert", "body", "wing", "other"] as const).map((kind) => (
                <option key={kind} value={kind}>
                  {t(`garage.kinds.${kind}`)}
                </option>
              ))}
            </SelectInput>
          </FormField>
          <FormField id="part-unit" label={t("garage.fields.unit")} error={errors.unit?.message}>
            <SelectInput
              id="part-unit"
              invalid={Boolean(errors.unit)}
              aria-describedby={fieldDescribedBy("part-unit", errors.unit?.message, undefined)}
              {...register("unit")}
            >
              {(["un", "ml", "l", "g"] as const).map((value) => (
                <option key={value} value={value}>
                  {t(`garage.units.${value}`)}
                </option>
              ))}
            </SelectInput>
          </FormField>
          <FormField id="part-description" label={t("garage.fields.description")} error={errors.description?.message} className="sm:col-span-2">
            <TextInput
              id="part-description"
              invalid={Boolean(errors.description)}
              aria-describedby={fieldDescribedBy("part-description", errors.description?.message, undefined)}
              {...register("description")}
            />
          </FormField>
          <FormField id="part-brand" label={t("garage.fields.brand")} error={errors.brand?.message}>
            <TextInput
              id="part-brand"
              invalid={Boolean(errors.brand)}
              aria-describedby={fieldDescribedBy("part-brand", errors.brand?.message, undefined)}
              {...register("brand")}
            />
          </FormField>
          <FormField id="part-reference" label={t("garage.fields.reference")} error={errors.reference?.message}>
            <TextInput
              id="part-reference"
              invalid={Boolean(errors.reference)}
              aria-describedby={fieldDescribedBy("part-reference", errors.reference?.message, undefined)}
              {...register("reference")}
            />
          </FormField>
          <FormField
            id="part-min"
            label={t("garage.fields.minStock")}
            unit={t(`garage.units.${unit}`)}
            error={errors.minStock?.message}
          >
            <TextInput
              id="part-min"
              inputMode="decimal"
              invalid={Boolean(errors.minStock)}
              aria-describedby={fieldDescribedBy("part-min", errors.minStock?.message, undefined)}
              {...register("minStock")}
            />
          </FormField>
          {!isEdit ? (
            <FormField
              id="part-initial"
              label={t("garage.fields.initialQty")}
              unit={t(`garage.units.${unit}`)}
              error={errors.initialQty?.message}
            >
              <TextInput
                id="part-initial"
                inputMode="decimal"
                invalid={Boolean(errors.initialQty)}
                aria-describedby={fieldDescribedBy("part-initial", errors.initialQty?.message, undefined)}
                {...register("initialQty")}
              />
            </FormField>
          ) : null}
        </div>
        <div className="flex min-h-12 items-center gap-3 rounded-xl border border-line bg-surface px-4">
          <input
            id="part-service"
            type="checkbox"
            className="size-5 accent-[var(--primary)]"
            {...register("hasServiceLife")}
          />
          <label htmlFor="part-service" className="text-sm font-semibold text-fg">
            {t("garage.fields.serviceLife")}
          </label>
        </div>
        <p className="text-xs text-muted">{t("garage.fields.serviceLifeHint")}</p>
        <FormActions>
          <Button variant="secondary" full asChild>
            <Link to="/teams/$teamId/garage/parts" params={{ teamId }}>
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
