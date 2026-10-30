import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { renderApp } from "@/test/render";
import type { Team } from "@/features/teams/domain/team";

import { emptyGarageRepository } from "@/test/garage";
import type { GarageRepository } from "../data/garage-repository";
import type { PartWithStock } from "../domain/models";

const team: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

const pilot: Team = { ...team, role: "pilot" };

describe("garage pages", () => {
  it("keeps the user's values after a validation error", async () => {
    const repository = emptyGarageRepository();
    renderApp({
      initialPath: `/teams/${team.id}/garage/cars/new`,
      teams: [team],
      garageRepository: repository,
    });
    expect(await screen.findByRole("heading", { name: "Novo carro" })).toBeVisible();
    await userEvent.type(screen.getByLabelText("Marca"), "Mugen");
    await userEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText("Este campo é obrigatório.")).toBeVisible();
    expect(screen.getByLabelText("Marca")).toHaveValue("Mugen");
  });

  it("does not show a write form to a pilot", async () => {
    const repository = emptyGarageRepository();
    renderApp({
      initialPath: `/teams/${team.id}/garage/cars/new`,
      teams: [pilot],
      garageRepository: repository,
    });
    expect(await screen.findByText("Não tens permissão para alterar a garagem.")).toBeVisible();
    expect(screen.queryByLabelText("Marca")).not.toBeInTheDocument();
  });

  it("shows stock below the minimum and filters it", async () => {
    const part: PartWithStock = {
      id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
      teamId: team.id,
      kind: "glow_plug",
      brand: "OS",
      reference: "P3",
      description: "Vela OS P3",
      unit: "un",
      specs: {},
    reorderToCenti: null,
    minStockCenti: 200,
      hasServiceLife: true,
      createdAt: "2026-10-26T12:00:00Z",
      updatedAt: "2026-10-26T12:00:00Z",
      version: 1,
      localRev: 0,
      qtyCenti: 100,
      belowMin: true,
    };
    const repository: GarageRepository = {
      ...emptyGarageRepository(),
      listParts: vi.fn().mockResolvedValue([part]),
    };
    renderApp({
      initialPath: `/teams/${team.id}/garage/parts`,
      teams: [team],
      garageRepository: repository,
    });
    expect(await screen.findByText("Vela OS P3")).toBeVisible();
    expect(screen.getAllByText("Abaixo do mínimo").length).toBeGreaterThan(0);
    expect(screen.getByText("1 un")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: /Abaixo do mínimo/ }));
    expect(screen.getByText("Vela OS P3")).toBeVisible();
  });
});
