import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRouter,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

import { routeTree } from "@/app/routeTree.gen";
import type { TeamsRepository } from "@/features/teams/data/teams-repository";
import type { Team } from "@/features/teams/domain/team";

export function renderWithQuery(ui: ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

export function renderApp({
  initialPath = "/teams",
  teams = [],
  repository,
}: {
  initialPath?: string;
  teams?: Team[];
  repository?: TeamsRepository;
} = {}) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(
      () =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              authenticated: true,
              user_id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
              amr: [],
              auth_time: "2026-10-25T12:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const teamsRepository: TeamsRepository = repository ?? {
    listMyTeams: vi.fn().mockResolvedValue(teams),
  };
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    context: { queryClient, teamsRepository },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...view, queryClient, router, teamsRepository };
}
