import { createFileRoute } from "@tanstack/react-router";

import { EngineListPage } from "@/features/garage/ui/EngineListPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/engines/")({
  component: EnginesRoute,
});

function EnginesRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <EngineListPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
