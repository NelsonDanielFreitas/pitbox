export type GarageConflictCode =
  | "transponder"
  | "tyre_label"
  | "mount"
  | "stale"
  | "retired"
  | "constraint"
  | "undone"
  | "install_closed"
  | "not_serviceable";

export class GarageConflictError extends Error {
  constructor(
    readonly code: GarageConflictCode,
    message = "garage conflict",
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "GarageConflictError";
  }
}

export class GarageNotFoundError extends Error {
  constructor(message = "garage record not found", options?: ErrorOptions) {
    super(message, options);
    this.name = "GarageNotFoundError";
  }
}
