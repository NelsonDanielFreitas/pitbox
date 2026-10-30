export type SetupConflictCode = "stale" | "constraint" | "no_changes";

export class SetupConflictError extends Error {
  constructor(
    readonly code: SetupConflictCode,
    message = "setup conflict",
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "SetupConflictError";
  }
}

export class SetupNotFoundError extends Error {
  constructor(message = "setup record not found", options?: ErrorOptions) {
    super(message, options);
    this.name = "SetupNotFoundError";
  }
}

/** A data violation of schema v1: carries paths and rule ids, never values. */
export class SetupInvalidError extends Error {
  constructor(
    readonly issues: readonly { path: string; ruleId: string }[],
    options?: ErrorOptions,
  ) {
    super("invalid setup data", options);
    this.name = "SetupInvalidError";
  }
}
