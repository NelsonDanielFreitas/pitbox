import { createFileRoute } from "@tanstack/react-router";

import { TyreListPage } from "@/features/garage/ui/TyreListPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/tyres/")({
  component: TyresRoute,
});

function TyresRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <TyreListPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
