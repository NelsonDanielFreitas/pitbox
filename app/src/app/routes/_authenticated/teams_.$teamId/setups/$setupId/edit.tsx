import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { SetupEditPage } from "@/features/setups/ui/SetupEditPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/setups/$setupId/edit")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.setupId);
      return parsed.success ? { setupId: parsed.data } : false;
    },
  },
  component: SetupEditRoute,
});

function SetupEditRoute() {
  const { setupId } = Route.useParams();
  const team = useTeamContext();
  const { setupRepository, garageRepository } = Route.useRouteContext();
  return (
    <SetupEditPage
      teamId={team.id}
      setupId={setupId}
      role={team.role}
      repository={setupRepository}
      garageRepository={garageRepository}
    />
  );
}
