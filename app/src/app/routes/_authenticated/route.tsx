import { createFileRoute, redirect } from "@tanstack/react-router";

import { sessionQueryOptions } from "@/auth/session-query";
import { AppShell } from "@/shared/ui/AppShell";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: async ({ context, location }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions);
    if (!session.authenticated) {
      throw redirect({
        to: "/login",
        search: { returnTo: `${location.pathname}${location.searchStr}` },
      });
    }
  },
  component: AppShell,
});
