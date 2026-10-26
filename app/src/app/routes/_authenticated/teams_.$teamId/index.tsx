import { createFileRoute } from "@tanstack/react-router";

import { TeamHomePage } from "@/features/teams/ui/TeamHomePage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/")({
  component: TeamHomeRoute,
});

function TeamHomeRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <TeamHomePage team={team} repository={garageRepository} />;
}
