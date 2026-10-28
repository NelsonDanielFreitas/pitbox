import type { Team } from "../domain/team";

export interface CreateTeamInput {
  requestId: string;
  name: string;
  category: string;
  club?: string | undefined;
  countryCode: string;
}

export interface TeamsRepository {
  listMyTeams: () => Promise<Team[]>;
  createTeam: (input: CreateTeamInput) => Promise<Team>;
}
