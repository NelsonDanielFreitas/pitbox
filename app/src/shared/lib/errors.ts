export class AuthUnavailableError extends Error {
  constructor() {
    super("authentication temporarily unavailable");
    this.name = "AuthUnavailableError";
  }
}

export class OriginMismatchError extends Error {
  constructor() {
    super("local app origin does not match the BFF");
    this.name = "OriginMismatchError";
  }
}

export class ContractError extends Error {
  constructor() {
    super("unexpected authentication response");
    this.name = "ContractError";
  }
}
