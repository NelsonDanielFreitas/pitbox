import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import type { Team } from "@/features/teams/domain/team";

import type { GarageRepository } from "../data/garage-repository";
import type { PartWithStock, ShoppingLine } from "../domain/models";

const sessionUser = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const team: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};
const pilot: Team = { ...team, role: "pilot" };

const part: PartWithStock = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
  teamId: team.id,
  kind: "bearing",
  brand: "Mugen",
  reference: "B1",
  description: "Rolamento 5x10",
  unit: "un",
  specs: {},
  minStockCenti: 300,
  reorderToCenti: null,
  hasServiceLife: false,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-26T12:00:00Z",
  version: 1,
  localRev: 0,
  qtyCenti: 100,
  belowMin: true,
  unitCostCents: 250,
};
const line: ShoppingLine = {
  partId: part.id,
  kind: "bearing",
  description: part.description,
  brand: "Mugen",
  reference: "B1",
  unit: "un",
  stockCenti: 100,
  qtyCenti: 200,
  suggestedQtyCenti: 200,
  belowMin: true,
  note: null,
  unitCostCents: 250,
};

const path = `/teams/${team.id}/garage/shopping`;

function repo(over: Partial<GarageRepository> = {}): GarageRepository {
  return {
    ...emptyGarageRepository(),
    listShoppingList: vi.fn().mockResolvedValue([line]),
    listParts: vi.fn().mockResolvedValue([part]),
    ...over,
  };
}

describe("shopping list", () => {
  it("shows lines by brand with the estimated total for the owner", async () => {
    renderApp({ initialPath: path, teams: [team], garageRepository: repo() });
    expect(await screen.findByRole("heading", { name: "Lista de compras · 1" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "Mugen" })).toBeVisible();
    expect(screen.getByText("Rolamento 5x10")).toBeVisible();
    expect(screen.getByText("Automático")).toBeVisible();
    expect(screen.getByText("Total estimado")).toBeVisible();
    // The line cost and the total are both 2 x 2,50 €.
    expect(screen.getAllByText(/5,00\s€/)).toHaveLength(2);
  });

  it("hides costs and writes from a pilot, also in the shared text", async () => {
    const listShoppingList = vi.fn().mockResolvedValue([{ ...line, unitCostCents: undefined }]);
    renderApp({
      initialPath: path,
      teams: [pilot],
      garageRepository: repo({ listShoppingList }),
    });
    expect(await screen.findByText("Rolamento 5x10")).toBeVisible();
    expect(listShoppingList).toHaveBeenCalledWith(team.id, { costs: false });
    expect(screen.queryByText("Total estimado")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Comprado" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adicionar à lista" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Partilhar" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Partilhar lista" }));
    expect(sheet.getByText(/- Rolamento 5x10 \(B1\) · 2 un/)).toBeVisible();
    expect(sheet.queryByText(/€/)).not.toBeInTheDocument();
  });

  it("does not show a 0,00 € total when no part has a price", async () => {
    renderApp({
      initialPath: path,
      teams: [team],
      garageRepository: repo({
        listShoppingList: vi.fn().mockResolvedValue([{ ...line, unitCostCents: null }]),
      }),
    });
    expect(await screen.findByText("Rolamento 5x10")).toBeVisible();
    expect(screen.queryByText("Total estimado")).not.toBeInTheDocument();
  });

  it("removes a line through the repository", async () => {
    const removeShoppingItem = vi.fn().mockResolvedValue(undefined);
    renderApp({ initialPath: path, teams: [team], garageRepository: repo({ removeShoppingItem }) });
    await userEvent.click(await screen.findByRole("button", { name: "Remover" }));
    expect(removeShoppingItem).toHaveBeenCalledWith(team.id, part.id);
  });

  it("opens a purchase with the quantity of the line", async () => {
    const addStockMovement = vi
      .fn()
      .mockResolvedValue({ movementId: "x", beforeCenti: 100, afterCenti: 300, minStockCenti: 300 });
    renderApp({ initialPath: path, teams: [team], garageRepository: repo({ addStockMovement }) });
    await userEvent.click(await screen.findByRole("button", { name: "Comprado" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Movimento de stock" }));
    expect(sheet.getByLabelText("Motivo")).toHaveValue("purchase");
    expect(sheet.getByLabelText(/Quantidade/)).toHaveValue("2");
    await userEvent.click(sheet.getByRole("button", { name: "Guardar" }));
    expect(addStockMovement).toHaveBeenCalledWith(
      team.id,
      expect.objectContaining({ partId: part.id, reason: "purchase", qtyDeltaCenti: 200, createdBy: sessionUser }),
    );
  });

  it("changes the quantity of a line", async () => {
    const setShoppingItem = vi.fn().mockResolvedValue(undefined);
    renderApp({ initialPath: path, teams: [team], garageRepository: repo({ setShoppingItem }) });
    await userEvent.click(await screen.findByRole("button", { name: "Quantidade" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Quantidade a comprar" }));
    await userEvent.clear(sheet.getByLabelText(/Quantidade a comprar/));
    await userEvent.type(sheet.getByLabelText(/Quantidade a comprar/), "1,5");
    await userEvent.click(sheet.getByRole("button", { name: "Guardar" }));
    expect(await sheet.findByText("Para a unidade “un”, usa um número inteiro.")).toBeVisible();
    expect(setShoppingItem).not.toHaveBeenCalled();
    await userEvent.clear(sheet.getByLabelText(/Quantidade a comprar/));
    await userEvent.type(sheet.getByLabelText(/Quantidade a comprar/), "4");
    await userEvent.click(sheet.getByRole("button", { name: "Guardar" }));
    expect(setShoppingItem).toHaveBeenCalledWith(team.id, part.id, 400);
  });

  it("adds a manual line from the search and offers to create a missing part", async () => {
    const setShoppingItem = vi.fn().mockResolvedValue(undefined);
    renderApp({ initialPath: path, teams: [team], garageRepository: repo({ setShoppingItem }) });
    await userEvent.click(await screen.findByRole("button", { name: "Adicionar à lista" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Adicionar à lista" }));

    await userEvent.type(sheet.getByRole("searchbox"), "xyz");
    expect(sheet.getByRole("link", { name: "Criar peça «xyz»" })).toBeVisible();

    await userEvent.clear(sheet.getByRole("searchbox"));
    await userEvent.type(sheet.getByRole("searchbox"), "rolamento");
    await userEvent.click(sheet.getByRole("radio", { name: /Rolamento 5x10/ }));
    expect(sheet.getByLabelText(/Quantidade a comprar/)).toHaveValue("2");
    await userEvent.click(sheet.getByRole("button", { name: "Guardar" }));
    expect(setShoppingItem).toHaveBeenCalledWith(team.id, part.id, 200);
  });

  it("shows an empty state when there is nothing to buy", async () => {
    renderApp({
      initialPath: path,
      teams: [team],
      garageRepository: repo({ listShoppingList: vi.fn().mockResolvedValue([]) }),
    });
    expect(await screen.findByText("Nada para comprar.")).toBeVisible();
  });
});
