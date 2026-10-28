import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { safeReturnTo } from "@/auth/session";
import { ConfirmEmailPage } from "@/features/auth/ui/ConfirmEmailPage";

const confirmEmailSearchSchema = z
  .object({
    return_to: z.string().optional(),
  })
  .catch({});

export const Route = createFileRoute("/confirm-email")({
  validateSearch: confirmEmailSearchSchema,
  component: ConfirmEmailRoute,
});

function ConfirmEmailRoute() {
  const search = Route.useSearch();
  return <ConfirmEmailPage returnTo={safeReturnTo(search.return_to ?? "/teams")} />;
}
