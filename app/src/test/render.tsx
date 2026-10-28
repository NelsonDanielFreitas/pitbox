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
import type { MembersRepository } from "@/features/teams/data/members-repository";
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
  sessionAmr = [],
  repository,
  membersRepository: membersRepositoryOverride,
  garageRepository,
}: {
  initialPath?: string;
  teams?: Team[];
  sessionAmr?: string[];
  repository?: TeamsRepository;
  membersRepository?: MembersRepository;
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
              amr: sessionAmr,
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
    createTeam: vi.fn().mockRejectedValue(new Error("createTeam is not configured in this test")),
  };
  const membersRepository: MembersRepository = membersRepositoryOverride ?? {
    listMembers: vi.fn().mockResolvedValue([]),
    listInvitations: vi.fn().mockResolvedValue([]),
    inviteMember: vi.fn().mockRejectedValue(new Error("inviteMember is not configured in this test")),
    acceptInvitation: vi.fn().mockRejectedValue(new Error("acceptInvitation is not configured in this test")),
    changeMemberRole: vi.fn().mockRejectedValue(new Error("changeMemberRole is not configured in this test")),
    removeMember: vi.fn().mockRejectedValue(new Error("removeMember is not configured in this test")),
    revokeInvitation: vi.fn().mockRejectedValue(new Error("revokeInvitation is not configured in this test")),
  };
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    context: {
      queryClient,
      teamsRepository,
      membersRepository,
      garageRepository: garageRepository ?? emptyGarageRepository(),
    },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...view, queryClient, router, teamsRepository, membersRepository };
}
