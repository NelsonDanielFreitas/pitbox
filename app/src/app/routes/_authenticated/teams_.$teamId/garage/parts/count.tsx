import { createFileRoute } from "@tanstack/react-router";

import { PartCountPage } from "@/features/garage/ui/PartCountPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/parts/count")({
  component: CountRoute,
});

function CountRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <PartCountPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
