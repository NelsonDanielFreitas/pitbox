import { createFileRoute } from "@tanstack/react-router";

import { CarListPage } from "@/features/garage/ui/CarListPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/cars/")({
  component: CarsRoute,
});

function CarsRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <CarListPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
