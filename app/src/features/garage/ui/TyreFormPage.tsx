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

import { tyreSetQueryOptions, tyreSetsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { canWriteGarage } from "../domain/roles";
import {
  tyreFormSchema,
  type TyreFormOutput,
  type TyreFormValues,
} from "../domain/schemas";
import { GarageForbidden, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";

function today(): string {
  const date = new Date();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

const emptyValues: TyreFormValues = {
  label: "",
  make: "",
  model: "",
  compound: "",
  insertType: "",
  gluedOn: today(),
  status: "new",
  wearNotes: "",
};

export function TyreFormPage({
  teamId,
  role,
  repository,
  tyreSetId,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
  tyreSetId?: string;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canWrite = canWriteGarage(role);
  const isEdit = tyreSetId !== undefined;
  const tyre = useQuery({
    ...tyreSetQueryOptions(repository, teamId, tyreSetId ?? ""),
    enabled: isEdit,
  });
  const tyres = useQuery({
    ...tyreSetsQueryOptions(repository, teamId),
    enabled: !isEdit,
  });
  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<TyreFormValues, unknown, TyreFormOutput>({
    resolver: zodResolver(tyreFormSchema),
    defaultValues: emptyValues,
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });
  const loadedRef = useRef<string | undefined>(undefined);
  const prefilledRef = useRef(false);

  useEffect(() => {
    if (tyre.data && loadedRef.current !== tyre.data.id) {
      loadedRef.current = tyre.data.id;
      reset({
        label: tyre.data.label,
        make: tyre.data.make,
        model: tyre.data.model,
        compound: tyre.data.compound ?? "",
        insertType: tyre.data.insertType ?? "",
        gluedOn: tyre.data.gluedOn ?? "",
        status: tyre.data.status,
        wearNotes: tyre.data.wearNotes ?? "",
      });
    }
  }, [tyre.data, reset]);

  useEffect(() => {
    const previous = tyres.data?.[0];
    if (!isEdit && previous && !prefilledRef.current) {
      prefilledRef.current = true;
      reset({ ...emptyValues, make: previous.make });
    }
  }, [tyres.data, isEdit, reset]);

  if (!canWrite) return <GarageForbidden />;
  if (isEdit && tyre.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (isEdit && (tyre.isError || !tyre.data)) {
    return (
      <ErrorState
        title={t("garage.notFound")}
        body={t("garage.errorBody")}
        onRetry={() => void tyre.refetch()}
      />
    );
  }

  const onSubmit = async (values: TyreFormOutput) => {
    try {
      const saved = tyreSetId
        ? await repository.updateTyreSet(
            teamId,
            tyreSetId,
            tyre.data?.localRev ?? 0,
            values,
          )
        : await repository.createTyreSet(teamId, values);
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      await navigate({
        to: "/teams/$teamId/garage/tyres/$tyreSetId",
        params: { teamId, tyreSetId: saved.id },
      });
    } catch (error) {
      setError("root", { message: garageErrorMessage(t, error) });
    }
  };

  return (
    <>
      <GaragePageHeader
        title={isEdit ? t("garage.tyre.editTitle") : t("garage.tyre.newTitle")}
        subtitle={t("garage.subtitle")}
      />
      <form noValidate onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <FormRootError error={errors.root?.message} />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField id="tyre-label" label={t("garage.fields.label")} error={errors.label?.message}>
            <TextInput
              id="tyre-label"
              autoCapitalize="characters"
              invalid={Boolean(errors.label)}
              aria-describedby={fieldDescribedBy("tyre-label", errors.label?.message, undefined)}
              {...register("label")}
            />
          </FormField>
          <FormField id="tyre-status" label={t("garage.fields.status")} error={errors.status?.message}>
            <SelectInput
              id="tyre-status"
              invalid={Boolean(errors.status)}
              aria-describedby={fieldDescribedBy("tyre-status", errors.status?.message, undefined)}
              {...register("status")}
            >
              <option value="new">{t("garage.statuses.new")}</option>
              <option value="used">{t("garage.statuses.used")}</option>
              <option value="worn">{t("garage.statuses.worn")}</option>
              <option value="retired">{t("garage.statuses.retired")}</option>
            </SelectInput>
          </FormField>
          <FormField id="tyre-make" label={t("garage.fields.make")} error={errors.make?.message}>
            <TextInput
              id="tyre-make"
              invalid={Boolean(errors.make)}
              aria-describedby={fieldDescribedBy("tyre-make", errors.make?.message, undefined)}
              {...register("make")}
            />
          </FormField>
          <FormField id="tyre-model" label={t("garage.fields.model")} error={errors.model?.message}>
            <TextInput
              id="tyre-model"
              invalid={Boolean(errors.model)}
              aria-describedby={fieldDescribedBy("tyre-model", errors.model?.message, undefined)}
              {...register("model")}
            />
          </FormField>
        </div>
        <details className="rounded-2xl border border-line bg-surface p-4">
          <summary className="flex min-h-12 cursor-pointer items-center font-semibold text-fg">
            {t("garage.actions.moreDetails")}
          </summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <FormField id="tyre-compound" label={t("garage.fields.compound")} error={errors.compound?.message}>
              <TextInput
                id="tyre-compound"
                invalid={Boolean(errors.compound)}
                aria-describedby={fieldDescribedBy("tyre-compound", errors.compound?.message, undefined)}
                {...register("compound")}
              />
            </FormField>
            <FormField id="tyre-insert" label={t("garage.fields.insertType")} error={errors.insertType?.message}>
              <TextInput
                id="tyre-insert"
                invalid={Boolean(errors.insertType)}
                aria-describedby={fieldDescribedBy("tyre-insert", errors.insertType?.message, undefined)}
                {...register("insertType")}
              />
            </FormField>
            <FormField id="tyre-glued" label={t("garage.fields.gluedOn")} error={errors.gluedOn?.message}>
              <TextInput
                id="tyre-glued"
                type="date"
                invalid={Boolean(errors.gluedOn)}
                aria-describedby={fieldDescribedBy("tyre-glued", errors.gluedOn?.message, undefined)}
                {...register("gluedOn")}
              />
            </FormField>
            <FormField id="tyre-wear" label={t("garage.fields.wearNotes")} error={errors.wearNotes?.message} className="sm:col-span-2">
              <TextArea
                id="tyre-wear"
                invalid={Boolean(errors.wearNotes)}
                aria-describedby={fieldDescribedBy("tyre-wear", errors.wearNotes?.message, undefined)}
                {...register("wearNotes")}
              />
            </FormField>
          </div>
        </details>
        <FormActions>
          <Button variant="secondary" full asChild>
            <Link to="/teams/$teamId/garage/tyres" params={{ teamId }}>
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
