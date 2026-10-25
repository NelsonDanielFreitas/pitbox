import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";

import { LoginPage } from "@/features/auth/ui/LoginPage";
import { sessionQueryOptions } from "@/auth/session-query";
import { safeReturnTo } from "@/auth/session";
import { OriginMismatchError } from "@/shared/lib/errors";

const loginSearchSchema = z
  .object({
    returnTo: z.string().optional(),
    logged_out: z.union([z.literal("1"), z.literal(1)]).optional(),
  })
  .catch({});

export const Route = createFileRoute("/login")({
  validateSearch: loginSearchSchema,
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions).catch((error) => {
      if (error instanceof OriginMismatchError) {
        throw error;
      }
      return null;
    });
    if (session?.authenticated) {
      throw redirect({ to: "/teams" });
    }
  },
  component: LoginRoute,
});

function LoginRoute() {
  const search = Route.useSearch();
  return (
    <LoginPage
      returnTo={safeReturnTo(search.returnTo ?? "/teams")}
      loggedOut={search.logged_out !== undefined}
    />
  );
}
