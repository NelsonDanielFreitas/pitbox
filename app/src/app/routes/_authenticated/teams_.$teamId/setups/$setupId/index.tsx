import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { SetupDetailPage } from "@/features/setups/ui/SetupDetailPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/setups/$setupId/")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.setupId);
      return parsed.success ? { setupId: parsed.data } : false;
    },
  },
  validateSearch: z.object({ v: z.coerce.number().int().positive().optional().catch(undefined) }),
  component: SetupDetailRoute,
});

function SetupDetailRoute() {
  const { setupId } = Route.useParams();
  const { v } = Route.useSearch();
  const team = useTeamContext();
  const { setupRepository, garageRepository } = Route.useRouteContext();
  return (
    <SetupDetailPage
      teamId={team.id}
      setupId={setupId}
      version={v}
      role={team.role}
      repository={setupRepository}
      garageRepository={garageRepository}
    />
  );
}
