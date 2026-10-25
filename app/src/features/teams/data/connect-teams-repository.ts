import { createClient, type Client } from "@connectrpc/connect";

import { TeamRole as ProtoTeamRole } from "@/gen/pitbox/identity/v1/team_service_pb";
import { TeamService } from "@/gen/pitbox/identity/v1/team_service_pb";
import { createPitboxTransport } from "@/shared/api/transport";

import type { Team, TeamRole } from "../domain/team";
import type { TeamsRepository } from "./teams-repository";

const maxPages = 5;
const pageSize = 200;

type TeamClient = Pick<Client<typeof TeamService>, "listMyTeams">;

export class ConnectTeamsRepository implements TeamsRepository {
  private readonly client: TeamClient;

  constructor(client: TeamClient = createClient(TeamService, createPitboxTransport())) {
    this.client = client;
  }

  async listMyTeams(): Promise<Team[]> {
    const teams: Team[] = [];
    let pageToken = "";

    for (let page = 0; page < maxPages; page += 1) {
      const response = await this.client.listMyTeams({ pageSize, pageToken });
      for (const membership of response.teams) {
        if (membership.team) {
          teams.push({
            id: membership.team.id,
            name: membership.team.name,
            category: membership.team.category,
            club: membership.team.club,
            countryCode: membership.team.countryCode,
            role: roleFromProto(membership.role),
          });
        }
      }

      if (!response.nextPageToken) {
        return teams;
      }
      pageToken = response.nextPageToken;
    }

    return teams;
  }
}

export function roleFromProto(role: ProtoTeamRole): TeamRole {
  switch (role) {
    case ProtoTeamRole.OWNER:
      return "owner";
    case ProtoTeamRole.MECHANIC:
      return "mechanic";
    case ProtoTeamRole.PILOT:
      return "pilot";
    case ProtoTeamRole.GUEST:
      return "guest";
    default:
      return "unknown";
  }
}
