import { expect, test, type Page } from "@playwright/test";

import { createCar, createSetup, navigateInApp, openTeam, preloadRoutes, syncStatus, teamUrl, unique } from "./flows";
import { serverRows } from "./server";

// F2-00 (inherited from F1-14): two devices of the same team edit the same setup version while
// offline. The first to reach the server wins; the second one sees the conflict on screen and
// chooses. Nothing is lost silently.

async function editFrontDiffOil(page: Page, setupName: string, oil: string) {
  await navigateInApp(page, `${teamUrl}/setups`);
  await page.getByRole("link", { name: new RegExp(setupName) }).click();
  await page.getByRole("link", { name: "Editar" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();
  await page.getByRole("button", { name: /^Diferenciais/ }).click();
  await page.getByLabel(/Óleo do diferencial da frente/).fill(oil);
  await page.getByRole("button", { name: "Guardar nova versão" }).click();
  await expect(page.getByRole("heading", { name: setupName })).toBeVisible();
}

test("two devices edit the same version offline: the second one sees the conflict and chooses", async ({ browser }) => {
  const first = await browser.newContext();
  const second = await browser.newContext(); // its own local database and device key
  try {
    const a = await first.newPage();
    const b = await second.newPage();
    await openTeam(a, first, "owner");
    await preloadRoutes(a);
    await openTeam(b, second, "owner");
    await preloadRoutes(b);

    const name = unique("Conflito");
    await createCar(a, unique("Carro"));
    await createSetup(a, name);
    await expect(syncStatus(a)).toHaveAccessibleName("Sincronizado");
    await navigateInApp(b, `${teamUrl}/setups`);
    await expect(b.getByRole("link", { name: new RegExp(name) })).toBeVisible({ timeout: 30_000 });

    // Both go offline on the same base (version 1) and save different values.
    await first.setOffline(true);
    await second.setOffline(true);
    await editFrontDiffOil(a, name, "500");
    await editFrontDiffOil(b, name, "700");

    // A reaches the server first and wins.
    await first.setOffline(false);
    await expect(syncStatus(a)).toHaveAccessibleName("Sincronizado");
    const setup = (await serverRows("setups")).find((r) => r.name === name);
    expect(setup).toBeDefined();
    expect((await serverRows("versions")).filter((r) => r.setup_id === setup?.id)).toHaveLength(2);

    // B reaches it second: the server keeps A's version and B is told, on screen.
    await second.setOffline(false);
    await expect(syncStatus(b)).toHaveAccessibleName(/atenção|conflito/i, { timeout: 30_000 });
    await b.getByRole("status").click();
    await expect(b.getByRole("heading", { name: "Conflitos" })).toBeVisible();
    await expect(b.getByText("No servidor")).toBeVisible();
    await expect(b.getByText("O teu valor")).toBeVisible();
    // Still two versions on the server: the conflict did not overwrite anything.
    expect((await serverRows("versions")).filter((r) => r.setup_id === setup?.id)).toHaveLength(2);

    // B keeps the server's value; the conflict goes away and the devices agree.
    await b.getByRole("button", { name: "Manter o do servidor" }).first().click();
    await expect(b.getByText("Nenhum conflito por resolver.")).toBeVisible({ timeout: 30_000 });
    await expect(syncStatus(b)).toHaveAccessibleName("Sincronizado", { timeout: 30_000 });
  } finally {
    await first.close();
    await second.close();
  }
});
