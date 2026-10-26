import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { EngineFormPage } from "@/features/garage/ui/EngineFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/engines/$engineId/edit")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.engineId);
      return parsed.success ? { engineId: parsed.data } : false;
    },
  },
  component: EditEngineRoute,
});

function EditEngineRoute() {
  const { engineId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <EngineFormPage
      teamId={team.id}
      engineId={engineId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
