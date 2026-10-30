import { useEffect, useRef } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
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
import { centiToInput, unitLabel } from "../domain/format";
import { canSeeCosts, canWriteGarage } from "../domain/roles";
import { parseSpecInputs, specFieldsFor, specInputsFrom } from "../domain/specs";
import {
  partFormSchema,
  type PartFormOutput,
  type PartFormValues,
} from "../domain/schemas";
import { GarageForbidden, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";

const emptyValues = (description = ""): PartFormValues => ({
  kind: "glow_plug",
  brand: "",
  reference: "",
  description,
  unit: "un",
  minStock: "0",
  reorderTo: "",
  specs: {},
  unitCost: "",
  initialQty: "",
  hasServiceLife: false,
});

export function PartFormPage({
  teamId,
  role,
  repository,
  partId,
  prefillDescription,
  thenShopping = false,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
  partId?: string;
  /** Description typed in the shopping list search, when the part does not exist yet. */
  prefillDescription?: string | undefined;
  /** After creating, put the new part on the shopping list and go back to it. */
  thenShopping?: boolean;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canWrite = canWriteGarage(role);
  const costs = canSeeCosts(role);
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const isEdit = partId !== undefined;
  const part = useQuery({
    ...partQueryOptions(repository, teamId, partId ?? "", { costs }),
    enabled: isEdit,
  });
  const parts = useQuery({
    ...partsQueryOptions(repository, teamId, { costs }),
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
    defaultValues: emptyValues(prefillDescription),
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });
  const unit = useWatch({ control, name: "unit" });
  const kind = useWatch({ control, name: "kind" });
  const specFields = specFieldsFor(kind);
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
        reorderTo:
          part.data.reorderToCenti === null
            ? ""
            : centiToInput(part.data.reorderToCenti, part.data.unit),
        specs: specInputsFrom(part.data.kind, part.data.specs),
        unitCost:
          part.data.unitCostCents == null
            ? ""
            : (part.data.unitCostCents / 100).toFixed(2).replace(".", ","),
        initialQty: "",
        hasServiceLife: part.data.hasServiceLife,
      });
    }
  }, [part.data, reset]);

  useEffect(() => {
    const previous = parts.data?.find((candidate) => candidate.brand);
    if (!isEdit && previous && !prefilledRef.current) {
      prefilledRef.current = true;
      reset({ ...emptyValues(prefillDescription), brand: previous.brand ?? "" });
    }
  }, [parts.data, isEdit, reset, prefillDescription]);

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
      // Spec inputs of another kind are dropped; the form schema already validated the rest.
      const parsed = parseSpecInputs(values.kind, values.specs);
      const base = {
        kind: values.kind,
        brand: values.brand,
        reference: values.reference,
        description: values.description,
        unit: values.unit,
        specs: parsed.specs ?? {},
        minStockCenti: values.minStock,
        reorderToCenti: values.reorderTo,
        hasServiceLife: values.hasServiceLife,
        // `undefined` keeps the stored cost for readers who cannot see it.
        ...(costs ? { unitCostCents: values.unitCost } : {}),
      };
      const saved = partId
        ? await repository.updatePart(teamId, partId, part.data?.localRev ?? 0, base)
        : await repository.createPart(teamId, {
            ...base,
            initialQtyCenti: values.initialQty,
            createdBy: userId,
          });
      if (thenShopping && !partId) {
        // A new part has no stock, so it needs an explicit quantity: one unit to start.
        await repository.setShoppingItem(teamId, saved.id, 100);
      }
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      if (thenShopping && !partId) {
        await navigate({ to: "/teams/$teamId/garage/shopping", params: { teamId } });
        return;
      }
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
            unit={unitLabel(unit)}
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
          <FormField
            id="part-reorder"
            label={t("garage.fields.reorderTo")}
            unit={unitLabel(unit)}
            hint={t("garage.fields.reorderToHint")}
            error={errors.reorderTo?.message}
          >
            <TextInput
              id="part-reorder"
              inputMode="decimal"
              invalid={Boolean(errors.reorderTo)}
              aria-describedby={fieldDescribedBy("part-reorder", errors.reorderTo?.message, t("garage.fields.reorderToHint"))}
              {...register("reorderTo")}
            />
          </FormField>
          {costs ? (
            <FormField
              id="part-cost"
              label={t("garage.fields.unitCost")}
              unit="€"
              error={errors.unitCost?.message}
            >
              <TextInput
                id="part-cost"
                inputMode="decimal"
                invalid={Boolean(errors.unitCost)}
                aria-describedby={fieldDescribedBy("part-cost", errors.unitCost?.message, undefined)}
                {...register("unitCost")}
              />
            </FormField>
          ) : null}
          {specFields.map((field) => {
            const message = errors.specs?.[field.key]?.message;
            const id = `part-spec-${field.key}`;
            return (
              <FormField
                key={`${kind}-${field.key}`}
                id={id}
                label={t(`garage.fields.spec.${field.key}`)}
                unit={field.unit}
                error={message}
              >
                <TextInput
                  id={id}
                  inputMode={field.type === "number" ? "decimal" : "text"}
                  invalid={Boolean(message)}
                  aria-describedby={fieldDescribedBy(id, message, undefined)}
                  {...register(`specs.${field.key}`)}
                />
              </FormField>
            );
          })}
          {!isEdit ? (
            <FormField
              id="part-initial"
              label={t("garage.fields.initialQty")}
              unit={unitLabel(unit)}
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
