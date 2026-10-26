import type { QueryClient } from "@tanstack/react-query";

import type { GarageRepository } from "@/features/garage/data/garage-repository";
import type { TeamsRepository } from "@/features/teams/data/teams-repository";

export interface RouterContext {
  queryClient: QueryClient;
  teamsRepository: TeamsRepository;
  garageRepository: GarageRepository;
}
