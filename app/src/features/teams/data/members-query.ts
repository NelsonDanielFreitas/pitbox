import { queryOptions } from "@tanstack/react-query";

import type { MembersRepository } from "./members-repository";

export const membersQueryKey = (teamId: string) => ["teams", teamId, "members"] as const;
export const invitationsQueryKey = (teamId: string) => ["teams", teamId, "invitations"] as const;

export function membersQueryOptions(repository: MembersRepository, teamId: string) {
  return queryOptions({
    queryKey: membersQueryKey(teamId),
    queryFn: () => repository.listMembers(teamId),
    staleTime: 0,
    networkMode: "always",
  });
}

export function invitationsQueryOptions(repository: MembersRepository, teamId: string) {
  return queryOptions({
    queryKey: invitationsQueryKey(teamId),
    queryFn: () => repository.listInvitations(teamId),
    staleTime: 0,
    networkMode: "always",
  });
}
