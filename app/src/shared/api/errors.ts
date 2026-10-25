import { Code, ConnectError } from "@connectrpc/connect";

export function isUnauthenticated(error: unknown): boolean {
  return error instanceof ConnectError && error.code === Code.Unauthenticated;
}
