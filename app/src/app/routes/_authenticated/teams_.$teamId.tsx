import { createFileRoute, notFound } from "@tanstack/react-router";
import { z } from "zod";

import { teamsQueryOptions } from "@/features/teams/data/teams-query";
import { TeamHomePage } from "@/features/teams/ui/TeamHomePage";

const teamIdSchema = z.uuid();

export const Route = createFileRoute("/_authenticated/teams_/$teamId")({
  params: {
    parse: (raw) => {
      const parsed = teamIdSchema.safeParse(raw.teamId);
      return parsed.success ? { teamId: parsed.data } : false;
    },
  },
  loader: async ({ context, params }) => {
    const teams = await context.queryClient.ensureQueryData(
      teamsQueryOptions(context.teamsRepository),
    );
    const team = teams.find((candidate) => candidate.id === params.teamId);
    if (!team) {
      throw notFound();
    }
    return { team };
  },
  component: TeamRoute,
});

function TeamRoute() {
  const { team } = Route.useLoaderData();
  return <TeamHomePage team={team} />;
}
