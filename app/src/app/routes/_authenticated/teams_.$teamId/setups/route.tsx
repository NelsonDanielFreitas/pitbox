import { createFileRoute, Outlet } from "@tanstack/react-router";

import { canReadSetups } from "@/features/setups/domain/roles";
import { useTeamContext } from "@/features/teams/ui/team-context";
import { NotFound } from "@/shared/ui/NotFound";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/setups")({
  component: SetupsLayout,
});

function SetupsLayout() {
  const team = useTeamContext();
  if (!canReadSetups(team.role)) return <NotFound />;
  return <Outlet />;
}
