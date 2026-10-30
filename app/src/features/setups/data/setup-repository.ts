import type {
  Setup,
  SetupData,
  SetupFeedback,
  SetupFeedbackInput,
  SetupVersion,
} from "../domain/models";

/** The version a new setup starts from (its data is copied). */
export interface SetupSource {
  setupId: string;
  version: number;
}

export interface CreateSetupInput {
  carId: string;
  name: string;
  notes: string | null;
  source: SetupSource | null;
  createdBy: string;
}

export interface CreateVersionInput {
  /** The version being edited: it must still be the current one. */
  baseVersion: number;
  data: SetupData;
  changeSummary: string | null;
  createdBy: string;
}

export interface SetupRepository {
  /** Setups of the team, most recently changed first; optionally only those of one car. */
  listSetups(teamId: string, carId?: string): Promise<Setup[]>;
  getSetup(teamId: string, setupId: string): Promise<Setup | undefined>;
  /** Versions of a setup, newest first. */
  listVersions(teamId: string, setupId: string): Promise<SetupVersion[]>;
  /** One version; the current one when `version` is omitted. */
  getVersion(teamId: string, setupId: string, version?: number): Promise<SetupVersion | undefined>;
  createSetup(
    teamId: string,
    input: CreateSetupInput,
  ): Promise<{ setup: Setup; version: SetupVersion }>;
  /** Saves a new immutable version; `SetupConflictError("stale")` if the base is no longer current. */
  createVersion(teamId: string, setupId: string, input: CreateVersionInput): Promise<SetupVersion>;
  updateSetup(
    teamId: string,
    setupId: string,
    localRev: number,
    input: { name: string; notes: string | null },
  ): Promise<Setup>;
  addFeedback(
    teamId: string,
    setupId: string,
    version: number,
    input: SetupFeedbackInput,
    authorId: string,
  ): Promise<SetupFeedback>;
  /** Feedback of a setup, newest first; of one version when `version` is given. */
  listFeedback(teamId: string, setupId: string, version?: number): Promise<SetupFeedback[]>;
}
