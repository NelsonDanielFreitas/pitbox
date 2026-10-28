import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { renderApp } from "@/test/render";

import type { Team } from "../domain/team";

const alpha: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

describe("team home route", () => {
  it("shows a team from the user's list", async () => {
    renderApp({ initialPath: `/teams/${alpha.id}`, teams: [alpha] });
    expect(
      await screen.findByRole("link", { name: /tocar para mudar/ }),
    ).toBeVisible();
    expect(await screen.findByRole("heading", { name: "Garagem" })).toBeVisible();
    expect(screen.getByText("Ainda não há material registado.")).toBeVisible();
  });

  it("returns not found for a team outside the user's list", async () => {
    const repository = {
      listMyTeams: vi.fn().mockResolvedValue([alpha]),
      createTeam: vi.fn().mockRejectedValue(new Error("unused")),
    };
    renderApp({ initialPath: "/teams/0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d99", repository });
    expect(await screen.findByRole("heading", { name: "Página não encontrada" })).toBeVisible();
    expect(repository.listMyTeams).toHaveBeenCalledTimes(1);
  });

  it("does not query the repository for an invalid id", async () => {
    const repository = {
      listMyTeams: vi.fn().mockResolvedValue([alpha]),
      createTeam: vi.fn().mockRejectedValue(new Error("unused")),
    };
    renderApp({ initialPath: "/teams/not-a-uuid", repository });
    expect(await screen.findByRole("heading", { name: "Página não encontrada" })).toBeVisible();
    expect(repository.listMyTeams).not.toHaveBeenCalled();
  });
});
