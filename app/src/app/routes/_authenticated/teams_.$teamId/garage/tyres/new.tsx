import { createFileRoute } from "@tanstack/react-router";

import { TyreFormPage } from "@/features/garage/ui/TyreFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/tyres/new")({
  component: NewTyreRoute,
});

function NewTyreRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <TyreFormPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
