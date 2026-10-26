import { createFileRoute, redirect } from "@tanstack/react-router";

import { sessionQueryOptions, useSession } from "@/auth/session-query";
import { DatabaseGate } from "@/app/DatabaseGate";
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
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : undefined;
  if (!userId) return null;
  return (
    <DatabaseGate userId={userId}>
      <AppShell />
    </DatabaseGate>
  );
}
