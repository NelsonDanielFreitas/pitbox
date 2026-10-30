import { z } from "zod";

import {
  feedbackScales,
  type FeedbackRatings,
  type FeedbackScale,
  type Setup,
  type SetupData,
  type SetupFeedback,
  type SetupFeedbackInput,
  type SetupVersion,
} from "./models";

export const MAX_NAME = 120;
export const MAX_NOTES = 2000;
export const MAX_CHANGE_SUMMARY = 500;
export const MAX_TRACK_ZONE = 80;

export const setupNameSchema = z
  .string()
  .trim()
  .min(1, { message: "setups.validation.required" })
  .max(MAX_NAME, { message: "setups.validation.tooLong" });

const optionalText = (max: number) =>
  z
    .string()
    .trim()
    .max(max, { message: "setups.validation.tooLong" })
    .transform((value) => (value === "" ? null : value));

export const setupNotesSchema = optionalText(MAX_NOTES);
export const changeSummarySchema = optionalText(MAX_CHANGE_SUMMARY);

export const setupFormSchema = z.object({
  name: setupNameSchema,
  notes: setupNotesSchema,
});
export type SetupFormValues = z.input<typeof setupFormSchema>;

const rating = z
  .number({ message: "setups.validation.rating" })
  .int({ message: "setups.validation.rating" })
  .min(1, { message: "setups.validation.rating" })
  .max(5, { message: "setups.validation.rating" });

/** Driver feedback: any of the eight scales, a track zone and a note, at least one filled in. */
export const feedbackInputSchema = z
  .object({
    ratings: z.object(Object.fromEntries(feedbackScales.map((scale) => [scale, rating.optional()]))).strict(),
    trackZone: optionalText(MAX_TRACK_ZONE),
    note: optionalText(MAX_NOTES),
  })
  .refine((value) => Object.values(value.ratings).some((v) => v !== undefined) || value.note !== null, {
    message: "setups.validation.feedbackEmpty",
    path: ["note"],
  });

export function parseFeedbackInput(input: {
  ratings: FeedbackRatings;
  trackZone: string;
  note: string;
}): SetupFeedbackInput {
  const parsed = feedbackInputSchema.parse(input);
  const ratings: FeedbackRatings = {};
  for (const scale of feedbackScales) {
    const value = parsed.ratings[scale];
    if (value !== undefined) ratings[scale] = value;
  }
  return { ratings, trackZone: parsed.trackZone, note: parsed.note };
}

/** SQL column of each feedback scale (the same names as the server's `driver_feedback`). */
export const feedbackColumns: Record<FeedbackScale, string> = {
  steeringEntry: "steering_entry",
  steeringMid: "steering_mid",
  steeringExit: "steering_exit",
  traction: "traction",
  jumpStability: "jump_stability",
  braking: "braking",
  engineLowEnd: "engine_low_end",
  engineTopEnd: "engine_top_end",
};

const nullableString = z.string().nullable();
const nullableRating = z.number().int().min(1).max(5).nullable();

export const setupRowSchema = z
  .object({
    id: z.uuid(),
    car_id: z.uuid(),
    name: z.string(),
    notes: nullableString,
    current_version: z.number().int().positive(),
    created_at: z.string(),
    updated_at: z.string(),
    created_by: nullableString,
    local_rev: z.number().int().nonnegative(),
  })
  .strict();

export const setupVersionRowSchema = z
  .object({
    setup_id: z.uuid(),
    version: z.number().int().positive(),
    data: z.string(),
    schema_version: z.number().int().positive(),
    based_on_setup_id: z.uuid().nullable(),
    based_on_version: z.number().int().positive().nullable(),
    change_summary: nullableString,
    created_at: z.string(),
    created_by: nullableString,
  })
  .strict();

export const setupFeedbackRowSchema = z
  .object({
    id: z.uuid(),
    setup_id: z.uuid(),
    version: z.number().int().positive(),
    author_id: z.uuid(),
    ...Object.fromEntries(Object.values(feedbackColumns).map((column) => [column, nullableRating])),
    track_zone: nullableString,
    note: nullableString,
    created_at: z.string(),
  })
  .strict();

export function setupFromRow(row: z.infer<typeof setupRowSchema>): Setup {
  return {
    id: row.id,
    carId: row.car_id,
    name: row.name,
    notes: row.notes,
    currentVersion: row.current_version,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    createdBy: row.created_by,
    localRev: row.local_rev,
  };
}

const dataSchema = z.record(z.string(), z.unknown());

/** Parses the stored JSON text; anything but an object is corrupt data. */
export function parseStoredData(raw: string): SetupData {
  return dataSchema.parse(JSON.parse(raw));
}

export function versionFromRow(row: z.infer<typeof setupVersionRowSchema>): SetupVersion {
  return {
    setupId: row.setup_id,
    version: row.version,
    data: parseStoredData(row.data),
    schemaVersion: row.schema_version,
    basedOn:
      row.based_on_setup_id !== null && row.based_on_version !== null
        ? { setupId: row.based_on_setup_id, version: row.based_on_version }
        : null,
    changeSummary: row.change_summary,
    createdAt: row.created_at,
    createdBy: row.created_by,
  };
}

export function feedbackFromRow(row: z.infer<typeof setupFeedbackRowSchema> & Record<string, unknown>): SetupFeedback {
  const ratings: FeedbackRatings = {};
  for (const scale of feedbackScales) {
    const value = row[feedbackColumns[scale]];
    if (typeof value === "number") ratings[scale] = value;
  }
  return {
    id: row.id,
    setupId: row.setup_id,
    version: row.version,
    authorId: row.author_id,
    ratings,
    trackZone: row.track_zone,
    note: row.note,
    createdAt: row.created_at,
  };
}
