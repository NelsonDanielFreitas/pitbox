import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { GarageRepository } from "@/features/garage/data/garage-repository";
import type { Car } from "@/features/garage/domain/models";
import type { Team } from "@/features/teams/domain/team";
import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import { emptySetupRepository } from "@/test/setups";

import type { SetupRepository } from "../data/setup-repository";
import { SetupConflictError } from "../domain/errors";
import type { Setup, SetupVersion } from "../domain/models";

const sessionUser = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const team: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};
const pilot: Team = { ...team, role: "pilot" };

const car: Car = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
  teamId: team.id,
  make: "Mugen",
  model: "MBX8",
  modelYear: null,
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
const setup: Setup = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80",
  carId: car.id,
  name: "Pista seca",
  notes: "Saibro firme",
  currentVersion: 2,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-27T12:00:00Z",
  createdBy: sessionUser,
  localRev: 0,
};
const version = (n: number, data: Record<string, unknown>, summary: string | null = null): SetupVersion => ({
  setupId: setup.id,
  version: n,
  data,
  schemaVersion: 1,
  basedOn: null,
  changeSummary: summary,
  createdAt: `2026-10-2${n}T12:00:00Z`,
  createdBy: sessionUser,
});
const v1 = version(1, { diffs: { front_cst: 500 }, transmission: { spur_teeth: 46, clutch_bell_teeth: 13 } });
const v2 = version(2, { diffs: { front_cst: 600 }, transmission: { spur_teeth: 46, clutch_bell_teeth: 13 } }, "Óleo mais grosso");

function garage(over: Partial<GarageRepository> = {}): GarageRepository {
  return { ...emptyGarageRepository(), listCars: vi.fn().mockResolvedValue([car]), ...over };
}

function repo(over: Partial<SetupRepository> = {}): SetupRepository {
  return {
    ...emptySetupRepository(),
    listSetups: vi.fn().mockResolvedValue([setup]),
    getSetup: vi.fn().mockResolvedValue(setup),
    listVersions: vi.fn().mockResolvedValue([v2, v1]),
    getVersion: vi
      .fn()
      .mockImplementation((_t: string, _s: string, n?: number) => Promise.resolve(n === 1 ? v1 : v2)),
    ...over,
  };
}

const base = `/teams/${team.id}/setups`;

describe("setup list", () => {
  it("lists setups with car and version, and offers a new one to the owner", async () => {
    renderApp({ initialPath: base, teams: [team], setupRepository: repo(), garageRepository: garage() });
    const link = await screen.findByRole("link", { name: /Pista seca/ });
    expect(link).toHaveTextContent("Carro A");
    expect(link).toHaveTextContent("v2");
    expect(screen.getByRole("link", { name: "Novo setup" })).toBeVisible();
  });

  it("shows the Setups tab to every role", async () => {
    renderApp({ initialPath: base, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    const nav = await screen.findByRole("navigation", { name: "Navegação da equipa" });
    expect(within(nav).getByRole("link", { name: "Setups" })).toHaveAttribute("aria-current", "page");
  });

  it("hides creation from a pilot", async () => {
    renderApp({ initialPath: base, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByRole("link", { name: /Pista seca/ })).toBeVisible();
    expect(screen.queryByRole("link", { name: "Novo setup" })).not.toBeInTheDocument();
  });

  it("explains an empty team", async () => {
    renderApp({
      initialPath: base,
      teams: [team],
      setupRepository: repo({ listSetups: vi.fn().mockResolvedValue([]) }),
      garageRepository: garage(),
    });
    expect(await screen.findByRole("heading", { name: "Ainda sem setups" })).toBeVisible();
  });
});

describe("new setup", () => {
  it("creates a blank setup for the first car and goes to the sheet", async () => {
    const createSetup = vi.fn().mockResolvedValue({ setup, version: v1 });
    renderApp({
      initialPath: `${base}/new`,
      teams: [team],
      setupRepository: repo({ createSetup }),
      garageRepository: garage(),
    });
    await userEvent.type(await screen.findByLabelText("Nome"), "  Pista molhada ");
    await userEvent.click(screen.getByRole("button", { name: "Criar setup" }));
    expect(createSetup).toHaveBeenCalledWith(team.id, {
      carId: car.id,
      name: "Pista molhada",
      notes: null,
      source: null,
      createdBy: sessionUser,
    });
    expect(await screen.findByRole("heading", { name: "Editar setup" })).toBeVisible();
  });

  it("copies the last used setup", async () => {
    const createSetup = vi.fn().mockResolvedValue({ setup, version: v1 });
    renderApp({
      initialPath: `${base}/new`,
      teams: [team],
      setupRepository: repo({ createSetup }),
      garageRepository: garage(),
    });
    await userEvent.type(await screen.findByLabelText("Nome"), "Cópia");
    await userEvent.click(screen.getByRole("radio", { name: /Último setup usado: Pista seca/ }));
    await userEvent.click(screen.getByRole("button", { name: "Criar setup" }));
    expect(createSetup).toHaveBeenCalledWith(
      team.id,
      expect.objectContaining({ source: { setupId: setup.id, version: 2 } }),
    );
  });

  it("copies an older version of another setup", async () => {
    const createSetup = vi.fn().mockResolvedValue({ setup, version: v1 });
    renderApp({
      initialPath: `${base}/new`,
      teams: [team],
      setupRepository: repo({ createSetup }),
      garageRepository: garage(),
    });
    await userEvent.type(await screen.findByLabelText("Nome"), "Cópia v1");
    await userEvent.click(screen.getByRole("radio", { name: "Outro setup" }));
    await userEvent.selectOptions(screen.getByLabelText("Setup"), setup.id);
    await userEvent.selectOptions(await screen.findByLabelText("Versão"), "1");
    await userEvent.click(screen.getByRole("button", { name: "Criar setup" }));
    expect(createSetup).toHaveBeenCalledWith(
      team.id,
      expect.objectContaining({ source: { setupId: setup.id, version: 1 } }),
    );
  });

  it("asks for a name", async () => {
    const createSetup = vi.fn();
    renderApp({
      initialPath: `${base}/new`,
      teams: [team],
      setupRepository: repo({ createSetup }),
      garageRepository: garage(),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Criar setup" }));
    expect(await screen.findByText("Campo obrigatório.")).toBeVisible();
    expect(createSetup).not.toHaveBeenCalled();
  });

  it("is closed to a pilot", async () => {
    renderApp({ initialPath: `${base}/new`, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("Sem permissão")).toBeVisible();
  });
});

describe("setup sheet", () => {
  const detail = `${base}/${setup.id}`;

  it("shows what changed since the previous version and the history", async () => {
    renderApp({ initialPath: detail, teams: [team], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByRole("heading", { name: "Pista seca" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "O que mudou desde a v1" })).toBeVisible();
    const changes = within(screen.getByRole("heading", { name: "O que mudou desde a v1" }).closest("section")!);
    expect(changes.getByText("Óleo do diferencial da frente")).toBeVisible();
    expect(changes.getByText("Alterado")).toBeVisible();
    expect(changes.getByText(/500 cSt/)).toBeVisible();
    expect(screen.getAllByText(/600 cSt/).length).toBeGreaterThan(0);
    expect(screen.getByText("3,54")).toBeVisible();
    expect(screen.getByText("Óleo mais grosso", { selector: "p" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Editar" })).toBeVisible();
  });

  it("opens an older version read-only with a way back", async () => {
    renderApp({ initialPath: `${detail}?v=1`, teams: [team], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("A ver a v1 (não é a atual)")).toBeVisible();
    expect(screen.queryByRole("link", { name: "Editar" })).not.toBeInTheDocument();
    expect(screen.getByText("Primeira versão: ainda não há nada para comparar.")).toBeVisible();
    expect(screen.getByRole("link", { name: "Ir para a versão atual" })).toBeVisible();
  });

  it("lets a pilot read but not edit", async () => {
    renderApp({ initialPath: detail, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("Só podes consultar este setup.")).toBeVisible();
    expect(screen.queryByRole("link", { name: "Editar" })).not.toBeInTheDocument();
  });

  it("says when the setup does not exist in the team", async () => {
    renderApp({
      initialPath: detail,
      teams: [team],
      setupRepository: repo({
        getSetup: vi.fn().mockResolvedValue(undefined),
        getVersion: vi.fn().mockResolvedValue(undefined),
        listVersions: vi.fn().mockResolvedValue([]),
      }),
      garageRepository: garage(),
    });
    expect(await screen.findByText("Este setup já não existe nesta equipa.")).toBeVisible();
  });
});

describe("editing a setup", () => {
  const edit = `${base}/${setup.id}/edit`;

  async function openGroup(name: RegExp | string) {
    await userEvent.click(await screen.findByRole("button", { name }));
  }

  it("saves a new version with only what was typed, counting changes per group", async () => {
    const createVersion = vi.fn().mockResolvedValue(v2);
    renderApp({ initialPath: edit, teams: [team], setupRepository: repo({ createVersion }), garageRepository: garage() });
    expect(await screen.findByText(/A editar a partir da v2/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Guardar nova versão" })).toBeDisabled();
    expect(screen.getByText("Altera pelo menos um campo para guardar.")).toBeVisible();

    await openGroup(/^Diferenciais/);
    const input = screen.getByLabelText(/Óleo do diferencial da frente/);
    expect(input).toHaveValue("600");
    await userEvent.clear(input);
    await userEvent.type(input, "700");
    expect(screen.getByText("Antes: 600 cSt")).toBeVisible();
    expect(screen.getByRole("button", { name: /Diferenciais.*1 alteração/ })).toBeVisible();

    await userEvent.type(screen.getByLabelText("O que mudaste (opcional)"), "  Mais grosso ");
    await userEvent.click(screen.getByRole("button", { name: "Guardar nova versão" }));
    expect(createVersion).toHaveBeenCalledWith(team.id, setup.id, {
      baseVersion: 2,
      data: { diffs: { front_cst: 700 }, transmission: { spur_teeth: 46, clutch_bell_teeth: 13 } },
      changeSummary: "Mais grosso",
      createdBy: sessionUser,
    });
    expect(await screen.findByRole("heading", { name: "Pista seca" })).toBeVisible();
  });

  it("accepts a decimal comma and shows the gear ratio while typing", async () => {
    const createVersion = vi.fn().mockResolvedValue(v2);
    renderApp({ initialPath: edit, teams: [team], setupRepository: repo({ createVersion }), garageRepository: garage() });
    await openGroup(/^Transmissão/);
    expect(screen.getByText("Relação de transmissão: 3,54")).toBeVisible();
    const spur = screen.getByLabelText(/Dentes da coroa/);
    await userEvent.clear(spur);
    await userEvent.type(spur, "50");
    expect(screen.getByText("Relação de transmissão: 3,85")).toBeVisible();

    await openGroup(/^Geometria/);
    await userEvent.type(screen.getByLabelText(/Camber da frente/), "-1,5");
    await userEvent.click(screen.getByRole("button", { name: "Guardar nova versão" }));
    expect(createVersion).toHaveBeenCalledWith(
      team.id,
      setup.id,
      expect.objectContaining({
        data: expect.objectContaining({
          geometry: { front_camber_deg: -1.5 },
          transmission: { spur_teeth: 50, clutch_bell_teeth: 13 },
        }) as unknown,
      }),
    );
  });

  it("explains values outside the allowed range and does not save them", async () => {
    const createVersion = vi.fn();
    renderApp({ initialPath: edit, teams: [team], setupRepository: repo({ createVersion }), garageRepository: garage() });
    await openGroup(/^Diferenciais/);
    const input = screen.getByLabelText(/Óleo do diferencial da frente/);
    await userEvent.clear(input);
    await userEvent.type(input, "99");
    expect(await screen.findByText("Entre 100 e 1000000.")).toBeVisible();
    expect(input).toHaveAttribute("aria-invalid", "true");
    await userEvent.click(screen.getByRole("button", { name: "Guardar nova versão" }));
    expect(screen.getByText("Corrige os campos com erro antes de guardar.")).toBeVisible();
    expect(createVersion).not.toHaveBeenCalled();
  });

  it("refuses a bar that is removed and sized", async () => {
    renderApp({ initialPath: edit, teams: [team], setupRepository: repo(), garageRepository: garage() });
    await openGroup(/^Barras estabilizadoras/);
    await userEvent.type(screen.getByLabelText(/Barra estabilizadora da frente/), "3");
    await userEvent.click(screen.getByLabelText("Barra da frente removida"));
    expect(await screen.findByText("Retira o tamanho ou desmarca «removida».")).toBeVisible();
  });

  it("tells the user when the setup changed meanwhile", async () => {
    const createVersion = vi.fn().mockRejectedValue(new SetupConflictError("stale"));
    renderApp({ initialPath: edit, teams: [team], setupRepository: repo({ createVersion }), garageRepository: garage() });
    await openGroup(/^Diferenciais/);
    const input = screen.getByLabelText(/Óleo do diferencial da frente/);
    await userEvent.clear(input);
    await userEvent.type(input, "700");
    await userEvent.click(screen.getByRole("button", { name: "Guardar nova versão" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Este setup foi alterado entretanto");
  });

  it("offers the team's engines and tyre sets", async () => {
    renderApp({
      initialPath: edit,
      teams: [team],
      setupRepository: repo(),
      garageRepository: garage({
        listEngines: vi.fn().mockResolvedValue([
          { id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d90", make: "Novarossi", model: "Plus 21", currentCar: null },
        ]),
      }),
    });
    await openGroup(/^Motor e combustível/);
    const select = screen.getByLabelText("Motor");
    expect(within(select).getByRole("option", { name: "Novarossi Plus 21" })).toBeVisible();
  });

  it("is closed to a pilot", async () => {
    renderApp({ initialPath: edit, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("Sem permissão")).toBeVisible();
  });
});

describe("comparing versions", () => {
  const compare = `${base}/${setup.id}/compare`;

  it("lists the differences between two versions as before and after", async () => {
    renderApp({ initialPath: `${compare}?from=1&to=2`, teams: [team], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("1 diferença entre a v1 e a v2")).toBeVisible();
    expect(screen.getByText("Óleo do diferencial da frente")).toBeVisible();
    expect(screen.getByText("Alterado")).toBeVisible();
    expect(screen.getByText(/500 cSt/)).toBeVisible();
    expect(screen.getByLabelText("De")).toHaveValue("1");
    expect(screen.getByLabelText("Para")).toHaveValue("2");
  });

  it("defaults to the previous version against the latest", async () => {
    renderApp({ initialPath: compare, teams: [team], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("1 diferença entre a v1 e a v2")).toBeVisible();
  });

  it("changes the pair through the selectors", async () => {
    const v3 = version(3, { ...v2.data, aero: { body: "Mugen" } });
    renderApp({
      initialPath: `${compare}?from=1&to=2`,
      teams: [team],
      setupRepository: repo({ listVersions: vi.fn().mockResolvedValue([v3, v2, v1]) }),
      garageRepository: garage(),
    });
    await userEvent.selectOptions(await screen.findByLabelText("Para"), "3");
    expect(await screen.findByText("2 diferenças entre a v1 e a v3")).toBeVisible();
    expect(screen.getByText("Novo")).toBeVisible();
  });

  it("says when two versions are equal or the same", async () => {
    const same = version(3, v2.data);
    renderApp({
      initialPath: `${compare}?from=2&to=3`,
      teams: [team],
      setupRepository: repo({ listVersions: vi.fn().mockResolvedValue([same, v2, v1]) }),
      garageRepository: garage(),
    });
    expect(await screen.findByText("Sem diferenças entre a v2 e a v3.")).toBeVisible();
    await userEvent.selectOptions(screen.getByLabelText("De"), "3");
    expect(await screen.findByText("Escolhe duas versões diferentes.")).toBeVisible();
  });

  it("needs at least two versions", async () => {
    renderApp({
      initialPath: compare,
      teams: [team],
      setupRepository: repo({ listVersions: vi.fn().mockResolvedValue([v1]) }),
      garageRepository: garage(),
    });
    expect(await screen.findByText("É preciso haver pelo menos duas versões para comparar.")).toBeVisible();
  });

  it("is reachable from the sheet and open to a pilot", async () => {
    renderApp({ initialPath: `${base}/${setup.id}`, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    const link = await screen.findByRole("link", { name: "Comparar versões" });
    expect(link).toHaveAttribute("href", expect.stringContaining("from=1"));
  });
});

describe("driver feedback", () => {
  const detail = `${base}/${setup.id}`;
  const entry = {
    id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d99",
    setupId: setup.id,
    version: 2,
    authorId: sessionUser,
    ratings: { traction: 4, steeringEntry: 2 },
    trackZone: "Curva 3",
    note: "Sai de frente",
    createdAt: "2026-10-28T12:00:00Z",
  };

  it("lists the feedback of the version, marking the user's own", async () => {
    renderApp({
      initialPath: detail,
      teams: [team],
      setupRepository: repo({ listFeedback: vi.fn().mockResolvedValue([entry, { ...entry, id: "x", authorId: "other" }]) }),
      garageRepository: garage(),
    });
    expect(await screen.findAllByText(/Curva 3/)).toHaveLength(2);
    expect(screen.getAllByText("Tração: 4 de 5")).toHaveLength(2);
    expect(screen.getByText(/^Tu ·/)).toBeVisible();
    expect(screen.getByText(/^Outro membro ·/)).toBeVisible();
  });

  it("lets the pilot rate with one tap each and add a note", async () => {
    const addFeedback = vi.fn().mockResolvedValue(entry);
    renderApp({
      initialPath: detail,
      teams: [pilot],
      setupRepository: repo({ addFeedback }),
      garageRepository: garage(),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Dar feedback" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Feedback da v2" }));
    await userEvent.click(within(sheet.getByRole("group", { name: "Tração" })).getByRole("radio", { name: "4" }));
    await userEvent.click(within(sheet.getByRole("group", { name: "Travagem" })).getByRole("radio", { name: "2" }));
    await userEvent.type(sheet.getByLabelText("Zona da pista"), " Reta ");
    await userEvent.type(sheet.getByLabelText("Nota"), "Trava tarde");
    await userEvent.click(sheet.getByRole("button", { name: "Guardar feedback" }));
    expect(addFeedback).toHaveBeenCalledWith(
      team.id,
      setup.id,
      2,
      { ratings: { traction: 4, braking: 2 }, trackZone: "Reta", note: "Trava tarde" },
      sessionUser,
    );
    expect(screen.queryByRole("link", { name: "Editar" })).not.toBeInTheDocument();
  });

  it("refuses an empty feedback", async () => {
    const addFeedback = vi.fn();
    renderApp({ initialPath: detail, teams: [pilot], setupRepository: repo({ addFeedback }), garageRepository: garage() });
    await userEvent.click(await screen.findByRole("button", { name: "Dar feedback" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Feedback da v2" }));
    await userEvent.click(sheet.getByRole("button", { name: "Guardar feedback" }));
    expect(await sheet.findByText("Indica pelo menos uma avaliação ou uma nota.")).toBeVisible();
    expect(addFeedback).not.toHaveBeenCalled();
  });

  it("starts empty every time the sheet opens", async () => {
    renderApp({ initialPath: detail, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    await userEvent.click(await screen.findByRole("button", { name: "Dar feedback" }));
    let sheet = within(await screen.findByRole("dialog", { name: "Feedback da v2" }));
    await userEvent.type(sheet.getByLabelText("Nota"), "rascunho");
    await userEvent.click(sheet.getByRole("button", { name: "Cancelar" }));
    await userEvent.click(screen.getByRole("button", { name: "Dar feedback" }));
    sheet = within(await screen.findByRole("dialog", { name: "Feedback da v2" }));
    expect(sheet.getByLabelText("Nota")).toHaveValue("");
  });

  it("explains an empty list", async () => {
    renderApp({ initialPath: detail, teams: [pilot], setupRepository: repo(), garageRepository: garage() });
    expect(await screen.findByText("Ainda sem feedback nesta versão.")).toBeVisible();
  });
});
