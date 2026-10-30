import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { SetupComparePage } from "@/features/setups/ui/SetupComparePage";
import { useTeamContext } from "@/features/teams/ui/team-context";

const version = z.coerce.number().int().positive().optional().catch(undefined);

export const Route = createFileRoute("/_authenticated/teams_/$teamId/setups/$setupId/compare")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.setupId);
      return parsed.success ? { setupId: parsed.data } : false;
    },
  },
  validateSearch: z.object({ from: version, to: version }),
  component: CompareRoute,
});

function CompareRoute() {
  const { setupId } = Route.useParams();
  const { from, to } = Route.useSearch();
  const team = useTeamContext();
  const { setupRepository, garageRepository } = Route.useRouteContext();
  return (
    <SetupComparePage
      teamId={team.id}
      setupId={setupId}
      from={from}
      to={to}
      repository={setupRepository}
      garageRepository={garageRepository}
    />
  );
}
