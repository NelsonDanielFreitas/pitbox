import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderApp } from "@/test/render";

import type { TeamsRepository } from "../data/teams-repository";
import type { Team } from "../domain/team";

const created: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
  name: "Freitas Racing",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

afterEach(() => vi.restoreAllMocks());

describe("TeamCreatePage", () => {
  it("disables the form without a connection and explains why", async () => {
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    renderApp({ initialPath: "/teams/new", sessionAmr: ["user"] });

    expect(await screen.findByRole("textbox", { name: "Nome da equipa" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Criar equipa" })).toBeDisabled();
    expect(screen.getByText(/precisa de ligação à Internet/)).toBeVisible();
  });

  it("requires a strong factor before exposing the creation form", async () => {
    const repository: TeamsRepository = {
      listMyTeams: vi.fn().mockResolvedValue([]),
      createTeam: vi.fn().mockResolvedValue(created),
    };
    renderApp({ initialPath: "/teams/new", repository, sessionAmr: ["pwd"] });

    expect(await screen.findByRole("heading", { name: "Protege a tua conta" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Ativar passkey ou TOTP" })).toHaveAttribute(
      "href",
      "/auth/account-security",
    );
    expect(screen.getByRole("link", { name: "Já ativei — reautenticar" })).toHaveAttribute(
      "href",
      "/auth/login?step_up=true&return_to=%2Fteams%2Fnew",
    );
    expect(screen.queryByRole("textbox", { name: "Nome da equipa" })).not.toBeInTheDocument();
    expect(repository.createTeam).not.toHaveBeenCalled();
  });

  it("creates with the validated fields and a UUIDv7 request id, then opens the team", async () => {
    const teams = [] as Team[];
    const repository: TeamsRepository = {
      listMyTeams: vi.fn().mockImplementation(() => Promise.resolve(teams)),
      createTeam: vi.fn().mockImplementation((input: { requestId: string }) => {
        teams.push(created);
        expect(input.requestId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
        return Promise.resolve(created);
      }),
    };
    const user = userEvent.setup();
    renderApp({ initialPath: "/teams/new", repository, sessionAmr: ["user"] });

    await user.type(await screen.findByRole("textbox", { name: "Nome da equipa" }), "Freitas Racing");
    await user.selectOptions(screen.getByRole("combobox", { name: "Categoria" }), "");
    await user.type(screen.getByRole("textbox", { name: "Clube (opcional)" }), "Braga RC");
    await user.click(screen.getByRole("button", { name: "Criar equipa" }));

    await waitFor(() => expect(repository.createTeam).toHaveBeenCalledOnce());
    expect(repository.createTeam).toHaveBeenCalledWith(expect.objectContaining({
      name: "Freitas Racing",
      category: "",
      club: "Braga RC",
      countryCode: "PT",
    }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Garagem" })).toBeVisible());
  });

  it("explains a weak-factor callback rather than showing a generic failure", async () => {
    renderApp({ initialPath: "/teams/new?step_up=weak", sessionAmr: ["pwd"] });

    expect(await screen.findByText(/Ainda não tens um fator forte ativo/)).toBeVisible();
    expect(screen.getByRole("link", { name: "Já ativei — reautenticar" })).toBeVisible();
  });
});
