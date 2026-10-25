import type { Team } from "../domain/team";

export interface TeamsRepository {
  listMyTeams(): Promise<Team[]>;
}
