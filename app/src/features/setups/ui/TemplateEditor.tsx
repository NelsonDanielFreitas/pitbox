import { useCallback, useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { TextInput } from "@/shared/ui/TextInput";

import type { SetupRepository } from "../data/setup-repository";
import { SCHEMA_VERSION_V2, type Template } from "../domain/template";
import { applyTemplateTexts, changedKeys, initialTemplateTexts, type TemplateTexts } from "../domain/template-form";
import { changeSummarySchema } from "../domain/schemas";
import { SetupsPageHeader } from "./common";
import { setupErrorMessage } from "./errors";
import { TemplateForm } from "./template/TemplateForm";
import { localized } from "./template/text";

/** Edits a setup made on a sheet template; saving creates the next immutable version. */
export function TemplateEditor({
  teamId,
  setupId,
  name,
  baseVersion,
  baseData,
  template,
  repository,
}: {
  teamId: string;
  setupId: string;
  name: string;
  baseVersion: number;
  baseData: Record<string, unknown>;
  template: Template;
  repository: SetupRepository;
}) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const baseValues = isRecord(baseData.values) ? baseData.values : {};
  const [texts, setTexts] = useState<TemplateTexts>(() => initialTemplateTexts(baseValues));
  const [summary, setSummary] = useState("");
  // Stable, so the memoised fields of a long sheet do not all redraw on each keystroke.
  const onFieldChange = useCallback((key: string, text: string) => setTexts((previous) => ({ ...previous, [key]: text })), []);
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();

  const result = applyTemplateTexts(template, texts);
  const total = changedKeys(baseValues, result.values).length;
  const hasIssues = Object.keys(result.issues).length > 0;
  const summaryParsed = changeSummarySchema.safeParse(summary);
  const visibleIssues = submitted ? result.issues : Object.fromEntries(Object.entries(result.issues).filter(([k]) => (texts[k] ?? "") !== initialTemplateTexts(baseValues)[k]));

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
        // References to the engine and tyre set stay as they were.
        data: {
          template: { id: template.id, version: template.version },
          values: result.values,
          ...(isRecord(baseData.refs) ? { refs: baseData.refs } : {}),
        },
        schemaVersion: SCHEMA_VERSION_V2,
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
      <SetupsPageHeader
        title={t("setups.edit.title")}
        subtitle={`${name} · ${localized(template.name, i18n.language)} · ${t("setups.edit.editing", { version: baseVersion, next: baseVersion + 1 })}`}
      />
      <form
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          void onSave();
        }}
        className="space-y-5"
      >
        <FormRootError error={error} />
        <TemplateForm
          template={template}
          texts={texts}
          base={baseValues}
          issues={visibleIssues}
          onChange={onFieldChange}
        />
        <FormField
          id="setup-change-summary"
          label={t("setups.edit.summary")}
          hint={t("setups.edit.summaryHint")}
          error={summaryParsed.success ? undefined : "setups.validation.tooLong"}
        >
          <TextInput id="setup-change-summary" value={summary} autoComplete="off" onChange={(event) => setSummary(event.target.value)} />
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

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
