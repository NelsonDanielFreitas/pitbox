import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { PartFormPage } from "@/features/garage/ui/PartFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/parts/$partId/edit")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.partId);
      return parsed.success ? { partId: parsed.data } : false;
    },
  },
  component: EditPartRoute,
});

function EditPartRoute() {
  const { partId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <PartFormPage
      teamId={team.id}
      partId={partId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
