import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { TyreFormPage } from "@/features/garage/ui/TyreFormPage";

import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/tyres/$tyreSetId/edit")({
  params: {
    parse: (raw) => {
      const parsed = z.uuid().safeParse(raw.tyreSetId);
      return parsed.success ? { tyreSetId: parsed.data } : false;
    },
  },
  component: EditTyreRoute,
});

function EditTyreRoute() {
  const { tyreSetId } = Route.useParams();
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  return (
    <TyreFormPage
      teamId={team.id}
      tyreSetId={tyreSetId}
      role={team.role}
      repository={garageRepository}
    />
  );
}
