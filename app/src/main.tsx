import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@fontsource-variable/inter";

import "@/i18n";
import "@/app/styles.css";

import { installBfcacheRevalidation } from "@/app/bfcache";
import { createAppRouter } from "@/app/router";
import { ConnectTeamsRepository } from "@/features/teams/data/connect-teams-repository";
import { isUnauthenticated } from "@/shared/api/errors";
import { handleQueryError } from "@/shared/api/query-error";

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
const teamsRepository = new ConnectTeamsRepository();
routerRef.current = createAppRouter({ queryClient, teamsRepository });

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
