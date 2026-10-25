import type { QueryClient } from "@tanstack/react-query";

import { sessionQueryKey } from "@/auth/session-query";

import { isUnauthenticated } from "./errors";

interface CurrentLocation {
  pathname: string;
  searchStr: string;
}

interface QueryErrorDependencies {
  queryClient: QueryClient;
  getCurrentLocation: () => CurrentLocation | undefined;
  navigate: (options: { to: "/login"; search: { returnTo: string } }) => void;
}

export function handleQueryError(error: unknown, deps: QueryErrorDependencies): void {
  if (!isUnauthenticated(error)) {
    return;
  }

  deps.queryClient.removeQueries({ queryKey: sessionQueryKey });
  // The login route asks ensureQueryData; without a cached "unauthenticated" value it
  // would refetch /auth/session (still 200) and bounce straight back to /teams.
  deps.queryClient.setQueryData(sessionQueryKey, { authenticated: false });
  const current = deps.getCurrentLocation();
  const returnTo = current ? `${current.pathname}${current.searchStr}` : "/teams";
  deps.navigate({ to: "/login", search: { returnTo } });
}
