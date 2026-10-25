import type { QueryClient } from "@tanstack/react-query";

import type { TeamsRepository } from "@/features/teams/data/teams-repository";

export interface RouterContext {
  queryClient: QueryClient;
  teamsRepository: TeamsRepository;
}
