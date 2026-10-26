import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { TyreDetailPage } from "@/features/garage/ui/TyreDetailPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/tyres/$tyreSetId/")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.tyreSetId);
      return parsed.success ? { tyreSetId: parsed.data } : false;
    },
  },
  component: TyreDetailRoute,
});

function TyreDetailRoute() {
  const { tyreSetId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <TyreDetailPage
      teamId={team.id}
      tyreSetId={tyreSetId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
