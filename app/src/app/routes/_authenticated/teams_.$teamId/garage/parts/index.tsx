import { createFileRoute } from "@tanstack/react-router";

import { PartListPage } from "@/features/garage/ui/PartListPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/parts/")({
  component: PartsRoute,
});

function PartsRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <PartListPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
