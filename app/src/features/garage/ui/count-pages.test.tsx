import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { emptyGarageRepository } from "@/test/garage";
import { renderApp } from "@/test/render";
import type { Team } from "@/features/teams/domain/team";

import type { GarageRepository } from "../data/garage-repository";
import type { CountDraft, PartWithStock } from "../domain/models";

const sessionUser = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";
const team: Team = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  countryCode: "PT",
  role: "owner",
};

const base: PartWithStock = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d70",
  teamId: team.id,
  kind: "bearing",
  brand: "Mugen",
  reference: "B1",
  description: "Rolamento 5x10",
  unit: "un",
  specs: {},
  minStockCenti: 200,
  reorderToCenti: null,
  hasServiceLife: false,
  createdAt: "2026-10-26T12:00:00Z",
  updatedAt: "2026-10-26T12:00:00Z",
  version: 1,
  localRev: 0,
  qtyCenti: 500,
  belowMin: false,
};
const other: PartWithStock = {
  ...base,
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d71",
  description: "Rolamento 8x16",
  qtyCenti: 300,
};

const draft = (over: Partial<CountDraft> = {}): CountDraft => ({
  scopeKind: "bearing",
  startedAt: "2026-10-26T12:00:00.000Z",
  movementsSince: 0,
  lines: {},
  ...over,
});

const countPath = `/teams/${team.id}/garage/parts/count`;

function repo(over: Partial<GarageRepository> = {}): GarageRepository {
  return {
    ...emptyGarageRepository(),
    listParts: vi.fn().mockResolvedValue([base, other]),
    getCountDraft: vi.fn().mockResolvedValue(draft()),
    setCountLine: vi.fn().mockResolvedValue(undefined),
    ...over,
  };
}

describe("inventory count", () => {
  it("counts, reviews the differences and applies them", async () => {
    const applyCount = vi.fn().mockResolvedValue(1);
    const setCountLine = vi.fn().mockResolvedValue(undefined);
    renderApp({
      initialPath: countPath,
      teams: [team],
      garageRepository: repo({ applyCount, setCountLine }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Menos Rolamento 5x10" }));
    // Nothing was counted before, so − starts from 0 and stays there.
    expect(setCountLine).toHaveBeenLastCalledWith(team.id, base.id, 0);
    await userEvent.clear(screen.getByLabelText("Contado: Rolamento 5x10"));
    await userEvent.type(screen.getByLabelText("Contado: Rolamento 5x10"), "4");
    await userEvent.type(screen.getByLabelText("Contado: Rolamento 8x16"), "3");
    expect(setCountLine).toHaveBeenLastCalledWith(team.id, other.id, 300);
    expect(screen.getByText("Certo")).toBeVisible();
    expect(screen.getByText("1 diferença")).toBeVisible();

    await userEvent.click(screen.getByRole("button", { name: "Rever" }));
    expect(await screen.findByRole("heading", { name: "Rever diferenças" })).toBeVisible();
    // Only the part that changed is listed.
    const list = screen.getByRole("list");
    expect(within(list).getByText("Rolamento 5x10")).toBeVisible();
    expect(within(list).queryByText("Rolamento 8x16")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Aplicar 1 ajuste" }));
    const dialog = within(await screen.findByRole("alertdialog"));
    await userEvent.click(dialog.getByRole("button", { name: "Aplicar 1 ajuste" }));
    expect(applyCount).toHaveBeenCalledWith(
      team.id,
      expect.objectContaining({ createdBy: sessionUser, note: expect.stringContaining("Ajuste · Contagem de") as string }),
    );
    expect(await screen.findByText("Contagem aplicada · 1 ajuste")).toBeVisible();
  });

  it("warns about movements made during the count", async () => {
    renderApp({
      initialPath: countPath,
      teams: [team],
      garageRepository: repo({
        getCountDraft: vi.fn().mockResolvedValue(draft({ lines: { [base.id]: 500 }, movementsSince: 2 })),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Rever" }));
    expect(
      await screen.findByText("Houve 2 movimentos durante a contagem; o ajuste usa o stock atual."),
    ).toBeVisible();
    expect(screen.getByText("Sem diferenças. O stock contado bate certo com o registado.")).toBeVisible();
  });

  it("limits the count to the chosen kind", async () => {
    const wing: PartWithStock = { ...base, id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d72", kind: "wing", description: "Asa" };
    renderApp({
      initialPath: countPath,
      teams: [team],
      garageRepository: repo({ listParts: vi.fn().mockResolvedValue([base, wing]) }),
    });
    expect(await screen.findByText("Rolamento 5x10")).toBeVisible();
    expect(screen.queryByText("Asa")).not.toBeInTheDocument();
  });

  it("is not available to a pilot", async () => {
    renderApp({ initialPath: countPath, teams: [{ ...team, role: "pilot" }], garageRepository: repo() });
    expect(await screen.findByText("Não tens permissão para alterar a garagem.")).toBeVisible();
  });

  it("starts a count from the parts list", async () => {
    const startCount = vi.fn().mockResolvedValue(draft());
    renderApp({
      initialPath: `/teams/${team.id}/garage/parts`,
      teams: [team],
      garageRepository: repo({ getCountDraft: vi.fn().mockResolvedValue(null), startCount }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Contar inventário" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Contar inventário" }));
    await userEvent.selectOptions(sheet.getByLabelText("O que contar"), "bearing");
    await userEvent.click(sheet.getByRole("button", { name: "Começar contagem" }));
    expect(startCount).toHaveBeenCalledWith(team.id, "bearing");
  });
});
