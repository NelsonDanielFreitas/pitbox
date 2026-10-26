import { z } from "zod";

import { MAX_STOCK_CENTI, partKinds, partUnits } from "./models";

const requiredText = (max: number) =>
  z
    .string()
    .trim()
    .min(1, { message: "garage.validation.required" })
    .max(max, { message: "garage.validation.tooLong" });

const optionalText = (max: number) =>
  z
    .string()
    .trim()
    .max(max, { message: "garage.validation.tooLong" })
    .transform((value) => (value === "" ? null : value));

const optionalInteger = (min: number, max: number) =>
  z
    .string()
    .trim()
    .refine((value) => value === "" || /^\d{1,4}$/.test(value), {
      message: "garage.validation.integer",
    })
    .transform((value) => (value === "" ? null : Number(value)))
    .pipe(
      z
        .number()
        .int({ message: "garage.validation.integer" })
        .min(min, { message: "garage.validation.range" })
        .max(max, { message: "garage.validation.range" })
        .nullable(),
    );

function isIsoDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value;
}

const optionalDate = z
  .string()
  .trim()
  .refine((value) => value === "" || isIsoDate(value), {
    message: "garage.validation.date",
  })
  .transform((value) => (value === "" ? null : value));

const nonNegativeCenti = z
  .string()
  .trim()
  .refine((value) => value === "" || /^\d+([.,]\d{0,2})?$/.test(value), {
    message: "garage.validation.quantity",
  })
  .transform((value) => {
    if (value === "") return 0;
    return Math.round(Number(value.replace(",", ".")) * 100);
  })
  .pipe(
    z
      .number()
      .int({ message: "garage.validation.quantity" })
      .min(0, { message: "garage.validation.quantity" })
      .max(MAX_STOCK_CENTI, { message: "garage.validation.quantity" }),
  );

const assetStatus = z.enum(["active", "spare", "retired"], {
  error: "garage.validation.status",
});
const tyreStatus = z.enum(["new", "used", "worn", "retired"], {
  error: "garage.validation.status",
});
const partKind = z.enum(partKinds, { error: "garage.validation.partKind" });
const partUnit = z.enum(partUnits, { error: "garage.validation.unit" });

export const carFormSchema = z.object({
  make: requiredText(80),
  model: requiredText(80),
  nickname: optionalText(40),
  transponderNumber: optionalText(64),
  modelYear: optionalInteger(1990, 2100),
  serialNumber: optionalText(64),
  notes: optionalText(2000),
  status: assetStatus,
});

export type CarFormValues = z.input<typeof carFormSchema>;
export type CarFormOutput = z.output<typeof carFormSchema>;

export const engineFormSchema = z.object({
  make: requiredText(80),
  model: requiredText(80),
  serialNumber: optionalText(64),
  purchasedOn: optionalDate,
  status: assetStatus,
  notes: optionalText(2000),
});

export type EngineFormValues = z.input<typeof engineFormSchema>;
export type EngineFormOutput = z.output<typeof engineFormSchema>;

export const tyreFormSchema = z.object({
  label: z
    .string()
    .trim()
    .min(1, { message: "garage.validation.required" })
    .max(20, { message: "garage.validation.tooLong" })
    .transform((value) => value.toUpperCase()),
  make: requiredText(80),
  model: requiredText(80),
  compound: optionalText(40),
  insertType: optionalText(40),
  gluedOn: optionalDate,
  status: tyreStatus,
  wearNotes: optionalText(2000),
});

export type TyreFormValues = z.input<typeof tyreFormSchema>;
export type TyreFormOutput = z.output<typeof tyreFormSchema>;

const partBaseSchema = z.object({
  kind: partKind,
  brand: optionalText(80),
  reference: optionalText(80),
  description: requiredText(200),
  unit: partUnit,
  minStock: nonNegativeCenti,
  hasServiceLife: z.boolean(),
});

const refinePartUnit = (
  value: { unit: string; minStock: number; initialQty?: number },
  ctx: z.RefinementCtx,
) => {
  if (value.unit !== "un") return;
  if (value.minStock % 100 !== 0) {
    ctx.addIssue({
      code: "custom",
      path: ["minStock"],
      message: "garage.validation.wholeUnit",
    });
  }
  if (value.initialQty !== undefined && value.initialQty % 100 !== 0) {
    ctx.addIssue({
      code: "custom",
      path: ["initialQty"],
      message: "garage.validation.wholeUnit",
    });
  }
};

export const partFormSchema = partBaseSchema
  .extend({ initialQty: nonNegativeCenti })
  .superRefine(refinePartUnit);

export const partUpdateFormSchema = partBaseSchema.superRefine(refinePartUnit);

export type PartFormValues = z.input<typeof partFormSchema>;
export type PartFormOutput = z.output<typeof partFormSchema>;
export type PartUpdateFormValues = z.input<typeof partUpdateFormSchema>;
export type PartUpdateFormOutput = z.output<typeof partUpdateFormSchema>;

const nullableString = z.string().nullable();
const sqliteBool = z.union([z.literal(0), z.literal(1)]);

export const carRowSchema = z
  .object({
    id: z.uuid(),
    team_id: z.uuid(),
    make: z.string(),
    model: z.string(),
    model_year: z.number().int().nullable(),
    serial_number: nullableString,
    nickname: nullableString,
    transponder_number: nullableString,
    status: assetStatus,
    scale: z.string(),
    powertrain: z.string(),
    notes: nullableString,
    created_at: z.string(),
    updated_at: z.string(),
    version: z.number().int().positive(),
    local_rev: z.number().int().nonnegative(),
  })
  .strict();

export const engineRowSchema = z
  .object({
    id: z.uuid(),
    team_id: z.uuid(),
    make: z.string(),
    model: z.string(),
    serial_number: nullableString,
    purchased_on: nullableString,
    status: assetStatus,
    tanks_total: z.number().nonnegative(),
    run_seconds_total: z.number().int().nonnegative(),
    sessions_total: z.number().int().nonnegative(),
    notes: nullableString,
    created_at: z.string(),
    updated_at: z.string(),
    version: z.number().int().positive(),
    local_rev: z.number().int().nonnegative(),
  })
  .strict();

export const engineWithMountRowSchema = engineRowSchema.extend({
  current_car_id: nullableString,
  current_mounted_at: nullableString,
  current_car_make: nullableString,
  current_car_model: nullableString,
  current_car_nickname: nullableString,
  current_car_status: assetStatus.nullable(),
});

export const engineMountRowSchema = z
  .object({
    id: z.uuid(),
    engine_id: z.uuid(),
    car_id: z.uuid(),
    mounted_at: z.string(),
    unmounted_at: nullableString,
    car_make: z.string(),
    car_model: z.string(),
    car_nickname: nullableString,
    car_status: assetStatus,
  })
  .strict();

export const tyreSetRowSchema = z
  .object({
    id: z.uuid(),
    team_id: z.uuid(),
    label: z.string(),
    make: z.string(),
    model: z.string(),
    compound: nullableString,
    insert_type: nullableString,
    glued_on: nullableString,
    status: tyreStatus,
    sessions_total: z.number().int().nonnegative(),
    run_seconds_total: z.number().int().nonnegative(),
    wear_notes: nullableString,
    created_at: z.string(),
    updated_at: z.string(),
    version: z.number().int().positive(),
    local_rev: z.number().int().nonnegative(),
  })
  .strict();

export const partRowSchema = z
  .object({
    id: z.uuid(),
    team_id: z.uuid(),
    kind: partKind,
    brand: nullableString,
    reference: nullableString,
    description: z.string(),
    unit: partUnit,
    min_stock_centi: z.number().int().nonnegative(),
    has_service_life: sqliteBool,
    created_at: z.string(),
    updated_at: z.string(),
    version: z.number().int().positive(),
    local_rev: z.number().int().nonnegative(),
    qty_centi: z.number().int(),
    below_min: sqliteBool,
  })
  .strict();
