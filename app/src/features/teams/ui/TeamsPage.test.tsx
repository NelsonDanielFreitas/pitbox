import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { renderWithQuery } from "@/test/render";

import type { Team } from "../domain/team";
import type { TeamsRepository } from "../data/teams-repository";
import { TeamsPage } from "./TeamsPage";

const alpha: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

function repository(listMyTeams: TeamsRepository["listMyTeams"]): TeamsRepository {
  return { listMyTeams };
}

describe("TeamsPage", () => {
  it("shows the teams and their role", async () => {
    renderWithQuery(<TeamsPage repository={repository(vi.fn().mockResolvedValue([alpha]))} />);
    expect(await screen.findByRole("heading", { name: "As minhas equipas" })).toBeVisible();
    expect(screen.getByText("Equipa Alpha")).toBeVisible();
    expect(screen.getByText("Dono")).toBeVisible();
  });

  it("shows the empty state", async () => {
    renderWithQuery(<TeamsPage repository={repository(vi.fn().mockResolvedValue([]))} />);
    expect(await screen.findByText("Ainda não pertences a nenhuma equipa")).toBeVisible();
  });

  it("retries after an error", async () => {
    let succeed = false;
    const list = vi.fn().mockImplementation(() =>
      succeed ? Promise.resolve([alpha]) : Promise.reject(new Error("offline")),
    );
    renderWithQuery(<TeamsPage repository={repository(list)} />);
    expect(
      await screen.findByText("Não foi possível carregar as equipas", {}, { timeout: 3000 }),
    ).toBeVisible();
    succeed = true;
    await userEvent.click(screen.getByRole("button", { name: "Tentar novamente" }));
    await waitFor(() => expect(screen.getByText("Equipa Alpha")).toBeVisible());
  });
});
