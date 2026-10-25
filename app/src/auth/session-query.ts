import { queryOptions, useQuery } from "@tanstack/react-query";

import { fetchSession } from "./session";

export const sessionQueryKey = ["auth", "session"] as const;

export const sessionQueryOptions = queryOptions({
  queryKey: sessionQueryKey,
  queryFn: ({ signal }) => fetchSession(signal),
  retry: false,
  staleTime: 0,
  gcTime: 5 * 60_000,
});

export function useSession() {
  return useQuery(sessionQueryOptions);
}
