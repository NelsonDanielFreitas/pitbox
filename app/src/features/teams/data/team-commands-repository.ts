import { bootstrapTeams } from "@/sync/teams";

import { ConnectTeamsRepository } from "./connect-teams-repository";
import type { CreateTeamInput, TeamsRepository } from "./teams-repository";
import { LocalTeamsRepository } from "./local-teams-repository";

export class AppTeamsRepository implements TeamsRepository {
  constructor(
    private readonly local: Pick<TeamsRepository, "listMyTeams"> = new LocalTeamsRepository(),
    private readonly remote: Pick<ConnectTeamsRepository, "createTeam"> = new ConnectTeamsRepository(),
    private readonly refresh = () => bootstrapTeams(true),
  ) {}

  listMyTeams() {
    return this.local.listMyTeams();
  }

  async createTeam(input: CreateTeamInput) {
    const team = await this.remote.createTeam(input);
    await this.refresh();
    return team;
  }
}
