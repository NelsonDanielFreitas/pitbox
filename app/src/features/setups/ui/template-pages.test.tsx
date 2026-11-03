import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Team } from "@/features/teams/domain/team";
import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import { emptySetupRepository } from "@/test/setups";
import { emptyTemplateRepository } from "@/test/templates";

import { GENERIC_TEMPLATE_ID, type TemplateRepository } from "../data/template-repository";
import type { Setup, SetupVersion } from "../domain/models";
import { parseTemplate, type Template } from "../domain/template";

const setupDir = "../../../../../backend/internal/setup/";
const read = (rel: string): unknown =>
  JSON.parse(readFileSync(fileURLToPath(new URL(setupDir + rel, import.meta.url)), "utf8"));

const generic = parseTemplate(read("templates/generic-1-8-nitro.v1.json")) as Template;
const vectors = read("domain/testdata/template/vectors.json") as { templates: { a: unknown } };
const modelA = parseTemplate(vectors.templates.a) as Template;

const user = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const team: Team = { id: user, name: "Equipa A", category: "1/8 TT nitro", countryCode: "PT", role: "owner" };
const setup: Setup = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d80",
  carId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
  name: "Pista seca",
  notes: null,
  currentVersion: 3,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-27T12:00:00Z",
  createdBy: user,
  localRev: 0,
};
const version = (n: number, schemaVersion: number, data: Record<string, unknown>): SetupVersion => ({
  setupId: setup.id,
  version: n,
  data,
  schemaVersion,
  basedOn: null,
  changeSummary: null,
  createdAt: `2026-10-2${n}T12:00:00Z`,
  createdBy: user,
});
const v1 = version(1, 1, { diffs: { front_cst: 500 } });
const v2 = version(2, 2, { template: { id: GENERIC_TEMPLATE_ID, version: 1 }, values: { diffs_front_cst: 600 } });
const v3 = version(3, 2, {
  template: { id: modelA.id, version: 1 },
  values: { front_cst: 700, tower: "3", stiffener: true },
});

const templates: TemplateRepository = {
  ...emptyTemplateRepository(),
  getTemplate: (_team, id) => Promise.resolve(id === GENERIC_TEMPLATE_ID ? generic : id === modelA.id ? modelA : undefined),
};

function repo(over: Partial<ReturnType<typeof emptySetupRepository>> = {}) {
  const all = [v3, v2, v1];
  return {
    ...emptySetupRepository(),
    getSetup: vi.fn().mockResolvedValue(setup),
    listVersions: vi.fn().mockResolvedValue(all),
    getVersion: vi.fn().mockImplementation((_t: string, _s: string, n?: number) => Promise.resolve(all.find((v) => v.version === (n ?? 3)))),
    ...over,
  };
}

const base = `/teams/${team.id}/setups/${setup.id}`;
const garage = { ...emptyGarageRepository() };

describe("a setup made on a template", () => {
  it("shows the values through the sheet of its version", async () => {
    renderApp({ initialPath: base, teams: [team], setupRepository: repo(), garageRepository: garage, templateRepository: templates });
    expect(await screen.findByText("Ficha: Acme A8")).toBeVisible();
    const group = screen.getByRole("heading", { name: "Front" }).closest("section");
    expect(group).not.toBeNull();
    if (group) {
      expect(within(group).getByText("Front diff")).toBeVisible();
      expect(within(group).getByText("700 cSt")).toBeVisible();
      expect(within(group).getByText("Stiffener")).toBeVisible();
    }
  });

  it("opens an old v1 version and compares it with a v2 one on the generic sheet", async () => {
    renderApp({
      initialPath: `${base}/compare?from=1&to=2`,
      teams: [team],
      setupRepository: repo(),
      garageRepository: garage,
      templateRepository: templates,
    });
    expect(await screen.findByText(/1 alteração|1 diferença/i)).toBeVisible();
    expect(screen.getByText("500 cSt")).toBeVisible();
    expect(screen.getByText("600 cSt")).toBeVisible();
  });

  it("compares two different sheets by their common fields and says what was left out", async () => {
    renderApp({
      initialPath: `${base}/compare?from=2&to=3`,
      teams: [team],
      setupRepository: repo(),
      garageRepository: garage,
      templateRepository: templates,
    });
    expect(await screen.findByText(/só se comparam os campos comuns/)).toBeVisible();
    expect(screen.getByText(/Sem correspondência no outro modelo:.*Tower/)).toBeVisible();
  });

  it("edits a v2 setup and saves the next version on the same sheet", async () => {
    const createVersion = vi.fn().mockResolvedValue({ ...v3, version: 4 });
    renderApp({
      initialPath: `${base}/edit`,
      teams: [team],
      setupRepository: repo({ createVersion }),
      garageRepository: garage,
      templateRepository: templates,
    });
    const field = await screen.findByRole("textbox", { name: /^Front diff/ });
    expect(field).toHaveValue("700");
    await userEvent.clear(field);
    await userEvent.type(field, "800");
    await userEvent.click(screen.getByRole("button", { name: "Guardar nova versão" }));
    expect(createVersion).toHaveBeenCalledTimes(1);
    expect(createVersion.mock.calls[0]?.[2]).toMatchObject({
      baseVersion: 3,
      schemaVersion: 2,
      data: { template: { id: modelA.id, version: 1 }, values: { front_cst: 800, tower: "3", stiffener: true } },
    });
  });

  it("says so when the sheet of a version is not on the phone yet", async () => {
    renderApp({
      initialPath: base,
      teams: [team],
      setupRepository: repo(),
      garageRepository: garage,
      templateRepository: { ...emptyTemplateRepository(), getTemplate: (_t, id) => Promise.resolve(id === GENERIC_TEMPLATE_ID ? generic : undefined) },
    });
    expect(await screen.findAllByText(/ainda não está neste telemóvel/)).not.toHaveLength(0);
  });
});
