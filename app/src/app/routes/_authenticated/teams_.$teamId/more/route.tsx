import { createFileRoute, Outlet } from "@tanstack/react-router";

import { NotFound } from "@/shared/ui/NotFound";
import { canViewTeamSettings } from "@/features/teams/domain/roles";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/more")({
  component: MoreLayout,
});

function MoreLayout() {
  const team = useTeamContext();
  if (!canViewTeamSettings(team.role)) return <NotFound />;
  return <Outlet />;
}
