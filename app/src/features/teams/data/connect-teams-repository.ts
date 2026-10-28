import { createClient, type Client } from "@connectrpc/connect";

import { TeamRole as ProtoTeamRole, type Team as ProtoTeam } from "@/gen/pitbox/identity/v1/team_service_pb";
import { TeamService } from "@/gen/pitbox/identity/v1/team_service_pb";
import { createPitboxTransport } from "@/shared/api/transport";

import type { Team, TeamRole } from "../domain/team";
import type { CreateTeamInput } from "./teams-repository";
import { IdentityResponseError } from "./errors";

const maxPages = 5;
const pageSize = 200;

type TeamClient = Pick<Client<typeof TeamService>, "listMyTeams" | "createTeam">;

export class ConnectTeamsRepository {
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
          teams.push(mapTeamFromProto(membership.team, membership.role));
        }
      }

      if (!response.nextPageToken) {
        return teams;
      }
      pageToken = response.nextPageToken;
    }

    return teams;
  }

  async createTeam(input: CreateTeamInput): Promise<Team> {
    const response = await this.client.createTeam({
      requestId: input.requestId,
      name: input.name,
      category: input.category,
      club: input.club,
      countryCode: input.countryCode,
    });
    if (!response.team) throw new IdentityResponseError();
    return mapTeamFromProto(response.team, ProtoTeamRole.OWNER);
  }
}

export function mapTeamFromProto(team: ProtoTeam, role: ProtoTeamRole): Team {
  return {
    id: team.id,
    name: team.name,
    category: team.category,
    club: team.club,
    countryCode: team.countryCode,
    role: roleFromProto(role),
  };
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
