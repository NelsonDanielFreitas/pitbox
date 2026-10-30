import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import type { Team } from "@/features/teams/domain/team";

import type { GarageRepository } from "../data/garage-repository";
import type { Car, PartInstall, PartWithStock } from "../domain/models";

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
  kind: "glow_plug",
  brand: "OS",
  reference: "P3",
  description: "Vela OS P3",
  unit: "un",
  specs: {},
  minStockCenti: 200,
  reorderToCenti: null,
  hasServiceLife: true,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-26T12:00:00Z",
  version: 1,
  localRev: 0,
  qtyCenti: 300,
  belowMin: false,
};
const car: Car = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d90",
  teamId: team.id,
  make: "Mugen",
  model: "MBX8",
  modelYear: 2024,
  serialNumber: null,
  nickname: "Carro A",
  transponderNumber: null,
  status: "active",
  scale: "1/8",
  powertrain: "nitro",
  notes: null,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-26T12:00:00Z",
  version: 1,
  localRev: 0,
};
const install: PartInstall = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4da0",
  partId: part.id,
  partKind: "glow_plug",
  partDescription: "Vela OS P3",
  partBrand: "OS",
  partReference: "P3",
  carId: car.id,
  engineId: null,
  targetName: "Carro A",
  installedAt: "2026-10-20T12:00:00Z",
  removedAt: null,
  removalDestination: null,
  removalReason: null,
  runSeconds: 0,
  sessions: 0,
  note: null,
};

const change = { movementId: "x", beforeCenti: 300, afterCenti: 200, minStockCenti: 200 };

function repo(over: Partial<GarageRepository> = {}): GarageRepository {
  return {
    ...emptyGarageRepository(),
    getPart: vi.fn().mockResolvedValue(part),
    getCar: vi.fn().mockResolvedValue(car),
    listCars: vi.fn().mockResolvedValue([car]),
    listParts: vi.fn().mockResolvedValue([part]),
    ...over,
  };
}

const partPath = `/teams/${team.id}/garage/parts/${part.id}`;

describe("mounting parts", () => {
  it("mounts the part in a car chosen in the sheet", async () => {
    const installPart = vi.fn().mockResolvedValue(change);
    renderApp({ initialPath: partPath, teams: [team], garageRepository: repo({ installPart }) });
    await userEvent.click(await screen.findByRole("button", { name: "Montar peça" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Montar peça" }));

    // Nothing chosen yet: the repository is not called.
    await userEvent.click(sheet.getByRole("button", { name: "Montar" }));
    expect(await sheet.findByText("Escolhe uma opção.")).toBeVisible();
    expect(installPart).not.toHaveBeenCalled();

    await userEvent.selectOptions(sheet.getByLabelText("Carro ou motor"), `car:${car.id}`);
    await userEvent.click(sheet.getByRole("button", { name: "Montar" }));
    expect(installPart).toHaveBeenCalledWith(
      team.id,
      expect.objectContaining({ partId: part.id, target: { carId: car.id }, createdBy: sessionUser }),
    );
  });

  it("does not offer parts without a service life", async () => {
    renderApp({
      initialPath: partPath,
      teams: [team],
      garageRepository: repo({ getPart: vi.fn().mockResolvedValue({ ...part, hasServiceLife: false }) }),
    });
    expect(await screen.findByRole("heading", { name: "Vela OS P3" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Montar peça" })).not.toBeInTheDocument();
  });

  it("lists the mounted parts in the car page and hides actions from a pilot", async () => {
    const listPartInstalls = vi.fn().mockResolvedValue([install]);
    renderApp({
      initialPath: `/teams/${team.id}/garage/cars/${car.id}`,
      teams: [pilot],
      garageRepository: repo({ listPartInstalls }),
    });
    expect(await screen.findByRole("heading", { name: "Peças montadas" })).toBeVisible();
    expect(await screen.findByText("Vela OS P3")).toBeVisible();
    expect(screen.getByText("Sem sessões registadas")).toBeVisible();
    expect(listPartInstalls).toHaveBeenCalledWith(team.id, { carId: car.id }, false);
    expect(screen.queryByRole("button", { name: "Montar peça" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Desmontar" })).not.toBeInTheDocument();
  });
});

describe("dismounting parts", () => {
  const open = async (removePart: GarageRepository["removePart"]) => {
    renderApp({
      initialPath: `/teams/${team.id}/garage/cars/${car.id}`,
      teams: [team],
      garageRepository: repo({ listPartInstalls: vi.fn().mockResolvedValue([install]), removePart }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Desmontar" }));
    return within(await screen.findByRole("dialog", { name: "Desmontar peça" }));
  };

  it("sends the unit back to the stock by default", async () => {
    const removePart = vi.fn().mockResolvedValue(undefined);
    const sheet = await open(removePart);
    await userEvent.click(sheet.getByRole("button", { name: "Desmontar" }));
    expect(removePart).toHaveBeenCalledWith(
      team.id,
      install.id,
      expect.objectContaining({ destination: "stock", reason: null, createdBy: sessionUser }),
    );
  });

  it("asks for a reason before scrapping", async () => {
    const removePart = vi.fn().mockResolvedValue(undefined);
    const sheet = await open(removePart);
    await userEvent.click(sheet.getByRole("radio", { name: /Abater/ }));
    await userEvent.click(sheet.getByRole("button", { name: "Desmontar" }));
    expect(await sheet.findByText("Indica o motivo do abate.")).toBeVisible();
    expect(removePart).not.toHaveBeenCalled();

    await userEvent.selectOptions(sheet.getByLabelText("Motivo do abate"), "worn");
    await userEvent.click(sheet.getByRole("button", { name: "Desmontar" }));
    expect(removePart).toHaveBeenCalledWith(
      team.id,
      install.id,
      expect.objectContaining({ destination: "scrap", reason: "worn" }),
    );
  });

  it("shows a message when the install was already closed", async () => {
    const { GarageConflictError } = await import("../domain/errors");
    const removePart = vi.fn().mockRejectedValue(new GarageConflictError("install_closed"));
    const sheet = await open(removePart);
    await userEvent.click(sheet.getByRole("button", { name: "Desmontar" }));
    expect(await sheet.findByText("Esta montagem já foi fechada.")).toBeVisible();
  });
});
