import { queryOptions } from "@tanstack/react-query";

import type { TeamsRepository } from "./teams-repository";

export const teamsQueryKey = ["teams", "mine"] as const;

export function teamsQueryOptions(repository: TeamsRepository) {
  return queryOptions({
    queryKey: teamsQueryKey,
    queryFn: () => repository.listMyTeams(),
    staleTime: 30_000,
    retry: 1,
  });
}
