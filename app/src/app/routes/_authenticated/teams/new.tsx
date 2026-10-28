import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { TeamCreatePage } from "@/features/teams/ui/TeamCreatePage";

const createTeamSearchSchema = z.object({ step_up: z.literal("weak").optional() }).catch({});

export const Route = createFileRoute("/_authenticated/teams/new")({
  validateSearch: createTeamSearchSchema,
  component: CreateTeamRoute,
});

function CreateTeamRoute() {
  const { teamsRepository } = Route.useRouteContext();
  const search = Route.useSearch();
  return <TeamCreatePage repository={teamsRepository} weakStepUp={search.step_up === "weak"} />;
}
