import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { RatingInput } from "@/shared/ui/RatingInput";
import { Sheet } from "@/shared/ui/Sheet";
import { TextArea } from "@/shared/ui/TextArea";
import { TextInput } from "@/shared/ui/TextInput";

import { feedbackScales, type FeedbackRatings, type SetupFeedbackInput } from "../domain/models";
import { feedbackInputSchema, parseFeedbackInput } from "../domain/schemas";

/** Quick driver feedback on one version. Mounted only while open, so it always starts empty. */
export function FeedbackSheet({
  version,
  onSave,
  onClose,
}: {
  version: number;
  onSave: (input: SetupFeedbackInput) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [ratings, setRatings] = useState<FeedbackRatings>({});
  const [trackZone, setTrackZone] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const submit = async () => {
    const draft = { ratings, trackZone, note };
    const check = feedbackInputSchema.safeParse(draft);
    if (!check.success) {
      setError(check.error.issues[0]?.message);
      return;
    }
    setSaving(true);
    setError(undefined);
    try {
      await onSave(parseFeedbackInput(draft));
    } catch {
      setError(t("setups.conflicts.generic"));
      setSaving(false);
    }
  };

  return (
    <Sheet open title={t("setups.feedback.sheetTitle", { version })} description={t("setups.feedback.scaleHint")} onClose={onClose}>
      <form
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
        className="space-y-4"
      >
        <FormRootError error={error} />
        {feedbackScales.map((scale) => (
          <RatingInput
            key={scale}
            legend={t(`setups.feedback.scales.${scale}`)}
            value={ratings[scale]}
            clearLabel={t("setups.feedback.clear")}
            onChange={(value) =>
              setRatings((previous) => {
                const next = { ...previous };
                if (value === undefined) delete next[scale];
                else next[scale] = value;
                return next;
              })
            }
          />
        ))}
        <FormField id="feedback-zone" label={t("setups.feedback.zone")} hint={t("setups.feedback.zoneHint")}>
          <TextInput id="feedback-zone" value={trackZone} autoComplete="off" onChange={(event) => setTrackZone(event.target.value)} />
        </FormField>
        <FormField id="feedback-note" label={t("setups.feedback.note")}>
          <TextArea id="feedback-note" value={note} onChange={(event) => setNote(event.target.value)} />
        </FormField>
        <div className="flex gap-3">
          <Button variant="secondary" full onClick={onClose}>
            {t("setups.actions.cancel")}
          </Button>
          <Button type="submit" full disabled={saving}>
            {saving ? t("setups.feedback.saving") : t("setups.feedback.save")}
          </Button>
        </div>
      </form>
    </Sheet>
  );
}
