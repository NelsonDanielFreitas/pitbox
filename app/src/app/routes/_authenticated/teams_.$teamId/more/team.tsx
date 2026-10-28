import { createFileRoute } from "@tanstack/react-router";

import { TeamManagementPage } from "@/features/teams/ui/TeamManagementPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/more/team")({
  component: TeamManagementRoute,
});

function TeamManagementRoute() {
  const team = useTeamContext();
  const { membersRepository } = Route.useRouteContext();
  return <TeamManagementPage team={team} repository={membersRepository} />;
}
