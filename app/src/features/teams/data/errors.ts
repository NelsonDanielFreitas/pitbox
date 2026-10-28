import { Code, ConnectError } from "@connectrpc/connect";

export class IdentityResponseError extends Error {
  constructor() {
    super("the identity service returned an invalid response");
    this.name = "IdentityResponseError";
  }
}

/** Transport-independent classification so the UI never imports Connect. */
export type RpcErrorKind =
  | "unauthenticated"
  | "not_found"
  | "conflict"
  | "limit"
  | "invalid"
  | "unavailable"
  | "other";

export function classifyRpcError(error: unknown): RpcErrorKind {
  if (!(error instanceof ConnectError)) return "other";
  switch (error.code) {
    case Code.Unauthenticated:
      return "unauthenticated";
    case Code.NotFound:
      return "not_found";
    case Code.Aborted:
      return "conflict";
    case Code.FailedPrecondition:
      return "limit";
    case Code.InvalidArgument:
      return "invalid";
    case Code.Unavailable:
    case Code.DeadlineExceeded:
      return "unavailable";
    default:
      return "other";
  }
}
