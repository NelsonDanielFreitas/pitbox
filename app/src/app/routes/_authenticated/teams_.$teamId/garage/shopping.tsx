import { createFileRoute } from "@tanstack/react-router";

import { ShoppingListPage } from "@/features/garage/ui/ShoppingListPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/shopping")({
  component: ShoppingRoute,
});

function ShoppingRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <ShoppingListPage
      teamId={team.id}
      teamName={team.name}
      role={team.role}
      repository={garageRepository}
    />
  );
}
