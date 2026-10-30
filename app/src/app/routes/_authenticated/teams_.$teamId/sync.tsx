import { createFileRoute } from "@tanstack/react-router";

import { SyncPage } from "@/features/sync/ui/SyncPage";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/sync")({
  component: SyncPage,
});
