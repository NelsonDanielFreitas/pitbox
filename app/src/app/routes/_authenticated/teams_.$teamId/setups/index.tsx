import { createFileRoute } from "@tanstack/react-router";

import { SetupListPage } from "@/features/setups/ui/SetupListPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/setups/")({
  component: SetupsRoute,
});

function SetupsRoute() {
  const team = useTeamContext();
  const { setupRepository, garageRepository } = Route.useRouteContext();
  return (
    <SetupListPage
      teamId={team.id}
      role={team.role}
      repository={setupRepository}
      garageRepository={garageRepository}
    />
  );
}
