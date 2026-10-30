import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import type { GarageRepository } from "@/features/garage/data/garage-repository";
import type { TeamRole } from "@/features/teams/domain/team";
import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { TextInput } from "@/shared/ui/TextInput";

import { setupQueryOptions, versionQueryOptions } from "../data/setup-queries";
import type { SetupRepository } from "../data/setup-repository";
import { applyTexts, changesPerGroup, initialTexts, totalChanges, type FieldTexts } from "../domain/form";
import type { SetupData } from "../domain/models";
import { canWriteSetups } from "../domain/roles";
import { changeSummarySchema } from "../domain/schemas";
import { SetupsForbidden, SetupsLoading, SetupsNotFound, SetupsPageHeader } from "./common";
import { setupErrorMessage } from "./errors";
import { SetupFieldsForm } from "./SetupFieldsForm";
import { useGarageChoices } from "./use-choices";

export function SetupEditPage({
  teamId,
  setupId,
  role,
  repository,
  garageRepository,
}: {
  teamId: string;
  setupId: string;
  role: TeamRole;
  repository: SetupRepository;
  garageRepository: GarageRepository;
}) {
  const { t } = useTranslation();
  const setup = useQuery(setupQueryOptions(repository, teamId, setupId));
  const current = useQuery(versionQueryOptions(repository, teamId, setupId));
  const { choices } = useGarageChoices(garageRepository, teamId);

  if (!canWriteSetups(role)) return <SetupsForbidden />;
  if (setup.isPending || current.isPending) return <SetupsLoading />;
  if (setup.isError || current.isError) {
    return (
      <SetupsNotFound
        onRetry={() => {
          void setup.refetch();
          void current.refetch();
        }}
      />
    );
  }
  if (!setup.data || !current.data) return <SetupsNotFound />;

  // Keyed by the version being edited: a new version starts a fresh form.
  return (
    <SetupEditor
      key={`${setupId}:${current.data.version}`}
      teamId={teamId}
      setupId={setupId}
      name={setup.data.name}
      baseVersion={current.data.version}
      base={current.data.data}
      choices={choices}
      repository={repository}
      title={t("setups.edit.title")}
    />
  );
}

function SetupEditor({
  teamId,
  setupId,
  name,
  baseVersion,
  base,
  choices,
  repository,
  title,
}: {
  teamId: string;
  setupId: string;
  name: string;
  baseVersion: number;
  base: SetupData;
  choices: React.ComponentProps<typeof SetupFieldsForm>["choices"];
  repository: SetupRepository;
  title: string;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const [initial] = useState<FieldTexts>(() => initialTexts(base));
  const [texts, setTexts] = useState<FieldTexts>(initial);
  const [summary, setSummary] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();

  const result = applyTexts(base, texts);
  const counts = changesPerGroup(base, result.data);
  const total = totalChanges(counts);
  const hasIssues = Object.keys(result.issues).length > 0;
  const summaryParsed = changeSummarySchema.safeParse(summary);

  const onSave = async () => {
    setSubmitted(true);
    setError(undefined);
    if (hasIssues || !summaryParsed.success) {
      setError(t("setups.edit.fixErrors"));
      return;
    }
    if (total === 0 || !userId) return;
    setSaving(true);
    try {
      await repository.createVersion(teamId, setupId, {
        baseVersion,
        data: result.data,
        changeSummary: summaryParsed.data,
        createdBy: userId,
      });
      await queryClient.invalidateQueries({ queryKey: ["setups", teamId] });
      await navigate({ to: "/teams/$teamId/setups/$setupId", params: { teamId, setupId } });
    } catch (caught) {
      setError(setupErrorMessage(t, caught));
      setSaving(false);
    }
  };

  return (
    <>
      <SetupsPageHeader title={title} subtitle={`${name} · ${t("setups.edit.editing", { version: baseVersion, next: baseVersion + 1 })}`} />
      <form
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          void onSave();
        }}
        className="space-y-5"
      >
        <FormRootError error={error} />
        <SetupFieldsForm
          base={base}
          texts={texts}
          initial={initial}
          result={result}
          counts={counts}
          showAllErrors={submitted}
          choices={choices}
          onChange={(path, text) => setTexts((previous) => ({ ...previous, [path]: text }))}
        />
        <FormField
          id="setup-change-summary"
          label={t("setups.edit.summary")}
          hint={t("setups.edit.summaryHint")}
          error={summaryParsed.success ? undefined : "setups.validation.tooLong"}
        >
          <TextInput
            id="setup-change-summary"
            value={summary}
            autoComplete="off"
            onChange={(event) => setSummary(event.target.value)}
          />
        </FormField>
        {total === 0 ? (
          <p className="text-sm text-muted" aria-live="polite">
            {t("setups.edit.nothingChanged")}
          </p>
        ) : (
          <p className="text-sm font-semibold text-primary" aria-live="polite">
            {t("setups.edit.changed", { count: total })}
          </p>
        )}
        <FormActions>
          <Button variant="secondary" full asChild>
            <Link to="/teams/$teamId/setups/$setupId" params={{ teamId, setupId }}>
              {t("setups.actions.cancel")}
            </Link>
          </Button>
          <Button type="submit" full disabled={saving || total === 0}>
            {saving ? t("setups.actions.saving") : t("setups.actions.save")}
          </Button>
        </FormActions>
      </form>
    </>
  );
}
