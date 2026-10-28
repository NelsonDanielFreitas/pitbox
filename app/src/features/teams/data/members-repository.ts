import type { Team } from "../domain/team";
import type { AssignableTeamRole, TeamInvitation, TeamMember } from "../domain/team-schemas";

export interface MembersRepository {
  listMembers: (teamId: string) => Promise<TeamMember[]>;
  listInvitations: (teamId: string) => Promise<TeamInvitation[]>;
  inviteMember: (input: {
    teamId: string;
    requestId: string;
    role: AssignableTeamRole;
  }) => Promise<{ invitation: TeamInvitation; token: string }>;
  acceptInvitation: (token: string, requestId: string) => Promise<Team>;
  changeMemberRole: (input: {
    teamId: string;
    requestId: string;
    userId: string;
    role: AssignableTeamRole;
    version: number;
  }) => Promise<void>;
  removeMember: (input: {
    teamId: string;
    requestId: string;
    userId: string;
    version: number;
  }) => Promise<void>;
  revokeInvitation: (input: { teamId: string; requestId: string; invitationId: string }) => Promise<void>;
}
