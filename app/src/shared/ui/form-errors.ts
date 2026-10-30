const validationKeys = [
  "garage.validation.required",
  "garage.validation.tooLong",
  "garage.validation.integer",
  "garage.validation.range",
  "garage.validation.date",
  "garage.validation.quantity",
  "garage.validation.wholeUnit",
  "garage.validation.status",
  "garage.validation.partKind",
  "garage.validation.unit",
  "garage.validation.price",
  "garage.validation.reorderBelowMin",
  "garage.validation.spec",
  "garage.validation.reason",
  "garage.validation.dateFuture",
  "garage.validation.lossReason",
  "garage.validation.priceOnlyPurchase",
] as const;
export type ValidationKey = (typeof validationKeys)[number];

function isValidationKey(value: string): value is ValidationKey {
  return (validationKeys as readonly string[]).includes(value);
}

export function fieldErrorText(
  translate: (key: ValidationKey) => string,
  message: string | undefined,
): string | undefined {
  if (!message) return undefined;
  return isValidationKey(message) ? translate(message) : message;
}

export function fieldDescribedBy(
  id: string,
  error: string | undefined,
  hint: string | undefined,
): string | undefined {
  if (error) return `${id}-error`;
  if (hint) return `${id}-hint`;
  return undefined;
}
