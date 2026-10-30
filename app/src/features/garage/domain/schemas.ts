import { z } from "zod";

import {
  lossReasons,
  removalDestinations,
  scrapReasons,
  MAX_STOCK_CENTI,
  partKinds,
  partUnits,
  stockReasons,
  type PartUnit,
  type StockMovementInput,
} from "./models";
import { parseSpecInputs } from "./specs";
import { reasonSign } from "./stock";

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

const optionalCenti = z
  .string()
  .trim()
  .refine((value) => value === "" || /^\d+([.,]\d{0,2})?$/.test(value), {
    message: "garage.validation.quantity",
  })
  .transform((value) =>
    value === "" ? null : Math.round(Number(value.replace(",", ".")) * 100),
  )
  .pipe(
    z
      .number()
      .int({ message: "garage.validation.quantity" })
      .min(0, { message: "garage.validation.quantity" })
      .max(MAX_STOCK_CENTI, { message: "garage.validation.quantity" })
      .nullable(),
  );

/** Money typed in euros (at most two decimals), kept as integer cents. */
const optionalEuros = z
  .string()
  .trim()
  .refine((value) => value === "" || /^\d{1,7}([.,]\d{1,2})?$/.test(value), {
    message: "garage.validation.price",
  })
  .transform((value) =>
    value === "" ? null : Math.round(Number(value.replace(",", ".")) * 100),
  );

const partBaseSchema = z.object({
  kind: partKind,
  brand: optionalText(80),
  reference: optionalText(80),
  description: requiredText(200),
  unit: partUnit,
  minStock: nonNegativeCenti,
  reorderTo: optionalCenti,
  specs: z.record(z.string(), z.string()),
  unitCost: optionalEuros,
  hasServiceLife: z.boolean(),
});

const refinePartUnit = (
  value: {
    kind: string;
    unit: string;
    minStock: number;
    reorderTo: number | null;
    specs: Record<string, string>;
    initialQty?: number;
  },
  ctx: z.RefinementCtx,
) => {
  const whole = (path: string, centi: number | null | undefined) => {
    if (value.unit === "un" && centi != null && centi % 100 !== 0) {
      ctx.addIssue({ code: "custom", path: [path], message: "garage.validation.wholeUnit" });
    }
  };
  whole("minStock", value.minStock);
  whole("reorderTo", value.reorderTo);
  whole("initialQty", value.initialQty);
  if (value.reorderTo !== null && value.reorderTo < value.minStock) {
    ctx.addIssue({ code: "custom", path: ["reorderTo"], message: "garage.validation.reorderBelowMin" });
  }
  const parsed = parseSpecInputs(value.kind as (typeof partKinds)[number], value.specs);
  if (parsed.error) {
    ctx.addIssue({
      code: "custom",
      path: ["specs", parsed.error.key],
      message: "garage.validation.spec",
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

export interface MovementFormContext {
  unit: PartUnit;
  /** Today as yyyy-mm-dd in the user's calendar; a movement cannot be in the future. */
  today: string;
}

/** Form for a stock movement; the rules depend on the part unit and on today's date. */
export function movementFormSchema({ unit, today }: MovementFormContext) {
  return z
    .object({
      reason: z.enum(stockReasons, { error: "garage.validation.reason" }),
      direction: z.enum(["in", "out"]),
      qty: z
        .string()
        .trim()
        .regex(/^\d+([.,]\d{0,2})?$/, { message: "garage.validation.quantity" }),
      lossReason: z.union([z.enum(lossReasons), z.literal("")]),
      price: optionalEuros,
      store: optionalText(80),
      date: z
        .string()
        .trim()
        .refine(isIsoDate, { message: "garage.validation.date" })
        .refine((value) => value <= today, { message: "garage.validation.dateFuture" }),
      note: optionalText(500),
    })
    .superRefine((value, ctx) => {
      const centi = Math.round(Number(value.qty.replace(",", ".")) * 100);
      if (!Number.isFinite(centi) || centi <= 0 || centi > MAX_STOCK_CENTI) {
        ctx.addIssue({ code: "custom", path: ["qty"], message: "garage.validation.quantity" });
      } else if (unit === "un" && centi % 100 !== 0) {
        ctx.addIssue({ code: "custom", path: ["qty"], message: "garage.validation.wholeUnit" });
      }
      if ((value.reason === "lost") !== (value.lossReason !== "")) {
        ctx.addIssue({ code: "custom", path: ["lossReason"], message: "garage.validation.lossReason" });
      }
      if (value.reason !== "purchase") {
        if (value.price !== null) {
          ctx.addIssue({ code: "custom", path: ["price"], message: "garage.validation.priceOnlyPurchase" });
        }
        if (value.store !== null) {
          ctx.addIssue({ code: "custom", path: ["store"], message: "garage.validation.priceOnlyPurchase" });
        }
      }
    });
}

export type MovementFormValues = z.input<ReturnType<typeof movementFormSchema>>;
export type MovementFormOutput = z.output<ReturnType<typeof movementFormSchema>>;

/** Maps validated form output to the repository input. */
export function toMovementInput(
  partId: string,
  value: MovementFormOutput,
  createdBy: string | null,
): StockMovementInput {
  const centi = Math.round(Number(value.qty.replace(",", ".")) * 100);
  const sign = reasonSign[value.reason] || (value.direction === "in" ? 1 : -1);
  return {
    partId,
    reason: value.reason,
    qtyDeltaCenti: sign * centi,
    lossReason: value.lossReason === "" ? null : value.lossReason,
    unitCostCents: value.reason === "purchase" ? value.price : null,
    store: value.reason === "purchase" ? value.store : null,
    occurredOn: value.date,
    note: value.note,
    createdBy,
  };
}

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

const specsText = z.string().transform((text, ctx) => {
  try {
    const parsed = z
      .record(z.string(), z.union([z.string(), z.number()]))
      .safeParse(JSON.parse(text));
    if (parsed.success) return parsed.data;
  } catch {
    // Falls through to the issue below.
  }
  ctx.addIssue({ code: "custom", message: "invalid part specs" });
  return z.NEVER;
});

export const partRowSchema = z
  .object({
    id: z.uuid(),
    team_id: z.uuid(),
    kind: partKind,
    brand: nullableString,
    reference: nullableString,
    description: z.string(),
    unit: partUnit,
    specs: specsText,
    min_stock_centi: z.number().int().nonnegative(),
    reorder_to_centi: z.number().int().nonnegative().nullable(),
    unit_cost_cents: z.number().int().nonnegative().nullable().optional(),
    has_service_life: sqliteBool,
    created_at: z.string(),
    updated_at: z.string(),
    version: z.number().int().positive(),
    local_rev: z.number().int().nonnegative(),
    qty_centi: z.number().int(),
    below_min: sqliteBool,
  })
  .strict();

export const stockMovementRowSchema = z
  .object({
    id: z.uuid(),
    part_id: z.uuid(),
    qty_delta_centi: z.number().int(),
    reason: z.enum(stockReasons),
    loss_reason: z.enum(lossReasons).nullable(),
    note: nullableString,
    store: nullableString,
    unit_cost_cents: z.number().int().nonnegative().nullable().optional(),
    occurred_at: z.string(),
    created_at: z.string(),
    created_by: nullableString,
    reverts_movement_id: nullableString,
    undone: sqliteBool,
    /** Stock of the part right after the movement; only the write queries select it. */
    part_qty_centi: z.number().int().optional(),
    part_min_stock_centi: z.number().int().optional(),
  })
  .strict();

export const partInstallRowSchema = z
  .object({
    id: z.uuid(),
    part_id: z.uuid(),
    part_kind: z.enum(partKinds),
    part_description: z.string(),
    part_brand: nullableString,
    part_reference: nullableString,
    car_id: nullableString,
    engine_id: nullableString,
    target_name: z.string(),
    installed_at: z.string(),
    removed_at: nullableString,
    removal_destination: z.enum(removalDestinations).nullable(),
    removal_reason: z.enum(scrapReasons).nullable(),
    run_seconds: z.number().int().nonnegative(),
    sessions: z.number().int().nonnegative(),
    note: nullableString,
  })
  .strict();

export const countDraftRowSchema = z
  .object({
    scope_kind: z.enum(partKinds).nullable(),
    started_at: z.string(),
    movements_since: z.number().int().nonnegative(),
  })
  .strict();

export const countLineRowSchema = z
  .object({ part_id: z.uuid(), counted_centi: z.number().int().nonnegative() })
  .strict();

/** The stored exception line of a part, as the sync queue needs to see it before a change. */
export const shoppingItemStateRowSchema = z
  .object({
    id: z.uuid(),
    version: z.number().int().min(1),
    local_rev: z.number().int().nonnegative(),
    qty_centi: z.number().int().positive().nullable(),
    dismissed: sqliteBool,
  })
  .strict();

export const shoppingLineRowSchema = z
  .object({
    part_id: z.uuid(),
    kind: z.enum(partKinds),
    description: z.string(),
    brand: nullableString,
    reference: nullableString,
    unit: partUnit,
    stock_centi: z.number().int(),
    min_stock_centi: z.number().int().nonnegative(),
    reorder_to_centi: z.number().int().nonnegative().nullable(),
    below_min: sqliteBool,
    item_qty_centi: z.number().int().positive().nullable(),
    item_note: nullableString,
    unit_cost_cents: z.number().int().nonnegative().nullable().optional(),
  })
  .strict();
