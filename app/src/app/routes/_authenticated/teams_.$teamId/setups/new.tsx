import { createFileRoute } from "@tanstack/react-router";

import { NewSetupPage } from "@/features/setups/ui/NewSetupPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/setups/new")({
  component: NewSetupRoute,
});

function NewSetupRoute() {
  const team = useTeamContext();
  const { setupRepository, garageRepository } = Route.useRouteContext();
  return (
    <NewSetupPage
      teamId={team.id}
      role={team.role}
      repository={setupRepository}
      garageRepository={garageRepository}
    />
  );
}
