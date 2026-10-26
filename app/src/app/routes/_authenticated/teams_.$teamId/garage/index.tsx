import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/garage/")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/teams/$teamId/garage/cars",
      params: { teamId: params.teamId },
    });
  },
});
