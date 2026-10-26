import { createFileRoute } from "@tanstack/react-router";

import { EngineFormPage } from "@/features/garage/ui/EngineFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/engines/new")({
  component: NewEngineRoute,
});

function NewEngineRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return <EngineFormPage teamId={team.id} role={team.role} repository={garageRepository} />;
}
