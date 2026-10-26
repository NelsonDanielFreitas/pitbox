import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRouter,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

vi.mock("@/app/DatabaseGate", () => ({
  DatabaseGate: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/db/client", () => ({
  openDatabase: vi.fn().mockResolvedValue(undefined),
  closeDatabase: vi.fn().mockResolvedValue(undefined),
  query: vi.fn(),
  transaction: vi.fn(),
}));
vi.mock("@/sync/teams", () => ({
  bootstrapTeams: vi.fn().mockResolvedValue(undefined),
  hasLocalTeams: vi.fn().mockResolvedValue(true),
}));

import { routeTree } from "@/app/routeTree.gen";
import type { GarageRepository } from "@/features/garage/data/garage-repository";
import type { TeamsRepository } from "@/features/teams/data/teams-repository";
import type { Team } from "@/features/teams/domain/team";
import { emptyGarageRepository } from "./garage";

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
  garageRepository,
}: {
  initialPath?: string;
  teams?: Team[];
  repository?: TeamsRepository;
  garageRepository?: GarageRepository;
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
    context: {
      queryClient,
      teamsRepository,
      garageRepository: garageRepository ?? emptyGarageRepository(),
    },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...view, queryClient, router, teamsRepository };
}
