import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@fontsource-variable/inter";

import "@/i18n";
import "@/app/styles.css";

import { installBfcacheRevalidation } from "@/app/bfcache";
import { createAppRouter } from "@/app/router";
import { LocalSetupRepository } from "@/features/setups/data/local-setup-repository";
import { LocalGarageRepository } from "@/features/garage/data/local-garage-repository";
import { AppTeamsRepository } from "@/features/teams/data/team-commands-repository";
import { ConnectMembersRepository } from "@/features/teams/data/connect-members-repository";
import { captureInvitationTokenFromLocation } from "@/features/teams/data/invitation-token";
import { isUnauthenticated } from "@/shared/api/errors";
import { handleQueryError } from "@/shared/api/query-error";
import { canonicalDevOrigin } from "@/shared/lib/canonical-dev-origin";
import { env } from "@/shared/lib/env";

const devOrigin = canonicalDevOrigin(new URL(window.location.href), env.isDev);
if (devOrigin) {
  window.location.replace(devOrigin);
} else {
  mountApp();
}

function mountApp(): void {
  captureInvitationTokenFromLocation();
  const routerRef: { current?: ReturnType<typeof createAppRouter> } = {};

  const queryClient = new QueryClient({
    queryCache: new QueryCache({
      onError: (error) =>
        handleQueryError(error, {
          queryClient,
          getCurrentLocation: () => routerRef.current?.state.location,
          navigate: (options) => {
            void routerRef.current?.navigate(options);
          },
        }),
    }),
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => !isUnauthenticated(error) && failureCount < 1,
      },
    },
  });
  const teamsRepository = new AppTeamsRepository();
  const membersRepository = new ConnectMembersRepository();
  const garageRepository = new LocalGarageRepository();
  const setupRepository = new LocalSetupRepository();
  routerRef.current = createAppRouter({ queryClient, teamsRepository, membersRepository,
    garageRepository,
    setupRepository,
  });

  installBfcacheRevalidation(queryClient);

  const root = document.getElementById("root");
  if (!root) {
    throw new Error("root element missing");
  }

  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={routerRef.current} />
      </QueryClientProvider>
    </StrictMode>,
  );
}
