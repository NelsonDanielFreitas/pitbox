import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { templatesQueryOptions } from "@/features/setups/data/template-queries";
import { defaultTemplateRepository, type TemplateRepository } from "@/features/setups/data/template-repository";
import type { TemplateInfo } from "@/features/setups/domain/models";
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

import { carQueryOptions, carsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { canWriteGarage } from "../domain/roles";
import {
  carFormSchema,
  type CarFormOutput,
  type CarFormValues,
} from "../domain/schemas";
import { GarageForbidden, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";

const emptyValues: CarFormValues = {
  make: "",
  model: "",
  nickname: "",
  transponderNumber: "",
  modelYear: "",
  serialNumber: "",
  notes: "",
  status: "active",
};

export function CarFormPage({
  teamId,
  role,
  repository,
  carId,
  templateRepository = defaultTemplateRepository,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
  carId?: string;
  templateRepository?: TemplateRepository;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const canWrite = canWriteGarage(role);
  const isEdit = carId !== undefined;
  const car = useQuery({
    ...carQueryOptions(repository, teamId, carId ?? ""),
    enabled: isEdit,
  });
  const cars = useQuery({
    ...carsQueryOptions(repository, teamId),
    enabled: !isEdit,
  });

  const templates = useQuery(templatesQueryOptions(templateRepository, teamId));
  // undefined = not touched: a new car starts on the generic sheet, an edited one keeps its sheet.
  const [sheetChoice, setSheetChoice] = useState<string | undefined>(undefined);
  const [duplicating, setDuplicating] = useState(false);
  const {
    register,
    handleSubmit,
    reset,
    setError,
    control,
    formState: { errors, isSubmitting },
  } = useForm<CarFormValues, unknown, CarFormOutput>({
    resolver: zodResolver(carFormSchema),
    defaultValues: emptyValues,
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });
  const make = useWatch({ control, name: "make" });
  const model = useWatch({ control, name: "model" });
  const loadedRef = useRef<string | undefined>(undefined);
  const prefilledRef = useRef(false);

  useEffect(() => {
    if (car.data && loadedRef.current !== car.data.id) {
      loadedRef.current = car.data.id;
      reset({
        make: car.data.make,
        model: car.data.model,
        nickname: car.data.nickname ?? "",
        transponderNumber: car.data.transponderNumber ?? "",
        modelYear: car.data.modelYear === null ? "" : String(car.data.modelYear),
        serialNumber: car.data.serialNumber ?? "",
        notes: car.data.notes ?? "",
        status: car.data.status,
      });
    }
  }, [car.data, reset]);

  useEffect(() => {
    const previous = cars.data?.[0];
    if (!isEdit && previous && !prefilledRef.current) {
      prefilledRef.current = true;
      reset({ ...emptyValues, make: previous.make });
    }
  }, [cars.data, isEdit, reset]);

  if (!canWrite) return <GarageForbidden />;

  if (isEdit && car.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (isEdit && (car.isError || !car.data)) {
    return (
      <ErrorState
        title={t("garage.notFound")}
        body={t("garage.errorBody")}
        onRetry={() => void car.refetch()}
      />
    );
  }

  const onSubmit = async (values: CarFormOutput) => {
    try {
      const saved = carId
        ? await repository.updateCar(teamId, carId, car.data?.localRev ?? 0, {
            ...values,
            ...(sheetChoice !== undefined ? { sheetTemplateId: sheetChoice || null } : {}),
          })
        : await repository.createCar(teamId, { ...values, sheetTemplateId: selected || null });
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      await navigate({
        to: "/teams/$teamId/garage/cars/$carId",
        params: { teamId, carId: saved.id },
      });
    } catch (error) {
      setError("root", { message: garageErrorMessage(t, error) });
    }
  };

  const all = templates.data ?? [];
  const suggested = suggestTemplates(all, make, model);
  // A new car whose make and model match exactly one sheet of the catalogue starts on it (the
  // person can still change it); an edited car keeps the sheet it has.
  const auto = !isEdit && sheetChoice === undefined && suggested.length === 1 ? suggested[0] : undefined;
  const selected = sheetChoice ?? auto?.id ?? car.data?.sheetTemplateId ?? "";
  const noSheetForModel = !isEdit && templates.isSuccess && selected === "" && make.trim() !== "" && model.trim() !== "" && suggested.length === 0;
  const selectedInfo = all.find((tpl) => tpl.id === selected);
  const others = all.filter((tpl) => !suggested.includes(tpl));

  const duplicate = async () => {
    if (!selectedInfo || selectedInfo.teamId !== null) return;
    setDuplicating(true);
    try {
      const copy = await templateRepository.duplicateForTeam(teamId, selectedInfo.id);
      await queryClient.invalidateQueries({ queryKey: ["setups", "templates"] });
      setSheetChoice(copy.id);
    } catch (error) {
      setError("root", { message: garageErrorMessage(t, error) });
    } finally {
      setDuplicating(false);
    }
  };

  const title = isEdit ? t("garage.car.editTitle") : t("garage.car.newTitle");

  return (
    <>
      <GaragePageHeader title={title} subtitle={t("garage.subtitle")} />
      <form noValidate onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <FormRootError error={errors.root?.message} />
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField id="car-make" label={t("garage.fields.make")} error={errors.make?.message}>
            <TextInput
              id="car-make"
              autoComplete="off"
              invalid={Boolean(errors.make)}
              aria-describedby={fieldDescribedBy("car-make", errors.make?.message, undefined)}
              {...register("make")}
            />
          </FormField>
          <FormField id="car-model" label={t("garage.fields.model")} error={errors.model?.message}>
            <TextInput
              id="car-model"
              autoComplete="off"
              invalid={Boolean(errors.model)}
              aria-describedby={fieldDescribedBy("car-model", errors.model?.message, undefined)}
              {...register("model")}
            />
          </FormField>
          <FormField id="car-nickname" label={t("garage.fields.nickname")} error={errors.nickname?.message}>
            <TextInput
              id="car-nickname"
              invalid={Boolean(errors.nickname)}
              aria-describedby={fieldDescribedBy("car-nickname", errors.nickname?.message, undefined)}
              {...register("nickname")}
            />
          </FormField>
          <FormField
            id="car-transponder"
            label={t("garage.fields.transponderNumber")}
            hint={t("garage.fields.transponderHint")}
            error={errors.transponderNumber?.message}
          >
            <TextInput
              id="car-transponder"
              inputMode="numeric"
              invalid={Boolean(errors.transponderNumber)}
              aria-describedby={fieldDescribedBy(
                "car-transponder",
                errors.transponderNumber?.message,
                t("garage.fields.transponderHint"),
              )}
              {...register("transponderNumber")}
            />
          </FormField>
          <FormField id="car-status" label={t("garage.fields.status")} error={errors.status?.message}>
            <SelectInput
              id="car-status"
              invalid={Boolean(errors.status)}
              aria-describedby={fieldDescribedBy("car-status", errors.status?.message, undefined)}
              {...register("status")}
            >
              <option value="active">{t("garage.statuses.active")}</option>
              <option value="spare">{t("garage.statuses.spare")}</option>
              <option value="retired">{t("garage.statuses.retired")}</option>
            </SelectInput>
          </FormField>
        </div>
        <FormField id="car-sheet" label={t("setups.template.sheetFor")}>
          <SelectInput id="car-sheet" value={selected} onChange={(event) => setSheetChoice(event.target.value)}>
            <option value="">{t("setups.template.sheetGeneric")}</option>
            {suggested.length > 0 ? (
              <optgroup label={t("setups.template.suggested")}>
                {suggested.map((tpl) => (
                  <option key={tpl.id} value={tpl.id}>
                    {tpl.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
            {others.map((tpl) => (
              <option key={tpl.id} value={tpl.id}>
                {tpl.name}
              </option>
            ))}
          </SelectInput>
        </FormField>
        {auto ? (
          <p role="status" className="text-sm text-muted">
            {t("setups.template.autoApplied", { name: auto.name })}
          </p>
        ) : null}
        {noSheetForModel ? (
          <p role="status" className="text-sm text-muted">
            {t("setups.template.noneForModel", { make: make.trim(), model: model.trim() })}
          </p>
        ) : null}
        {selectedInfo && selectedInfo.teamId === null ? (
          <Button type="button" variant="secondary" disabled={duplicating} onClick={() => void duplicate()}>
            {t("setups.template.duplicate")}
          </Button>
        ) : null}
        <div className="grid grid-cols-2 gap-4 rounded-2xl border border-line bg-surface p-4">
          <p className="text-sm text-muted">{t("garage.fields.scale")}</p>
          <p className="text-right text-sm font-semibold text-fg">1/8</p>
          <p className="text-sm text-muted">{t("garage.fields.powertrain")}</p>
          <p className="text-right text-sm font-semibold text-fg">Nitro</p>
        </div>
        <details className="rounded-2xl border border-line bg-surface p-4">
          <summary className="flex min-h-12 cursor-pointer items-center font-semibold text-fg">
            {t("garage.actions.moreDetails")}
          </summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <FormField id="car-year" label={t("garage.fields.modelYear")} error={errors.modelYear?.message}>
              <TextInput
                id="car-year"
                inputMode="numeric"
                invalid={Boolean(errors.modelYear)}
                aria-describedby={fieldDescribedBy("car-year", errors.modelYear?.message, undefined)}
                {...register("modelYear")}
              />
            </FormField>
            <FormField id="car-serial" label={t("garage.fields.serialNumber")} error={errors.serialNumber?.message}>
              <TextInput
                id="car-serial"
                invalid={Boolean(errors.serialNumber)}
                aria-describedby={fieldDescribedBy("car-serial", errors.serialNumber?.message, undefined)}
                {...register("serialNumber")}
              />
            </FormField>
            <FormField id="car-notes" label={t("garage.fields.notes")} error={errors.notes?.message} className="sm:col-span-2">
              <TextArea
                id="car-notes"
                invalid={Boolean(errors.notes)}
                aria-describedby={fieldDescribedBy("car-notes", errors.notes?.message, undefined)}
                {...register("notes")}
              />
            </FormField>
          </div>
        </details>
        <FormActions>
          <Button variant="secondary" full asChild>
            <Link to="/teams/$teamId/garage/cars" params={{ teamId }}>
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

const norm = (text: string): string => text.trim().toLowerCase();

/** Catalogue templates for the make and model typed so far: same make, and a model that matches either way. */
function suggestTemplates(all: readonly TemplateInfo[], make: string, model: string): TemplateInfo[] {
  const m = norm(make);
  const mod = norm(model);
  if (m === "" || mod === "") return [];
  return all.filter((tpl) => {
    const tm = norm(tpl.model);
    return norm(tpl.make) === m && (tm.includes(mod) || mod.includes(tm));
  });
}
