import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it, vi } from "vitest";

import {
  AcceptInvitationResponseSchema,
  ChangeMemberRoleResponseSchema,
  InvitationSchema,
  InviteMemberResponseSchema,
  ListMembersResponseSchema,
  MemberSchema,
} from "@/gen/pitbox/identity/v1/member_service_pb";
import { TeamRole, TeamMembershipSchema, TeamSchema } from "@/gen/pitbox/identity/v1/team_service_pb";

import { ConnectMembersRepository, PaginationLimitExceededError } from "./connect-members-repository";

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const ownerId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const pilotId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62";
const now = new Date("2026-10-28T12:00:00Z");

function member(userId: string, role: TeamRole) {
  return create(MemberSchema, {
    userId,
    displayName: role === TeamRole.OWNER ? "Dono" : "Piloto",
    role,
    joinedAt: timestampFromDate(now),
    version: 1,
  });
}

function makeClient() {
  return {
    listMembers: vi.fn(),
    listInvitations: vi.fn(),
    inviteMember: vi.fn(),
    acceptInvitation: vi.fn(),
    changeMemberRole: vi.fn(),
    removeMember: vi.fn(),
    revokeInvitation: vi.fn(),
  };
}

describe("ConnectMembersRepository", () => {
  it("follows member cursors and maps members without email", async () => {
    const client = makeClient();
    client.listMembers
      .mockResolvedValueOnce(create(ListMembersResponseSchema, { members: [member(ownerId, TeamRole.OWNER)], nextPageToken: "next" }))
      .mockResolvedValueOnce(create(ListMembersResponseSchema, { members: [member(pilotId, TeamRole.PILOT)] }));
    const repository = new ConnectMembersRepository(client);

    const members = await repository.listMembers(teamId);

    expect(members).toMatchObject([
      { userId: ownerId, displayName: "Dono", role: "owner", version: 1 },
      { userId: pilotId, displayName: "Piloto", role: "pilot", version: 1 },
    ]);
    expect(members[0]).not.toHaveProperty("email");
    expect(client.listMembers).toHaveBeenNthCalledWith(1, { teamId, pageSize: 200, pageToken: "" });
    expect(client.listMembers).toHaveBeenNthCalledWith(2, { teamId, pageSize: 200, pageToken: "next" });
  });

  it("fails explicitly rather than silently truncating after the page cap", async () => {
    const client = makeClient();
    client.listMembers.mockResolvedValue(create(ListMembersResponseSchema, { members: [], nextPageToken: "more" }));
    const repository = new ConnectMembersRepository(client);

    await expect(repository.listMembers(teamId)).rejects.toBeInstanceOf(PaginationLimitExceededError);
    expect(client.listMembers).toHaveBeenCalledTimes(10);
  });

  it("creates link invitations without an email and keeps the plaintext token only in the result", async () => {
    const client = makeClient();
    const token = "A".repeat(43);
    client.inviteMember.mockResolvedValue(create(InviteMemberResponseSchema, {
      invitation: create(InvitationSchema, {
        id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d63",
        role: TeamRole.PILOT,
        expiresAt: timestampFromDate(new Date(now.getTime() + 7 * 24 * 60 * 60 * 1000)),
        createdAt: timestampFromDate(now),
      }),
      token,
    }));
    const repository = new ConnectMembersRepository(client);

    const created = await repository.inviteMember({ teamId, requestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d64", role: "pilot" });

    expect(created.token).toBe(token);
    expect(created.invitation.email).toBeUndefined();
    expect(created.invitation.role).toBe("pilot");
    expect(client.inviteMember).toHaveBeenCalledWith({
      teamId,
      requestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d64",
      role: TeamRole.PILOT,
    });
  });

  it("accepts a link, then refreshes the complete local team snapshot", async () => {
    const client = makeClient();
    const refreshTeams = vi.fn().mockResolvedValue(undefined);
    client.acceptInvitation.mockResolvedValue(create(AcceptInvitationResponseSchema, {
      membership: create(TeamMembershipSchema, {
        team: create(TeamSchema, { id: teamId, name: "Equipa", category: "1/8 TT nitro", countryCode: "PT" }),
        role: TeamRole.PILOT,
      }),
    }));
    const repository = new ConnectMembersRepository(client, refreshTeams);
    const bearer = "B".repeat(43);
    const requestId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d65";

    await expect(repository.acceptInvitation(bearer, requestId)).resolves.toMatchObject({
      id: teamId,
      role: "pilot",
    });
    expect(client.acceptInvitation).toHaveBeenCalledWith({ token: bearer, requestId });
    expect(refreshTeams).toHaveBeenCalledOnce();
  });

  it("maps optimistic concurrency fields for changing a member role", async () => {
    const client = makeClient();
    client.changeMemberRole.mockResolvedValue(create(ChangeMemberRoleResponseSchema, {
      // The server omits display_name here, as the real UpdateMemberRole does.
      member: create(MemberSchema, {
        userId: pilotId,
        displayName: "",
        role: TeamRole.MECHANIC,
        joinedAt: timestampFromDate(now),
        version: 2,
      }),
    }));
    const repository = new ConnectMembersRepository(client);

    await repository.changeMemberRole({
      teamId,
      requestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d66",
      userId: pilotId,
      role: "mechanic",
      version: 1,
    });
    expect(client.changeMemberRole).toHaveBeenCalledWith({
      teamId,
      requestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d66",
      userId: pilotId,
      role: TeamRole.MECHANIC,
      version: 1,
    });
  });
});
