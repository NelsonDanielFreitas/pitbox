import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { renderApp } from "@/test/render";

import type { TeamsRepository } from "../data/teams-repository";
import type { Team } from "../domain/team";

const alpha: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

describe("TeamsPage", () => {
  it("shows the teams and links to their home", async () => {
    renderApp({ teams: [alpha] });
    expect(await screen.findByRole("heading", { name: "Escolhe a equipa" })).toBeVisible();
    const link = screen.getByRole("link", { name: /Equipa Alpha/ });
    expect(link).toHaveAttribute("href", `/teams/${alpha.id}`);
  });

  it("shows the empty state", async () => {
    renderApp();
    expect(await screen.findByText("Ainda não pertences a nenhuma equipa")).toBeVisible();
  });

  it("retries after an error", async () => {
    let succeed = false;
    const repository: TeamsRepository = {
      listMyTeams: vi.fn().mockImplementation(() =>
        succeed ? Promise.resolve([alpha]) : Promise.reject(new Error("offline")),
      ),
      createTeam: vi.fn().mockRejectedValue(new Error("unused")),
    };
    renderApp({ repository });
    expect(
      await screen.findByText("Não foi possível carregar as equipas", {}, { timeout: 3000 }),
    ).toBeVisible();
    succeed = true;
    await userEvent.click(screen.getByRole("button", { name: "Tentar novamente" }));
    await waitFor(() => expect(screen.getByText("Equipa Alpha")).toBeVisible());
  });
});
