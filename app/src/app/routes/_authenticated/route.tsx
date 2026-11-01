import { createFileRoute, redirect } from "@tanstack/react-router";

import { sessionQueryOptions, useSession } from "@/auth/session-query";
import { useSessionRecovery } from "@/auth/use-session-recovery";
import { DatabaseGate } from "@/app/DatabaseGate";
import { StorageNotices } from "@/app/StorageNotices";
import { RootError } from "@/app/routes/__root";
import { NetworkError } from "@/shared/lib/errors";
import { AppShell } from "@/shared/ui/AppShell";
import { OfflineBlocked } from "@/shared/ui/OfflineBlocked";

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
  // No network and no valid offline pass: say so and show no data (ADR 0028).
  errorComponent: ({ error }) =>
    error instanceof NetworkError ? <OfflineBlocked /> : <RootError error={error} />,
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : undefined;
  const offlineUserId = session.data?.authenticated && session.data.offline ? userId : undefined;
  useSessionRecovery(offlineUserId);
  if (!userId) return null;
  return (
    // key: another user starts with a fresh gate, so the previous user's screens unmount.
    <DatabaseGate key={userId} userId={userId}>
      <AppShell notices={<StorageNotices />} />
    </DatabaseGate>
  );
}
