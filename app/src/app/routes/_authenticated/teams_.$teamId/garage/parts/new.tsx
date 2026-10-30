import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { PartFormPage } from "@/features/garage/ui/PartFormPage";
import { useTeamContext } from "@/features/teams/ui/team-context";

// Both values come from the URL, so they are validated and bounded; anything else is dropped.
const searchSchema = z.object({
  description: z.string().max(200).optional().catch(undefined),
  then: z.literal("shopping").optional().catch(undefined),
});

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/parts/new")({
  validateSearch: (search) => searchSchema.parse(search),
  component: NewPartRoute,
});

function NewPartRoute() {
  const team = useTeamContext();
  const { garageRepository } = Route.useRouteContext();
  const { description, then } = Route.useSearch();
  return (
    <PartFormPage
      teamId={team.id}
      role={team.role}
      repository={garageRepository}
      prefillDescription={description}
      thenShopping={then === "shopping"}
    />
  );
}
