import { createFileRoute } from "@tanstack/react-router";

import { PartFormPage } from "@/features/garage/ui/PartFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/parts/new")({
  component: NewPartRoute,
});

function NewPartRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <PartFormPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
