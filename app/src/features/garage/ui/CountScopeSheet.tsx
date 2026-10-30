import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Sheet } from "@/shared/ui/Sheet";

import type { GarageRepository } from "../data/garage-repository";
import { partKinds, type PartKind } from "../domain/models";
import { garageErrorMessage } from "./errors";

/** Chooses what to count (every part or one kind) and opens the draft. */
export function CountScopeSheet({
  open,
  onClose,
  onStarted,
  teamId,
  repository,
}: {
  open: boolean;
  onClose: () => void;
  onStarted: () => void;
  teamId: string;
  repository: GarageRepository;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [scope, setScope] = useState<PartKind | "all">("all");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const start = async () => {
    setBusy(true);
    setError(null);
    try {
      await repository.startCount(teamId, scope === "all" ? null : scope);
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      onStarted();
    } catch (failure) {
      setError(garageErrorMessage(t, failure));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet
      open={open}
      title={t("garage.count.scopeTitle")}
      description={t("garage.count.scopeBody")}
      onClose={onClose}
    >
      <div className="space-y-4">
        <FormRootError error={error ?? undefined} />
        <FormField id="count-scope" label={t("garage.count.scope")}>
          <SelectInput
            id="count-scope"
            value={scope}
            onChange={(event) => {
              const value = event.target.value;
              setScope(value === "all" ? "all" : (partKinds.find((kind) => kind === value) ?? "all"));
            }}
          >
            <option value="all">{t("garage.count.allParts")}</option>
            {partKinds.map((kind) => (
              <option key={kind} value={kind}>
                {t(`garage.kinds.${kind}`)}
              </option>
            ))}
          </SelectInput>
        </FormField>
        <FormActions>
          <Button variant="secondary" full type="button" onClick={onClose}>
            {t("garage.actions.cancel")}
          </Button>
          <Button full disabled={busy} onClick={() => void start()}>
            {t("garage.count.start")}
          </Button>
        </FormActions>
      </div>
    </Sheet>
  );
}
