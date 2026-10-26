import { createFileRoute } from "@tanstack/react-router";

import { CarFormPage } from "@/features/garage/ui/CarFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/cars/new")({
  component: NewCarRoute,
});

function NewCarRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <CarFormPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
