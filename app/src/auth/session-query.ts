import { queryOptions, useQuery } from "@tanstack/react-query";

import { sessionQueryKey } from "./session-key";
import { loadSession } from "./session-load";

export { sessionQueryKey };

export const sessionQueryOptions = queryOptions({
  queryKey: sessionQueryKey,
  queryFn: ({ signal }) => loadSession(signal),
  retry: false,
  staleTime: 0,
  gcTime: 5 * 60_000,
});

export function useSession() {
  return useQuery(sessionQueryOptions);
}
