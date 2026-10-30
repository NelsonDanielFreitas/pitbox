import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Sheet } from "@/shared/ui/Sheet";
import { TextArea } from "@/shared/ui/TextArea";
import { TextInput } from "@/shared/ui/TextInput";

import type { GarageRepository } from "../data/garage-repository";
import {
  removalDestinations,
  scrapReasons,
  type PartInstall,
  type RemovalDestination,
  type ScrapReason,
} from "../domain/models";
import { todayIso } from "../domain/today";
import { garageErrorMessage } from "./errors";

export function UnmountSheet({
  install,
  onClose,
  teamId,
  repository,
  userId,
}: {
  /** The open install; `null` keeps the sheet closed. */
  install: PartInstall | null;
  onClose: () => void;
  teamId: string;
  repository: GarageRepository;
  userId: string | null;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const today = useMemo(() => todayIso(), []);
  const [destination, setDestination] = useState<RemovalDestination>("stock");
  const [reason, setReason] = useState<ScrapReason | "">("");
  const [date, setDate] = useState(today);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const close = () => {
    setDestination("stock");
    setReason("");
    setNote("");
    setError(null);
    onClose();
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!install) return;
    if (destination === "scrap" && reason === "") return setError(t("garage.partUnmount.reasonRequired"));
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || date > today) {
      return setError(t("garage.validation.dateFuture"));
    }
    const trimmed = note.trim();
    if ([...trimmed].length > 500) return setError(t("garage.validation.tooLong"));
    setBusy(true);
    setError(null);
    try {
      await repository.removePart(teamId, install.id, {
        destination,
        reason: destination === "scrap" && reason !== "" ? reason : null,
        occurredOn: date,
        note: trimmed === "" ? null : trimmed,
        createdBy: userId,
      });
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      close();
    } catch (failure) {
      setError(garageErrorMessage(t, failure));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet
      open={install !== null}
      title={t("garage.partUnmount.title")}
      description={install ? `${install.partDescription} · ${install.targetName}` : undefined}
      onClose={close}
    >
      <form noValidate onSubmit={(event) => void submit(event)} className="space-y-4">
        <FormRootError error={error ?? undefined} />
        <fieldset className="space-y-2">
          <legend className="text-sm font-semibold text-fg">{t("garage.partUnmount.destination")}</legend>
          {removalDestinations.map((value) => (
            <label
              key={value}
              className="flex min-h-12 items-center gap-3 rounded-xl border border-line bg-surface px-4 text-sm text-fg"
            >
              <input
                type="radio"
                name="removal-destination"
                value={value}
                checked={destination === value}
                onChange={() => setDestination(value)}
                className="size-5 accent-[var(--primary)]"
              />
              <span>
                <span className="block font-semibold">{t(`garage.partUnmount.destinations.${value}`)}</span>
                <span className="block text-xs text-muted">
                  {t(`garage.partUnmount.destinationHints.${value}`)}
                </span>
              </span>
            </label>
          ))}
        </fieldset>
        {destination === "scrap" ? (
          <FormField id="unmount-reason" label={t("garage.partUnmount.reason")}>
            <SelectInput
              id="unmount-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value as ScrapReason | "")}
            >
              <option value="">—</option>
              {scrapReasons.map((value) => (
                <option key={value} value={value}>
                  {t(`garage.movement.loss.${value}`)}
                </option>
              ))}
            </SelectInput>
          </FormField>
        ) : null}
        <FormField id="unmount-date" label={t("garage.movement.date")}>
          <TextInput
            id="unmount-date"
            type="date"
            max={today}
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
        </FormField>
        <FormField id="unmount-note" label={t("garage.fields.notes")}>
          <TextArea
            id="unmount-note"
            className="min-h-20"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </FormField>
        <FormActions>
          <Button variant="secondary" full type="button" onClick={close}>
            {t("garage.actions.cancel")}
          </Button>
          <Button type="submit" full disabled={busy}>
            {busy ? t("garage.actions.saving") : t("garage.partUnmount.confirm")}
          </Button>
        </FormActions>
      </form>
    </Sheet>
  );
}
