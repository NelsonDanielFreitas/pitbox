export class LocalDatabaseUnavailableError extends Error {
  constructor(message = "local database unavailable", options?: ErrorOptions) {
    super(message, options);
    this.name = "LocalDatabaseUnavailableError";
  }
}

export class LocalDatabaseBusyError extends Error {
  constructor(message = "local database busy", options?: ErrorOptions) {
    super(message, options);
    this.name = "LocalDatabaseBusyError";
  }
}

export class CorruptLocalDataError extends Error {
  constructor(message = "local data is corrupt", options?: ErrorOptions) {
    super(message, options);
    this.name = "CorruptLocalDataError";
  }
}

export class MigrationError extends Error {
  constructor(message = "local migration failed", options?: ErrorOptions) {
    super(message, options);
    this.name = "MigrationError";
  }
}
