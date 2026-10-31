import { expect, test } from "@playwright/test";

import { createCar, createSetup, navigateInApp, openTeam, preloadRoutes, syncStatus, teamUrl, unique } from "./flows";
import { serverRows } from "./server";

test("a new device receives the setup and its feedback together, on a second version", async ({ browser }) => {
  const first = await browser.newContext();
  const fresh = await browser.newContext();
  try {
    const a = await first.newPage();
    await openTeam(a, first, "owner");
    await preloadRoutes(a);
    const name = unique("Pista");
    await createCar(a, unique("Carro"));
    await createSetup(a, name);
    await a.getByRole("button", { name: /^Diferenciais/ }).click();
    await a.getByRole("region", { name: /^Diferenciais/ }).getByLabel(/Óleo do diferencial da frente/).fill("500");
    await a.getByLabel("O que mudaste (opcional)").fill("v2");
    await a.getByRole("button", { name: /Guardar nova versão/ }).click();
    await expect(syncStatus(a)).toHaveAccessibleName("Sincronizado");
    const setup = (await serverRows("setups")).find((r) => r.name === name);
    await navigateInApp(a, `${teamUrl}/setups/${String(setup?.id)}`);
    await a.getByRole("button", { name: "Dar feedback" }).click();
    const sheet = a.getByRole("dialog", { name: /Feedback da v/ });
    await sheet.getByRole("group", { name: "Tração" }).getByText("5", { exact: true }).click();
    await sheet.getByRole("button", { name: "Guardar feedback" }).click();
    await expect(a.getByText("Tração: 5 de 5")).toBeVisible();
    await expect(syncStatus(a)).toHaveAccessibleName("Sincronizado");
    expect((await serverRows("feedback")).filter((r) => r.setup_id === setup?.id)).toHaveLength(1);

    // A brand-new device: setup, versions and feedback all arrive in its first pulls.
    const b = await fresh.newPage();
    await openTeam(b, fresh, "owner");
    await b.getByRole("link", { name: "Setups", exact: true }).click();
    await b.getByRole("link", { name: new RegExp(name) }).click({ timeout: 30_000 });
    await expect(b.getByText("Tração: 5 de 5")).toBeVisible({ timeout: 30_000 });
  } finally {
    await first.close();
    await fresh.close();
  }
});
