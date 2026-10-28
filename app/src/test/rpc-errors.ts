import { Code, ConnectError } from "@connectrpc/connect";

// UI test files cannot import Connect directly (lint boundary); they build RPC failures here.
export const rpcNotFound = () => new ConnectError("not found", Code.NotFound);
export const rpcUnavailable = () => new ConnectError("unavailable", Code.Unavailable);
export const rpcAborted = () => new ConnectError("version conflict", Code.Aborted);
