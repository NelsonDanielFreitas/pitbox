import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { EngineDetailPage } from "@/features/garage/ui/EngineDetailPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/engines/$engineId/")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.engineId);
      return parsed.success ? { engineId: parsed.data } : false;
    },
  },
  component: EngineDetailRoute,
});

function EngineDetailRoute() {
  const { engineId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <EngineDetailPage
      teamId={team.id}
      engineId={engineId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
