import { createClient, type Client } from "@connectrpc/connect";

import { MemberService } from "@/gen/pitbox/identity/v1/member_service_pb";
import { TeamRole as ProtoTeamRole } from "@/gen/pitbox/identity/v1/team_service_pb";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { createPitboxTransport } from "@/shared/api/transport";
import { bootstrapTeams } from "@/sync/teams";

import type { Team } from "../domain/team";
import {
  assignableTeamRoleSchema,
  invitationSchema,
  memberSchema,
  type AssignableTeamRole,
  type TeamInvitation,
  type TeamMember,
} from "../domain/team-schemas";
import { mapTeamFromProto, roleFromProto } from "./connect-teams-repository";
import type { MembersRepository } from "./members-repository";
import { IdentityResponseError } from "./errors";

const maxPages = 10;
const pageSize = 200;

type MemberClient = Pick<
  Client<typeof MemberService>,
  | "listMembers"
  | "listInvitations"
  | "inviteMember"
  | "acceptInvitation"
  | "changeMemberRole"
  | "removeMember"
  | "revokeInvitation"
>;

export class PaginationLimitExceededError extends Error {
  constructor() {
    super("The team list exceeded the client pagination limit.");
    this.name = "PaginationLimitExceededError";
  }
}

export class ConnectMembersRepository implements MembersRepository {
  private readonly client: MemberClient;

  constructor(
    client: MemberClient = createClient(MemberService, createPitboxTransport()),
    private readonly refreshTeams: () => Promise<void> = () => bootstrapTeams(true),
  ) {
    this.client = client;
  }

  async listMembers(teamId: string): Promise<TeamMember[]> {
    const result: TeamMember[] = [];
    let pageToken = "";

    for (let page = 0; page < maxPages; page += 1) {
      const response = await this.client.listMembers({ teamId, pageSize, pageToken });
      for (const member of response.members) {
        const parsed = memberSchema.safeParse({
          userId: member.userId,
          displayName: member.displayName,
          role: roleFromProto(member.role),
          joinedAt: timestampToISO(member.joinedAt),
          version: member.version,
        });
        if (!parsed.success) throw new IdentityResponseError();
        result.push(parsed.data);
      }
      if (!response.nextPageToken) return result;
      pageToken = response.nextPageToken;
    }

    throw new PaginationLimitExceededError();
  }

  async listInvitations(teamId: string): Promise<TeamInvitation[]> {
    const result: TeamInvitation[] = [];
    let pageToken = "";

    for (let page = 0; page < maxPages; page += 1) {
      const response = await this.client.listInvitations({ teamId, pageSize, pageToken });
      for (const invitation of response.invitations) {
        const mapped = {
          id: invitation.id,
          email: invitation.email,
          role: roleFromProto(invitation.role),
          expiresAt: timestampToISO(invitation.expiresAt),
          createdAt: timestampToISO(invitation.createdAt),
        };
        const parsed = invitationSchema.safeParse(mapped);
        if (!parsed.success) throw new IdentityResponseError();
        result.push(parsed.data);
      }
      if (!response.nextPageToken) return result;
      pageToken = response.nextPageToken;
    }

    throw new PaginationLimitExceededError();
  }

  async inviteMember(input: {
    teamId: string;
    requestId: string;
    role: AssignableTeamRole;
  }): Promise<{ invitation: TeamInvitation; token: string }> {
    const role = roleToProto(input.role);
    const response = await this.client.inviteMember({
      teamId: input.teamId,
      requestId: input.requestId,
      role,
    });
    if (!response.invitation || response.token.length !== 43 || !/^[A-Za-z0-9_-]{43}$/.test(response.token)) {
      throw new IdentityResponseError();
    }
    const parsed = invitationSchema.safeParse({
      id: response.invitation.id,
      email: response.invitation.email,
      role: roleFromProto(response.invitation.role),
      expiresAt: timestampToISO(response.invitation.expiresAt),
      createdAt: timestampToISO(response.invitation.createdAt),
    });
    if (!parsed.success) throw new IdentityResponseError();
    return { invitation: parsed.data, token: response.token };
  }

  async acceptInvitation(token: string, requestId: string): Promise<Team> {
    const response = await this.client.acceptInvitation({ token, requestId });
    const accepted = response.membership;
    if (!accepted?.team) throw new IdentityResponseError();
    const membership = mapTeamFromProto(accepted.team, accepted.role);
    await this.refreshTeams();
    return membership;
  }

  // The response is not read: UpdateMemberRole returns the membership row without the
  // display name, so it cannot be validated as a listed member. A success means the change
  // was applied; the caller reloads the list, which is the source of truth.
  async changeMemberRole(input: {
    teamId: string;
    requestId: string;
    userId: string;
    role: AssignableTeamRole;
    version: number;
  }): Promise<void> {
    await this.client.changeMemberRole({
      teamId: input.teamId,
      requestId: input.requestId,
      userId: input.userId,
      role: roleToProto(input.role),
      version: input.version,
    });
  }

  async removeMember(input: {
    teamId: string;
    requestId: string;
    userId: string;
    version: number;
  }): Promise<void> {
    await this.client.removeMember(input);
  }

  async revokeInvitation(input: { teamId: string; requestId: string; invitationId: string }): Promise<void> {
    await this.client.revokeInvitation(input);
  }
}

function roleToProto(role: AssignableTeamRole): ProtoTeamRole {
  const parsed = assignableTeamRoleSchema.safeParse(role);
  if (!parsed.success) throw new IdentityResponseError();
  switch (parsed.data) {
    case "mechanic":
      return ProtoTeamRole.MECHANIC;
    case "pilot":
      return ProtoTeamRole.PILOT;
    case "guest":
      return ProtoTeamRole.GUEST;
  }
}

function timestampToISO(timestamp: Timestamp | undefined): string {
  if (!timestamp) throw new IdentityResponseError();
  const seconds = Number(timestamp.seconds);
  if (!Number.isSafeInteger(seconds) || !Number.isInteger(timestamp.nanos)) throw new IdentityResponseError();
  const date = new Date(seconds * 1000 + Math.floor(timestamp.nanos / 1_000_000));
  if (!Number.isFinite(date.getTime())) throw new IdentityResponseError();
  return date.toISOString();
}
