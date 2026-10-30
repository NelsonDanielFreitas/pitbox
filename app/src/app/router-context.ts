import type { QueryClient } from "@tanstack/react-query";

import type { GarageRepository } from "@/features/garage/data/garage-repository";
import type { SetupRepository } from "@/features/setups/data/setup-repository";
import type { MembersRepository } from "@/features/teams/data/members-repository";
import type { TeamsRepository } from "@/features/teams/data/teams-repository";

export interface RouterContext {
  queryClient: QueryClient;
  teamsRepository: TeamsRepository;
  membersRepository: MembersRepository;
  garageRepository: GarageRepository;
  setupRepository: SetupRepository;
}
