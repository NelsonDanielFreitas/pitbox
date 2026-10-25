import { createFileRoute } from "@tanstack/react-router";

import { TeamsPage } from "@/features/teams/ui/TeamsPage";

export const Route = createFileRoute("/_authenticated/teams")({
  component: TeamsRoute,
});

function TeamsRoute() {
  const { teamsRepository } = Route.useRouteContext();
  return <TeamsPage repository={teamsRepository} />;
}
