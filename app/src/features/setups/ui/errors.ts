import type { TFunction } from "i18next";

import { SetupConflictError, SetupInvalidError, SetupNotFoundError } from "../domain/errors";

export function setupErrorMessage(t: TFunction, error: unknown): string {
  if (error instanceof SetupConflictError) {
    switch (error.code) {
      case "stale":
        return t("setups.conflicts.stale");
      case "no_changes":
        return t("setups.conflicts.noChanges");
      case "constraint":
        return t("setups.conflicts.constraint");
    }
  }
  if (error instanceof SetupNotFoundError) return t("setups.conflicts.notFound");
  if (error instanceof SetupInvalidError) return t("setups.conflicts.invalid");
  if (error instanceof Error && error.name === "LocalDatabaseBusyError") return t("setups.conflicts.busy");
  return t("setups.conflicts.generic");
}
