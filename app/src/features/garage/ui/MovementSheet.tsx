import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { FormActions } from "@/shared/ui/FormActions";
import { FormField, FormRootError } from "@/shared/ui/FormField";
import { fieldDescribedBy } from "@/shared/ui/form-errors";
import { SelectInput } from "@/shared/ui/SelectInput";
import { Sheet } from "@/shared/ui/Sheet";
import { TextArea } from "@/shared/ui/TextArea";
import { TextInput } from "@/shared/ui/TextInput";

import type { GarageRepository } from "../data/garage-repository";
import { formatQuantity, unitLabel } from "../domain/format";
import {
  lossReasons,
  type PartWithStock,
  type StockChange,
  type StockReason,
} from "../domain/models";
import {
  movementFormSchema,
  toMovementInput,
  type MovementFormOutput,
  type MovementFormValues,
} from "../domain/schemas";
import { reasonSign } from "../domain/stock";
import { todayIso } from "../domain/today";
import { garageErrorMessage } from "./errors";

/** Reasons the user picks by hand; `installed` and `returned` come from mounting parts. */
const manualReasons = ["purchase", "consumed", "lost", "adjustment"] as const;

export function MovementSheet({
  open,
  onClose,
  onSaved,
  teamId,
  part,
  repository,
  userId,
  showCosts,
  initialReason = "consumed",
  initialQty = "",
}: {
  open: boolean;
  onClose: () => void;
  onSaved?: (change: StockChange) => void;
  teamId: string;
  part: PartWithStock;
  repository: GarageRepository;
  userId: string | null;
  showCosts: boolean;
  initialReason?: StockReason;
  /** Quantity typed in advance, e.g. the amount from the shopping list. */
  initialQty?: string;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const today = useMemo(() => todayIso(), []);
  const schema = useMemo(
    () => movementFormSchema({ unit: part.unit, today }),
    [part.unit, today],
  );
  const {
    register,
    handleSubmit,
    setError,
    reset,
    control,
    formState: { errors, isSubmitting },
  } = useForm<MovementFormValues, unknown, MovementFormOutput>({
    resolver: zodResolver(schema),
    defaultValues: {
      reason: initialReason,
      direction: "in",
      qty: initialQty,
      lossReason: "",
      price: "",
      store: "",
      date: today,
      note: "",
    },
    mode: "onTouched",
    reValidateMode: "onChange",
    shouldFocusError: true,
  });
  const reason = useWatch({ control, name: "reason" });
  const direction = useWatch({ control, name: "direction" });
  const qty = useWatch({ control, name: "qty" });

  const sign = reasonSign[reason] || (direction === "in" ? 1 : -1);
  const typed = Number(qty.replace(",", "."));
  const after = Number.isFinite(typed) && qty.trim() !== ""
    ? part.qtyCenti + sign * Math.round(typed * 100)
    : null;

  const close = () => {
    reset();
    onClose();
  };

  const onSubmit = async (values: MovementFormOutput) => {
    try {
      const change = await repository.addStockMovement(
        teamId,
        toMovementInput(part.id, values, userId),
      );
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      onSaved?.(change);
      close();
    } catch (error) {
      setError("root", { message: garageErrorMessage(t, error) });
    }
  };

  return (
    <Sheet open={open} title={t("garage.movement.title")} description={part.description} onClose={close}>
      <form noValidate onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <FormRootError error={errors.root?.message} />
        <FormField id="mv-reason" label={t("garage.movement.reason")} error={errors.reason?.message}>
          <SelectInput id="mv-reason" {...register("reason")}>
            {manualReasons.map((value) => (
              <option key={value} value={value}>
                {t(`garage.movement.reasons.${value}`)}
              </option>
            ))}
          </SelectInput>
        </FormField>
        {reason === "adjustment" ? (
          <FormField id="mv-direction" label={t("garage.movement.direction")}>
            <SelectInput id="mv-direction" {...register("direction")}>
              <option value="in">{t("garage.movement.directionIn")}</option>
              <option value="out">{t("garage.movement.directionOut")}</option>
            </SelectInput>
          </FormField>
        ) : null}
        <FormField
          id="mv-qty"
          label={t("garage.movement.qty")}
          unit={unitLabel(part.unit)}
          error={errors.qty?.message}
        >
          <TextInput
            id="mv-qty"
            inputMode="decimal"
            invalid={Boolean(errors.qty)}
            aria-describedby={fieldDescribedBy("mv-qty", errors.qty?.message, undefined)}
            {...register("qty")}
          />
        </FormField>
        {after !== null ? (
          <p className="text-sm text-muted" aria-live="polite">
            {t("garage.movement.after", {
              qty: formatQuantity(after, part.unit, i18n.language),
            })}
          </p>
        ) : null}
        {after !== null && after < 0 ? (
          <Alert tone="warn">{t("garage.movement.negativeStock")}</Alert>
        ) : null}
        {reason === "lost" ? (
          <FormField id="mv-loss" label={t("garage.movement.lossReason")} error={errors.lossReason?.message}>
            <SelectInput id="mv-loss" invalid={Boolean(errors.lossReason)} {...register("lossReason")}>
              <option value="">—</option>
              {lossReasons.map((value) => (
                <option key={value} value={value}>
                  {t(`garage.movement.loss.${value}`)}
                </option>
              ))}
            </SelectInput>
          </FormField>
        ) : null}
        {reason === "purchase" ? (
          <>
            {showCosts ? (
              <FormField id="mv-price" label={t("garage.movement.price")} unit="€" error={errors.price?.message}>
                <TextInput
                  id="mv-price"
                  inputMode="decimal"
                  invalid={Boolean(errors.price)}
                  aria-describedby={fieldDescribedBy("mv-price", errors.price?.message, undefined)}
                  {...register("price")}
                />
              </FormField>
            ) : null}
            <FormField id="mv-store" label={t("garage.movement.store")} error={errors.store?.message}>
              <TextInput id="mv-store" invalid={Boolean(errors.store)} {...register("store")} />
            </FormField>
          </>
        ) : null}
        <FormField id="mv-date" label={t("garage.movement.date")} error={errors.date?.message}>
          <TextInput
            id="mv-date"
            type="date"
            max={today}
            invalid={Boolean(errors.date)}
            aria-describedby={fieldDescribedBy("mv-date", errors.date?.message, undefined)}
            {...register("date")}
          />
        </FormField>
        <FormField id="mv-note" label={t("garage.fields.notes")} error={errors.note?.message}>
          <TextArea id="mv-note" className="min-h-20" invalid={Boolean(errors.note)} {...register("note")} />
        </FormField>
        <FormActions>
          <Button variant="secondary" full type="button" onClick={close}>
            {t("garage.actions.cancel")}
          </Button>
          <Button type="submit" full disabled={isSubmitting}>
            {isSubmitting ? t("garage.actions.saving") : t("garage.actions.save")}
          </Button>
        </FormActions>
      </form>
    </Sheet>
  );
}
