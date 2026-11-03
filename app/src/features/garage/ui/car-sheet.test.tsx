import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import type { TemplateRepository } from "@/features/setups/data/template-repository";
import type { TemplateInfo } from "@/features/setups/domain/models";
import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import { emptySetupRepository } from "@/test/setups";
import { emptyTemplateRepository } from "@/test/templates";

const user = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const team: Team = { id: user, name: "Equipa A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };

const info = (id: string, make: string, model: string, name: string): TemplateInfo => ({
  id, teamId: null, slug: id, make, model, scale: "1/8", powertrain: "nitro", name, sourceUrl: null, currentVersion: 1, basedOnId: null, truncated: false, version: 1,
});
const mbx8 = info("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a31", "Mugen", "MBX8", "Mugen MBX8 — ficha Ver. 1801E");
const xray = info("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a32", "Xray", "XB8", "Xray XB8");
const xray2 = info("0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a33", "Xray", "XB8E", "Xray XB8E");

const templates: TemplateRepository = { ...emptyTemplateRepository(), listTemplates: () => Promise.resolve([mbx8, xray, xray2]) };

function open(createCar = vi.fn().mockResolvedValue({ id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a99" })) {
  renderApp({
    initialPath: `/teams/${team.id}/garage/cars/new`,
    teams: [team],
    garageRepository: { ...emptyGarageRepository(), createCar },
    setupRepository: emptySetupRepository(),
    templateRepository: templates,
  });
  return createCar;
}

async function typeCar(make: string, model: string) {
  await userEvent.type(await screen.findByLabelText("Marca"), make);
  await userEvent.type(screen.getByLabelText("Modelo"), model);
}

describe("the sheet of a new car", () => {
  it("is applied by itself when the make and model match one sheet of the catalogue", async () => {
    const createCar = open();
    await typeCar("Mugen", "MBX8");
    expect(await screen.findByText(/Ficha aplicada: Mugen MBX8 — ficha Ver\. 1801E/)).toBeVisible();
    expect(screen.getByLabelText("Ficha do carro")).toHaveValue(mbx8.id);
    await userEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await vi.waitFor(() => expect(createCar).toHaveBeenCalled());
    expect(createCar.mock.calls[0]?.[1]).toMatchObject({ sheetTemplateId: mbx8.id });
  });

  it("can still be changed back to the generic sheet, and then stays there", async () => {
    const createCar = open();
    await typeCar("Mugen", "MBX8");
    await screen.findByText(/Ficha aplicada/);
    await userEvent.selectOptions(screen.getByLabelText("Ficha do carro"), "");
    expect(screen.queryByText(/Ficha aplicada/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await vi.waitFor(() => expect(createCar).toHaveBeenCalled());
    expect(createCar.mock.calls[0]?.[1]).toMatchObject({ sheetTemplateId: null });
  });

  it("says so when the catalogue has no sheet for the model, and uses the generic one", async () => {
    const createCar = open();
    await typeCar("Serpent", "811");
    expect(await screen.findByText("Ainda não temos a ficha de Serpent 811. Vais usar a ficha genérica.")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await vi.waitFor(() => expect(createCar).toHaveBeenCalled());
    expect(createCar.mock.calls[0]?.[1]).toMatchObject({ sheetTemplateId: null });
  });

  it("does not choose by itself when more than one sheet matches, and says nothing is missing", async () => {
    open();
    await typeCar("Xray", "XB8");
    expect(screen.getByLabelText("Ficha do carro")).toHaveValue("");
    expect(screen.queryByText(/Ficha aplicada/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Ainda não temos a ficha/)).not.toBeInTheDocument();
  });

  it("says nothing before there is a make and a model", async () => {
    open();
    await screen.findByLabelText("Marca");
    expect(screen.queryByText(/Ainda não temos a ficha/)).not.toBeInTheDocument();
  });
});
