import { createFileRoute, Outlet } from "@tanstack/react-router";

import { NotFound } from "@/shared/ui/NotFound";
import { canReadGarage } from "@/features/garage/domain/roles";
import { GarageNav } from "@/features/garage/ui/GarageNav";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage")({
  component: GarageLayout,
});

function GarageLayout() {
  const team = useTeamContext();
  if (!canReadGarage(team.role)) return <NotFound />;
  return (
    <div>
      <GarageNav teamId={team.id} />
      <Outlet />
    </div>
  );
}
