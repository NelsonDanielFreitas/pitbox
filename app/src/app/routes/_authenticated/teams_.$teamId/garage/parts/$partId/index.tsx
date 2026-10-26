import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { PartDetailPage } from "@/features/garage/ui/PartDetailPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/parts/$partId/")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.partId);
      return parsed.success ? { partId: parsed.data } : false;
    },
  },
  component: PartDetailRoute,
});

function PartDetailRoute() {
  const { partId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <PartDetailPage
      teamId={team.id}
      partId={partId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
