import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useRef } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { sessionQueryOptions, sessionQueryKey } from "@/auth/session-query";
import { stepUpHref } from "@/auth/session";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { PageHeader } from "@/shared/ui/PageHeader";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Spinner } from "@/shared/ui/Spinner";
import { TextInput } from "@/shared/ui/TextInput";
import { fieldDescribedBy } from "@/shared/ui/form-errors";
import { newId } from "@/shared/lib/id";
import { useOnline } from "@/shared/lib/use-online";

import type { TeamsRepository } from "../data/teams-repository";
import { createTeamFormSchema, teamCategories, type CreateTeamFormOutput, type CreateTeamFormValues } from "../domain/team-schemas";
import { hasStrongFactor } from "../domain/roles";
import { teamsQueryKey } from "../data/teams-query";
import { classifyRpcError } from "../data/errors";

const initialValues: CreateTeamFormValues = { name: "", category: "", club: "" };

export function TeamCreatePage({
  repository,
  weakStepUp = false,
}: {
  repository: TeamsRepository;
  weakStepUp?: boolean;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const online = useOnline();
  const session = useQuery(sessionQueryOptions);
  const lastRequest = useRef<{ body: string; id: string } | null>(null);

  const form = useForm<CreateTeamFormValues, unknown, CreateTeamFormOutput>({
    resolver: zodResolver(createTeamFormSchema),
    defaultValues: initialValues,
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });

  if (session.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("teams.loading")}</span>
      </div>
    );
  }

  const amr = session.data?.authenticated ? session.data.amr : [];
  const hasStrong = hasStrongFactor(amr);

  const submit = async (values: CreateTeamFormOutput) => {
    const body = JSON.stringify(values);
    if (!lastRequest.current || lastRequest.current.body !== body) {
      lastRequest.current = { body, id: newId() };
    }
    try {
      const team = await repository.createTeam({
        requestId: lastRequest.current.id,
        name: values.name,
        category: values.category,
        club: values.club,
        countryCode: "PT",
      });
      lastRequest.current = null;
      await queryClient.invalidateQueries({ queryKey: teamsQueryKey });
      await navigate({ to: "/teams/$teamId", params: { teamId: team.id } });
    } catch (error) {
      if (classifyRpcError(error) === "unauthenticated") {
        await queryClient.invalidateQueries({ queryKey: sessionQueryKey });
        const latest = await queryClient.fetchQuery(sessionQueryOptions).catch(() => undefined);
        if (latest?.authenticated) {
          await navigate({ to: "/teams/new", search: { step_up: "weak" } });
        } else {
          await navigate({ to: "/login", search: { returnTo: "/teams/new" } });
        }
        return;
      }
      form.setError("root", { message: teamErrorMessage(t, error) });
    }
  };

  if (!hasStrong || weakStepUp) {
    return (
      <main className="mx-auto max-w-xl space-y-5 px-4 py-6">
        <PageHeader title={t("teams.stepUp.title")} subtitle={t("teams.stepUp.body")} />
        {weakStepUp ? (
          <Alert tone="warn" title={t("teams.stepUp.weakTitle")}>
            {t("teams.stepUp.weakBody")}
          </Alert>
        ) : null}
        {!online ? <Alert tone="warn">{t("teams.offline")}</Alert> : null}
        <div className="space-y-3">
          <Button asChild size="lg" full disabled={!online}>
            <a href="/auth/account-security" target="_blank" rel="noopener noreferrer">
              {t("teams.stepUp.configure")}
            </a>
          </Button>
          <Button asChild variant="secondary" size="lg" full disabled={!online}>
            <a href={stepUpHref("/teams/new")}>{t("teams.stepUp.reauthenticate")}</a>
          </Button>
          <Button asChild variant="ghost" size="lg" full>
            <a href="/teams">{t("garage.actions.cancel")}</a>
          </Button>
        </div>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-xl space-y-5 px-4 py-6">
      <PageHeader title={t("teams.create.title")} subtitle={t("teams.create.subtitle")} />
      {!online ? <Alert tone="warn">{t("teams.offline")}</Alert> : null}
      <form noValidate onSubmit={(event) => void form.handleSubmit(submit)(event)} className="space-y-5">
        <FormRootError error={form.formState.errors.root?.message} />
        <FormField id="team-name" label={t("teams.create.name")} error={form.formState.errors.name?.message}>
          <TextInput
            id="team-name"
            autoComplete="organization"
            placeholder={t("teams.create.namePlaceholder")}
            disabled={!online || form.formState.isSubmitting}
            invalid={Boolean(form.formState.errors.name)}
            aria-describedby={fieldDescribedBy("team-name", form.formState.errors.name?.message, undefined)}
            {...form.register("name")}
          />
        </FormField>
        <FormField id="team-category" label={t("teams.create.category")} error={form.formState.errors.category?.message}>
          <SelectInput
            id="team-category"
            disabled={!online || form.formState.isSubmitting}
            invalid={Boolean(form.formState.errors.category)}
            aria-describedby={fieldDescribedBy("team-category", form.formState.errors.category?.message, undefined)}
            {...form.register("category")}
          >
            <option value="">{t("teams.create.defaultCategory")}</option>
            {teamCategories.map((category) => (
              <option key={category} value={category}>
                {category}
              </option>
            ))}
          </SelectInput>
        </FormField>
        <FormField id="team-club" label={t("teams.create.club")} error={form.formState.errors.club?.message}>
          <TextInput
            id="team-club"
            autoComplete="organization"
            disabled={!online || form.formState.isSubmitting}
            invalid={Boolean(form.formState.errors.club)}
            aria-describedby={fieldDescribedBy("team-club", form.formState.errors.club?.message, undefined)}
            {...form.register("club")}
          />
        </FormField>
        <FormActions>
          <Button asChild variant="secondary" size="lg">
            <a href="/teams">{t("garage.actions.cancel")}</a>
          </Button>
          <Button type="submit" size="lg" disabled={!online || form.formState.isSubmitting}>
            {form.formState.isSubmitting ? t("garage.actions.saving") : t("teams.create.submit")}
          </Button>
        </FormActions>
      </form>
    </main>
  );
}

function teamErrorMessage(t: TFunction, error: unknown): string {
  switch (classifyRpcError(error)) {
    case "limit":
      return t("teams.errors.limit");
    case "unavailable":
      return t("teams.errors.offline");
    case "invalid":
      return t("teams.errors.invalid");
    default:
      return t("teams.errors.generic");
  }
}
