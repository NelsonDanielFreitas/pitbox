import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@/i18n";
import "@/app/styles.css";

import { installBfcacheRevalidation } from "@/app/bfcache";
import { createAppRouter } from "@/app/router";
import { ConnectTeamsRepository } from "@/features/teams/data/connect-teams-repository";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
    },
  },
});
const teamsRepository = new ConnectTeamsRepository();
const router = createAppRouter({ queryClient, teamsRepository });

installBfcacheRevalidation(queryClient);

const root = document.getElementById("root");
if (!root) {
  throw new Error("root element missing");
}

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
