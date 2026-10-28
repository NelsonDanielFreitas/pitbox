import { createFileRoute } from "@tanstack/react-router";

import { InvitationPage } from "@/features/teams/ui/InvitationPage";

export const Route = createFileRoute("/invite")({ component: InviteRoute });

function InviteRoute() {
  const { membersRepository } = Route.useRouteContext();
  return <InvitationPage repository={membersRepository} />;
}
