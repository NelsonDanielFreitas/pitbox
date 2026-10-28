import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { rpcNotFound } from "@/test/rpc-errors";
import { renderApp } from "@/test/render";

import { captureInvitationTokenFromLocation, clearPendingInvitation } from "../data/invitation-token";
import type { MembersRepository } from "../data/members-repository";
import type { Team } from "../domain/team";

const token = "B".repeat(43);
const joined: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
  name: "Freitas Racing",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "mechanic",
};

function repository(overrides: Partial<MembersRepository> = {}): MembersRepository {
  return {
    listMembers: vi.fn().mockResolvedValue([]),
    listInvitations: vi.fn().mockResolvedValue([]),
    inviteMember: vi.fn().mockRejectedValue(new Error("unused")),
    acceptInvitation: vi.fn().mockResolvedValue(joined),
    changeMemberRole: vi.fn().mockRejectedValue(new Error("unused")),
    removeMember: vi.fn().mockRejectedValue(new Error("unused")),
    revokeInvitation: vi.fn().mockRejectedValue(new Error("unused")),
    ...overrides,
  };
}

// The app captures the fragment once, before the router, exactly like main.tsx does.
function openInviteLink(value: string = token) {
  window.history.replaceState(null, "", `/invite#t=${value}`);
  captureInvitationTokenFromLocation();
}

afterEach(() => {
  clearPendingInvitation();
  vi.restoreAllMocks();
  window.history.replaceState(null, "", "/");
});

describe("InvitationPage", () => {
  it("accepts a captured token and shows the role the server assigned", async () => {
    openInviteLink();
    const membersRepository = repository();
    const user = userEvent.setup();
    renderApp({ initialPath: "/invite", membersRepository });

    expect(window.location.hash).toBe("");
    expect(await screen.findByRole("heading", { name: "Foste convidado para uma equipa" })).toBeVisible();
    expect(screen.queryByText("Freitas Racing")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Aceitar convite" }));

    expect(await screen.findByRole("heading", { name: "Já fazes parte da equipa" })).toBeVisible();
    expect(screen.getByText("Freitas Racing")).toBeVisible();
    expect(membersRepository.acceptInvitation).toHaveBeenCalledOnce();
    const [sentToken, requestId] = vi.mocked(membersRepository.acceptInvitation).mock.calls[0] ?? [];
    expect(sentToken).toBe(token);
    expect(requestId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
    expect(document.body.textContent).not.toContain(token);
  });

  it("shows one generic message for an expired, revoked or used invitation", async () => {
    openInviteLink();
    const membersRepository = repository({ acceptInvitation: vi.fn().mockRejectedValue(rpcNotFound()) });
    const user = userEvent.setup();
    renderApp({ initialPath: "/invite", membersRepository });

    await user.click(await screen.findByRole("button", { name: "Aceitar convite" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Este convite não é válido ou já não pode ser usado");
    expect(alert).not.toHaveTextContent(/expir|revog|usado por/i);
    expect(document.body.textContent).not.toContain(token);
  });

  it("treats a malformed fragment as an invalid invitation without calling the API", async () => {
    openInviteLink("curto");
    const membersRepository = repository();
    renderApp({ initialPath: "/invite", membersRepository });

    expect(await screen.findByRole("alert")).toHaveTextContent("Este convite não é válido");
    expect(membersRepository.acceptInvitation).not.toHaveBeenCalled();
  });

  it("rejects pasted text that is not an invite link and clears the field", async () => {
    const membersRepository = repository();
    const user = userEvent.setup();
    renderApp({ initialPath: "/invite", membersRepository });

    const field = await screen.findByLabelText("Link do convite");
    await user.type(field, "https://evil.example/invite#t=" + token);
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Este convite não é válido");
    expect(screen.getByLabelText("Link do convite")).toHaveValue("");
    expect(membersRepository.acceptInvitation).not.toHaveBeenCalled();
  });

  it("disables accepting without a connection and explains why", async () => {
    openInviteLink();
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    const membersRepository = repository();
    renderApp({ initialPath: "/invite", membersRepository });

    expect(await screen.findByRole("button", { name: "Aceitar convite" })).toBeDisabled();
    expect(screen.getByText(/precisa de ligação à Internet/)).toBeVisible();
    expect(membersRepository.acceptInvitation).not.toHaveBeenCalled();
  });
});
