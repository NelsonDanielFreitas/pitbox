import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Sheet } from "@/shared/ui/Sheet";
import { TextArea } from "@/shared/ui/TextArea";
import { TextInput } from "@/shared/ui/TextInput";

import {
  carsQueryOptions,
  enginesQueryOptions,
  partsQueryOptions,
} from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import type { InstallPartInput } from "../domain/models";
import { todayIso } from "../domain/today";
import { garageErrorMessage } from "./errors";

/** What is already known when the sheet opens; the other side is chosen in the sheet. */
export type MountSubject =
  | { kind: "part"; partId: string }
  | { kind: "car"; carId: string }
  | { kind: "engine"; engineId: string };

export function MountSheet({
  open,
  onClose,
  teamId,
  repository,
  userId,
  subject,
}: {
  open: boolean;
  onClose: () => void;
  teamId: string;
  repository: GarageRepository;
  userId: string | null;
  subject: MountSubject;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const today = useMemo(() => todayIso(), []);
  const choosingPart = subject.kind !== "part";
  const parts = useQuery({ ...partsQueryOptions(repository, teamId), enabled: open && choosingPart });
  const cars = useQuery({ ...carsQueryOptions(repository, teamId), enabled: open && !choosingPart });
  const engines = useQuery({
    ...enginesQueryOptions(repository, teamId),
    enabled: open && !choosingPart,
  });

  // Options are `car:<id>` / `engine:<id>` / a part id, so one select covers every case.
  const options = useMemo(() => {
    if (choosingPart) {
      return (parts.data ?? [])
        .filter((part) => part.hasServiceLife)
        .map((part) => ({
          value: part.id,
          label: [part.description, part.reference].filter(Boolean).join(" · "),
        }));
    }
    return [
      ...(cars.data ?? [])
        .filter((car) => car.status !== "retired")
        .map((car) => ({
          value: `car:${car.id}`,
          label: car.nickname ?? `${car.make} ${car.model}`,
        })),
      ...(engines.data ?? [])
        .filter((engine) => engine.status !== "retired")
        .map((engine) => ({
          value: `engine:${engine.id}`,
          label: `${engine.make} ${engine.model}`,
        })),
    ];
  }, [choosingPart, parts.data, cars.data, engines.data]);

  const [choice, setChoice] = useState("");
  const [date, setDate] = useState(today);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const close = () => {
    setChoice("");
    setNote("");
    setError(null);
    onClose();
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (choice === "") return setError(t("garage.partMount.chooseOne"));
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || date > today) {
      return setError(t("garage.validation.dateFuture"));
    }
    const trimmed = note.trim();
    if ([...trimmed].length > 500) return setError(t("garage.validation.tooLong"));
    let input: InstallPartInput;
    if (subject.kind === "part") {
      const [kind, id = ""] = choice.split(":");
      input = {
        partId: subject.partId,
        target: kind === "car" ? { carId: id } : { engineId: id },
        occurredOn: date,
        note: trimmed === "" ? null : trimmed,
        createdBy: userId,
      };
    } else {
      input = {
        partId: choice,
        target: subject.kind === "car" ? { carId: subject.carId } : { engineId: subject.engineId },
        occurredOn: date,
        note: trimmed === "" ? null : trimmed,
        createdBy: userId,
      };
    }
    setBusy(true);
    setError(null);
    try {
      await repository.installPart(teamId, input);
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
      open={open}
      title={t("garage.partMount.title")}
      description={t("garage.partMount.description")}
      onClose={close}
    >
      <form noValidate onSubmit={(event) => void submit(event)} className="space-y-4">
        <FormRootError error={error ?? undefined} />
        <FormField
          id="mount-choice"
          label={choosingPart ? t("garage.partMount.part") : t("garage.partMount.target")}
        >
          <SelectInput id="mount-choice" value={choice} onChange={(e) => setChoice(e.target.value)}>
            <option value="">—</option>
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </SelectInput>
        </FormField>
        {options.length === 0 ? (
          <p className="text-sm text-muted">
            {choosingPart ? t("garage.partMount.noParts") : t("garage.partMount.noTargets")}
          </p>
        ) : null}
        <FormField id="mount-date" label={t("garage.movement.date")}>
          <TextInput
            id="mount-date"
            type="date"
            max={today}
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
        </FormField>
        <FormField id="mount-note" label={t("garage.fields.notes")}>
          <TextArea
            id="mount-note"
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
            {busy ? t("garage.actions.saving") : t("garage.partMount.confirm")}
          </Button>
        </FormActions>
      </form>
    </Sheet>
  );
}
