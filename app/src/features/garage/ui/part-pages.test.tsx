import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import type { Team } from "@/features/teams/domain/team";

import type { GarageRepository } from "../data/garage-repository";
import type { PartWithStock, StockMovement } from "../domain/models";

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
  kind: "fuel",
  brand: "Runner Time",
  reference: "RT25",
  description: "Combustível 25%",
  unit: "l",
  specs: { nitro_pct: 25 },
  minStockCenti: 200,
  reorderToCenti: 600,
  hasServiceLife: false,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-26T12:00:00Z",
  version: 1,
  localRev: 0,
  qtyCenti: 500,
  belowMin: false,
  unitCostCents: 1400,
};

const movement = (over: Partial<StockMovement>): StockMovement => ({
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80",
  partId: part.id,
  qtyDeltaCenti: -100,
  reason: "consumed",
  lossReason: null,
  note: null,
  store: null,
  occurredAt: "2026-10-26T12:00:00Z",
  createdAt: "2026-10-26T12:00:00Z",
  createdBy: sessionUser,
  revertsMovementId: null,
  undone: false,
  ...over,
});

function repositoryWith(over: Partial<GarageRepository> = {}): GarageRepository {
  return {
    ...emptyGarageRepository(),
    getPart: vi.fn().mockResolvedValue(part),
    listStockMovements: vi.fn().mockResolvedValue([
      movement({}),
      movement({
        id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d81",
        reason: "purchase",
        qtyDeltaCenti: 400,
        createdBy: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d99",
        undone: true,
      }),
    ]),
    ...over,
  };
}

const detailPath = `/teams/${team.id}/garage/parts/${part.id}`;

describe("part detail", () => {
  it("shows movements with the author, specs and cost for the owner", async () => {
    renderApp({ initialPath: detailPath, teams: [team], garageRepository: repositoryWith() });
    expect(await screen.findByRole("heading", { name: "Combustível 25%" })).toBeVisible();
    expect(screen.getByText("Nitro")).toBeVisible();
    expect(screen.getByText("Valor em stock")).toBeVisible();
    expect(screen.getByText(/· Tu/)).toBeVisible();
    expect(screen.getByText(/Outro membro/)).toBeVisible();
    // Only the live movement can be undone.
    expect(screen.getAllByRole("button", { name: "Anular" })).toHaveLength(1);
  });

  it("asks for costs only for owners and mechanics and hides actions from pilots", async () => {
    const getPart = vi.fn().mockResolvedValue(part);
    renderApp({
      initialPath: detailPath,
      teams: [pilot],
      garageRepository: repositoryWith({ getPart }),
    });
    expect(await screen.findByRole("heading", { name: "Combustível 25%" })).toBeVisible();
    expect(getPart).toHaveBeenCalledWith(team.id, part.id, { costs: false });
    expect(screen.queryByText("Valor em stock")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Registar movimento" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Anular" })).not.toBeInTheDocument();
  });

  it("undoes a movement through the repository", async () => {
    const undoMovement = vi
      .fn()
      .mockResolvedValue({ movementId: "x", beforeCenti: 400, afterCenti: 500, minStockCenti: 200 });
    renderApp({
      initialPath: detailPath,
      teams: [team],
      garageRepository: repositoryWith({ undoMovement }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Anular" }));
    expect(undoMovement).toHaveBeenCalledWith(
      team.id,
      "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80",
      expect.objectContaining({ createdBy: sessionUser, note: expect.stringContaining("Anula") as string }),
    );
  });

  it("records a purchase with price and store, and warns about negative stock", async () => {
    const addStockMovement = vi
      .fn()
      .mockResolvedValue({ movementId: "x", beforeCenti: 500, afterCenti: 900, minStockCenti: 200 });
    renderApp({
      initialPath: detailPath,
      teams: [team],
      garageRepository: repositoryWith({ addStockMovement }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Registar movimento" }));
    const sheet = await screen.findByRole("dialog", { name: "Movimento de stock" });
    const view = within(sheet);

    await userEvent.type(view.getByLabelText(/Quantidade/), "9");
    expect(view.getByText("O stock fica negativo. Confirma a quantidade.")).toBeVisible();

    await userEvent.selectOptions(view.getByLabelText("Motivo"), "purchase");
    await userEvent.clear(view.getByLabelText(/Quantidade/));
    await userEvent.type(view.getByLabelText(/Quantidade/), "4");
    await userEvent.type(view.getByLabelText(/Preço unitário/), "14,5");
    await userEvent.type(view.getByLabelText("Loja"), "RC Mania");
    await userEvent.click(view.getByRole("button", { name: "Guardar" }));

    expect(addStockMovement).toHaveBeenCalledWith(
      team.id,
      expect.objectContaining({
        partId: part.id,
        reason: "purchase",
        qtyDeltaCenti: 400,
        unitCostCents: 1450,
        store: "RC Mania",
        createdBy: sessionUser,
      }),
    );
  });

  it("refuses a loss without a reason", async () => {
    const addStockMovement = vi.fn();
    renderApp({
      initialPath: detailPath,
      teams: [team],
      garageRepository: repositoryWith({ addStockMovement }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Registar movimento" }));
    const view = within(await screen.findByRole("dialog", { name: "Movimento de stock" }));
    await userEvent.selectOptions(view.getByLabelText("Motivo"), "lost");
    await userEvent.type(view.getByLabelText(/Quantidade/), "1");
    await userEvent.click(view.getByRole("button", { name: "Guardar" }));
    expect(await view.findByText("Indica o motivo da perda (e só nas perdas).")).toBeVisible();
    expect(addStockMovement).not.toHaveBeenCalled();
  });
});

describe("part list", () => {
  it("searches without caring about case or accents", async () => {
    const other: PartWithStock = { ...part, id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71", description: "Vela OS P3", kind: "glow_plug", brand: "OS", reference: "P3" };
    renderApp({
      initialPath: `/teams/${team.id}/garage/parts`,
      teams: [team],
      garageRepository: {
        ...emptyGarageRepository(),
        listParts: vi.fn().mockResolvedValue([part, other]),
      },
    });
    await userEvent.type(await screen.findByRole("searchbox", { name: "Pesquisar peças" }), "COMBUSTIVEL");
    expect(screen.getByText("Combustível 25%")).toBeVisible();
    expect(screen.queryByText("Vela OS P3")).not.toBeInTheDocument();
  });
});
