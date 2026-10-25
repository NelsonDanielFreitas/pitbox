import { QueryClient } from "@tanstack/react-query";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";

import { sessionQueryKey } from "@/auth/session-query";

import { handleQueryError } from "./query-error";

describe("handleQueryError", () => {
  it("removes the cached session and returns to login on Unauthenticated", () => {
    const queryClient = new QueryClient();
    const remove = vi.spyOn(queryClient, "removeQueries");
    const set = vi.spyOn(queryClient, "setQueryData");
    const navigate = vi.fn();

    handleQueryError(new ConnectError("revoked", Code.Unauthenticated), {
      queryClient,
      getCurrentLocation: () => ({ pathname: "/teams", searchStr: "?tab=all" }),
      navigate,
    });

    expect(remove).toHaveBeenCalledWith({ queryKey: sessionQueryKey });
    expect(set).toHaveBeenCalledWith(sessionQueryKey, { authenticated: false });
    expect(navigate).toHaveBeenCalledWith({
      to: "/login",
      search: { returnTo: "/teams?tab=all" },
    });
  });

  it("does nothing for other errors", () => {
    const queryClient = new QueryClient();
    const remove = vi.spyOn(queryClient, "removeQueries");
    const navigate = vi.fn();

    handleQueryError(new ConnectError("offline", Code.Unavailable), {
      queryClient,
      getCurrentLocation: () => ({ pathname: "/teams", searchStr: "" }),
      navigate,
    });

    expect(remove).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it("falls back to /teams when the router has no location yet", () => {
    const navigate = vi.fn();
    handleQueryError(new ConnectError("revoked", Code.Unauthenticated), {
      queryClient: new QueryClient(),
      getCurrentLocation: () => undefined,
      navigate,
    });
    expect(navigate).toHaveBeenCalledWith({ to: "/login", search: { returnTo: "/teams" } });
  });
});
