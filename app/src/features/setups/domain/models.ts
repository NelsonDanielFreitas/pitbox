/** A setup version's data: a JSON object shaped by schema v1 (never `null` values). */
export type SetupData = Record<string, unknown>;

export interface Setup {
  id: string;
  carId: string;
  name: string;
  notes: string | null;
  currentVersion: number;
  createdAt: string;
  updatedAt: string;
  createdBy: string | null;
  /** Local optimistic-concurrency counter for name/notes edits. */
  localRev: number;
}

export interface SetupVersion {
  setupId: string;
  version: number;
  data: SetupData;
  schemaVersion: number;
  basedOn: { setupId: string; version: number } | null;
  changeSummary: string | null;
  createdAt: string;
  createdBy: string | null;
}

export const feedbackScales = [
  "steeringEntry",
  "steeringMid",
  "steeringExit",
  "traction",
  "jumpStability",
  "braking",
  "engineLowEnd",
  "engineTopEnd",
] as const;
export type FeedbackScale = (typeof feedbackScales)[number];

export type FeedbackRatings = Partial<Record<FeedbackScale, number>>;

export interface SetupFeedback {
  id: string;
  setupId: string;
  version: number;
  authorId: string;
  ratings: FeedbackRatings;
  trackZone: string | null;
  note: string | null;
  createdAt: string;
}

export interface SetupFeedbackInput {
  ratings: FeedbackRatings;
  trackZone: string | null;
  note: string | null;
}

export type DifferenceKind = "added" | "removed" | "changed";

export interface Difference {
  path: string;
  kind: DifferenceKind;
  before?: unknown;
  after?: unknown;
}

export const SCHEMA_VERSION = 1;
export const MAX_DATA_BYTES = 64 << 10;
export const MAX_DATA_DEPTH = 8;
