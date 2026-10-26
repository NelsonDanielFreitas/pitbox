import type { TFunction } from "i18next";

import { GarageConflictError, GarageNotFoundError } from "../domain/errors";

type ErrorKey =
  | "garage.conflicts.generic"
  | "garage.conflicts.transponder"
  | "garage.conflicts.tyreLabel"
  | "garage.conflicts.mount"
  | "garage.conflicts.stale"
  | "garage.conflicts.retired"
  | "garage.conflicts.constraint"
  | "garage.conflicts.notFound"
  | "garage.conflicts.busy";

export function garageErrorMessage(t: TFunction, error: unknown): string {
  let key: ErrorKey = "garage.conflicts.generic";
  if (error instanceof GarageConflictError) {
    switch (error.code) {
      case "transponder":
        key = "garage.conflicts.transponder";
        break;
      case "tyre_label":
        key = "garage.conflicts.tyreLabel";
        break;
      case "mount":
        key = "garage.conflicts.mount";
        break;
      case "stale":
        key = "garage.conflicts.stale";
        break;
      case "retired":
        key = "garage.conflicts.retired";
        break;
      case "constraint":
        key = "garage.conflicts.constraint";
        break;
    }
  } else if (error instanceof GarageNotFoundError) {
    key = "garage.conflicts.notFound";
  } else if (error instanceof Error && error.name === "LocalDatabaseBusyError") {
    key = "garage.conflicts.busy";
  }
  return t(key);
}
