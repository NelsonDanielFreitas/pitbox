import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { carsQueryOptions } from "@/features/garage/data/garage-queries";
import type { GarageRepository } from "@/features/garage/data/garage-repository";
import type { TeamRole } from "@/features/teams/domain/team";
import { Button } from "@/shared/ui/Button";
import { ErrorState } from "@/shared/ui/ErrorState";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { fieldDescribedBy } from "@/shared/ui/form-errors";
import { SelectInput } from "@/shared/ui/SelectInput";
import { TextArea } from "@/shared/ui/TextArea";
import { TextInput } from "@/shared/ui/TextInput";

import { setupsQueryOptions, versionsQueryOptions } from "../data/setup-queries";
import type { SetupRepository, SetupSource } from "../data/setup-repository";
import { canWriteSetups } from "../domain/roles";
import { setupFormSchema, type SetupFormValues } from "../domain/schemas";
import { SetupsForbidden, SetupsLoading, SetupsPageHeader } from "./common";
import { setupErrorMessage } from "./errors";

type SourceMode = "blank" | "last" | "other";

export function NewSetupPage({
  teamId,
  role,
  repository,
  garageRepository,
}: {
  teamId: string;
  role: TeamRole;
  repository: SetupRepository;
  garageRepository: GarageRepository;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const cars = useQuery(carsQueryOptions(garageRepository, teamId));
  const setups = useQuery(setupsQueryOptions(repository, teamId));
  const [carId, setCarId] = useState("");
  const [mode, setMode] = useState<SourceMode>("blank");
  const [otherSetupId, setOtherSetupId] = useState("");
  const [otherVersion, setOtherVersion] = useState("");
  const versions = useQuery({
    ...versionsQueryOptions(repository, teamId, otherSetupId),
    enabled: otherSetupId !== "",
  });
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<SetupFormValues, unknown, { name: string; notes: string | null }>({
    resolver: zodResolver(setupFormSchema),
    defaultValues: { name: "", notes: "" },
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });

  if (!canWriteSetups(role)) return <SetupsForbidden />;
  if (cars.isPending || setups.isPending) return <SetupsLoading />;
  if (cars.isError || setups.isError) {
    return (
      <ErrorState
        title={t("setups.errorTitle")}
        body={t("setups.errorBody")}
        onRetry={() => {
          void cars.refetch();
          void setups.refetch();
        }}
      />
    );
  }

  const activeCars = cars.data.filter((car) => car.status !== "retired");
  const effectiveCarId = carId || activeCars[0]?.id || "";
  const last = setups.data[0];

  const resolveSource = (): SetupSource | null => {
    if (mode === "last" && last) return { setupId: last.id, version: last.currentVersion };
    if (mode === "other" && otherSetupId) {
      const version = Number(otherVersion || versions.data?.[0]?.version);
      return Number.isInteger(version) && version > 0 ? { setupId: otherSetupId, version } : null;
    }
    return null;
  };

  const onSubmit = async (values: { name: string; notes: string | null }) => {
    if (!userId || !effectiveCarId) {
      setError("root", { message: t("setups.new.pickCar") });
      return;
    }
    const source = resolveSource();
    if (mode !== "blank" && !source) {
      setError("root", { message: t("setups.new.sourceNone") });
      return;
    }
    try {
      const created = await repository.createSetup(teamId, {
        carId: effectiveCarId,
        name: values.name,
        notes: values.notes,
        source,
        createdBy: userId,
      });
      await queryClient.invalidateQueries({ queryKey: ["setups", teamId] });
      await navigate({
        to: "/teams/$teamId/setups/$setupId/edit",
        params: { teamId, setupId: created.setup.id },
      });
    } catch (error) {
      setError("root", { message: setupErrorMessage(t, error) });
    }
  };

  return (
    <>
      <SetupsPageHeader title={t("setups.new.title")} subtitle={t("setups.subtitle")} />
      {activeCars.length === 0 ? (
        <ErrorState title={t("setups.list.noCars")} body={t("setups.list.noCarsBody")} />
      ) : (
        <form noValidate onSubmit={handleSubmit(onSubmit)} className="space-y-5">
          <FormRootError error={errors.root?.message} />
          <FormField id="setup-car" label={t("setups.new.car")}>
            <SelectInput id="setup-car" value={effectiveCarId} onChange={(event) => setCarId(event.target.value)}>
              {activeCars.map((car) => (
                <option key={car.id} value={car.id}>
                  {car.nickname ?? `${car.make} ${car.model}`}
                </option>
              ))}
            </SelectInput>
          </FormField>
          <FormField id="setup-name" label={t("setups.new.name")} error={errors.name?.message}>
            <TextInput
              id="setup-name"
              autoComplete="off"
              placeholder={t("setups.new.namePlaceholder")}
              invalid={Boolean(errors.name)}
              aria-describedby={fieldDescribedBy("setup-name", errors.name?.message, undefined)}
              {...register("name")}
            />
          </FormField>
          <FormField id="setup-notes" label={t("setups.new.notes")} error={errors.notes?.message}>
            <TextArea
              id="setup-notes"
              invalid={Boolean(errors.notes)}
              aria-describedby={fieldDescribedBy("setup-notes", errors.notes?.message, undefined)}
              {...register("notes")}
            />
          </FormField>
          <fieldset className="space-y-2">
            <legend className="mb-1 text-sm font-semibold text-fg">{t("setups.new.source")}</legend>
            <SourceOption id="source-blank" checked={mode === "blank"} onSelect={() => setMode("blank")}>
              {t("setups.new.sourceBlank")}
            </SourceOption>
            {last ? (
              <SourceOption id="source-last" checked={mode === "last"} onSelect={() => setMode("last")}>
                {t("setups.new.sourceLast")}: {last.name}
              </SourceOption>
            ) : null}
            {setups.data.length > 0 ? (
              <SourceOption id="source-other" checked={mode === "other"} onSelect={() => setMode("other")}>
                {t("setups.new.sourceOther")}
              </SourceOption>
            ) : (
              <p className="text-sm text-muted">{t("setups.new.sourceNone")}</p>
            )}
          </fieldset>
          {mode === "other" ? (
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="source-setup" label={t("setups.new.sourceSetup")}>
                <SelectInput
                  id="source-setup"
                  value={otherSetupId}
                  onChange={(event) => {
                    setOtherSetupId(event.target.value);
                    setOtherVersion("");
                  }}
                >
                  <option value="">—</option>
                  {setups.data.map((setup) => (
                    <option key={setup.id} value={setup.id}>
                      {setup.name}
                    </option>
                  ))}
                </SelectInput>
              </FormField>
              <FormField id="source-version" label={t("setups.new.sourceVersion")}>
                <SelectInput
                  id="source-version"
                  value={otherVersion || String(versions.data?.[0]?.version ?? "")}
                  disabled={otherSetupId === ""}
                  onChange={(event) => setOtherVersion(event.target.value)}
                >
                  {(versions.data ?? []).map((version) => (
                    <option key={version.version} value={version.version}>
                      {t("setups.list.version", { version: version.version })}
                    </option>
                  ))}
                </SelectInput>
              </FormField>
            </div>
          ) : null}
          <FormActions>
            <Button variant="secondary" full asChild>
              <Link to="/teams/$teamId/setups" params={{ teamId }}>
                {t("setups.actions.cancel")}
              </Link>
            </Button>
            <Button type="submit" full disabled={isSubmitting}>
              {isSubmitting ? t("setups.actions.creating") : t("setups.actions.create")}
            </Button>
          </FormActions>
        </form>
      )}
    </>
  );
}

function SourceOption({
  id,
  checked,
  onSelect,
  children,
}: {
  id: string;
  checked: boolean;
  onSelect: () => void;
  children: React.ReactNode;
}) {
  return (
    <label htmlFor={id} className="flex min-h-12 items-center gap-3 rounded-xl border border-line bg-surface px-3 text-sm font-medium text-fg">
      <input id={id} type="radio" name="setup-source" checked={checked} onChange={onSelect} className="size-5 accent-primary" />
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </label>
  );
}
