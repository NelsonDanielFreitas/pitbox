import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderApp } from "@/test/render";
import { rpcAborted } from "@/test/rpc-errors";

import type { MembersRepository } from "../data/members-repository";
import type { Team } from "../domain/team";
import type { TeamInvitation, TeamMember } from "../domain/team-schemas";

const uuidV7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const ownerId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const pilotId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62";
const team = (role: Team["role"]): Team => ({
  id: teamId,
  name: "Freitas Racing",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role,
});
const members: TeamMember[] = [
  { userId: ownerId, displayName: "Nelson Freitas", role: "owner", joinedAt: "2026-10-27T12:00:00Z", version: 1 },
  { userId: pilotId, displayName: "Piloto Teste", role: "pilot", joinedAt: "2026-10-28T12:00:00Z", version: 2 },
];
const pending: TeamInvitation[] = [
  { id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d63", role: "mechanic", expiresAt: "2026-11-04T12:00:00Z", createdAt: "2026-10-28T12:00:00Z" },
];

function repository(overrides: Partial<MembersRepository> = {}): MembersRepository {
  return {
    listMembers: vi.fn().mockResolvedValue(members),
    listInvitations: vi.fn().mockResolvedValue(pending),
    inviteMember: vi.fn().mockResolvedValue({ invitation: pending[0]!, token: "A".repeat(43) }),
    acceptInvitation: vi.fn().mockRejectedValue(new Error("unused")),
    changeMemberRole: vi.fn().mockResolvedValue(undefined),
    removeMember: vi.fn().mockResolvedValue(undefined),
    revokeInvitation: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

afterEach(() => vi.restoreAllMocks());

describe("TeamManagementPage", () => {
  it("disables management without a connection, explains why and makes no request", async () => {
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    const membersRepository = repository();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("owner")], membersRepository });

    expect(await screen.findByText(/precisa de ligação à Internet/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Convidar membro" })).toBeDisabled();
    expect(membersRepository.listMembers).not.toHaveBeenCalled();
    expect(membersRepository.listInvitations).not.toHaveBeenCalled();
  });

  it("shows member and invitation lists to the owner, without member emails", async () => {
    const membersRepository = repository();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("owner")], membersRepository });

    expect(await screen.findByText(/Nelson Freitas/)).toBeVisible();
    expect(screen.getByText("Piloto Teste")).toBeVisible();
    expect(screen.getByText("Convites pendentes")).toBeVisible();
    expect(screen.getByRole("button", { name: "Convidar membro" })).toBeVisible();
    expect(membersRepository.listInvitations).toHaveBeenCalledWith(teamId);
    expect(screen.queryByText(/@/)).not.toBeInTheDocument();
  });

  it("hides owner actions from a pilot and never requests pending invitations", async () => {
    const membersRepository = repository();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("pilot")], membersRepository });

    expect(await screen.findByText("Piloto Teste")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Convidar membro" })).not.toBeInTheDocument();
    expect(screen.queryByText("Convites pendentes")).not.toBeInTheDocument();
    expect(membersRepository.listInvitations).not.toHaveBeenCalled();
  });

  it("does not expose team settings to a guest even when navigating directly", async () => {
    const membersRepository = repository();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("guest")], membersRepository });

    expect(await screen.findByRole("heading", { name: "Página não encontrada" })).toBeVisible();
    expect(membersRepository.listMembers).not.toHaveBeenCalled();
    expect(membersRepository.listInvitations).not.toHaveBeenCalled();
  });

  it("creates a link invitation without rendering the bearer token", async () => {
    const token = "A".repeat(43);
    const membersRepository = repository({
      inviteMember: vi.fn().mockResolvedValue({ invitation: pending[0]!, token }),
    });
    const user = userEvent.setup();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("owner")], membersRepository });

    await user.click(await screen.findByRole("button", { name: "Convidar membro" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Função" }), "mechanic");
    await user.click(screen.getByRole("button", { name: "Criar link" }));

    expect(await screen.findByText("Link para Mecânico pronto.")).toBeVisible();
    expect(document.body.textContent).not.toContain(token);
    const inviteCall = vi.mocked(membersRepository.inviteMember).mock.calls[0]?.[0];
    expect(inviteCall).toMatchObject({ teamId, role: "mechanic" });
    expect(inviteCall?.requestId).toMatch(uuidV7);
    await waitFor(() => expect(membersRepository.listInvitations).toHaveBeenCalledTimes(2));
  });

  it("sends the listed version when changing a member role", async () => {
    const membersRepository = repository();
    const user = userEvent.setup();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("owner")], membersRepository });

    await user.click(await screen.findByRole("button", { name: "Mudar função de Piloto Teste" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Função" }), "mechanic");
    await user.click(screen.getByRole("button", { name: "Guardar função" }));

    await waitFor(() => expect(membersRepository.changeMemberRole).toHaveBeenCalledOnce());
    const roleCall = vi.mocked(membersRepository.changeMemberRole).mock.calls[0]?.[0];
    expect(roleCall).toMatchObject({ teamId, userId: pilotId, role: "mechanic", version: 2 });
    expect(roleCall?.requestId).toMatch(uuidV7);
  });
  it("sends a role change once even when the button is tapped twice", async () => {
    const membersRepository = repository();
    const user = userEvent.setup();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("owner")], membersRepository });

    await user.click(await screen.findByRole("button", { name: "Mudar função de Piloto Teste" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Função" }), "mechanic");
    await user.dblClick(screen.getByRole("button", { name: "Guardar função" }));

    await waitFor(() => expect(membersRepository.changeMemberRole).toHaveBeenCalledOnce());
  });

  it("on a version conflict reloads the members, uses the new version on the next attempt", async () => {
    const changed: TeamMember = { ...members[1]!, role: "guest", version: 5 };
    const listMembers = vi.fn().mockResolvedValueOnce(members).mockResolvedValue([members[0]!, changed]);
    const changeMemberRole = vi.fn().mockRejectedValueOnce(rpcAborted()).mockResolvedValue(undefined);
    const membersRepository = repository({ listMembers, changeMemberRole });
    const user = userEvent.setup();
    renderApp({ initialPath: `/teams/${teamId}/more/team`, teams: [team("owner")], membersRepository });

    await user.click(await screen.findByRole("button", { name: "Mudar função de Piloto Teste" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Função" }), "mechanic");
    await user.click(screen.getByRole("button", { name: "Guardar função" }));

    expect((await screen.findAllByText(/foi alterado por outra pessoa/)).length).toBeGreaterThan(0);
    await waitFor(() => expect(listMembers).toHaveBeenCalledTimes(2));

    await user.click(await screen.findByRole("button", { name: "Mudar função de Piloto Teste" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Função" }), "mechanic");
    await user.click(screen.getByRole("button", { name: "Guardar função" }));

    await waitFor(() => expect(changeMemberRole).toHaveBeenCalledTimes(2));
    expect(vi.mocked(changeMemberRole).mock.calls[1]?.[0]).toMatchObject({ version: 5 });
  });
});
