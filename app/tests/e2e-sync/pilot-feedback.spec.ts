import { expect, test } from "@playwright/test";

import { createCar, createSetup, navigateInApp, openTeam, preloadRoutes, syncStatus, teamUrl, unique } from "./flows";
import { serverRows, state } from "./server";

test("the pilot's feedback, given offline, reaches the server and the mechanic", async ({ browser }) => {
  const mechanic = await browser.newContext();
  const pilot = await browser.newContext();
  try {
    const m = await mechanic.newPage();
    const p = await pilot.newPage();
    await openTeam(m, mechanic, "owner");
    await preloadRoutes(m);
    const name = unique("Pista");
    await createCar(m, unique("Carro"));
    await createSetup(m, name);
    await expect(syncStatus(m)).toHaveAccessibleName("Sincronizado");
    const setup = (await serverRows("setups")).find((r) => r.name === name);
    expect(setup).toBeDefined();
    const setupUrl = `${teamUrl}/setups/${String(setup?.id)}`;

    // The pilot's device pulls the setup, then loses the network in the pits.
    await openTeam(p, pilot, "pilot");
    await p.getByRole("link", { name: "Setups", exact: true }).click();
    await expect(p.getByRole("link", { name: new RegExp(name) })).toBeVisible({ timeout: 30_000 });
    await p.getByRole("link", { name: new RegExp(name) }).click();
    await expect(p.getByRole("button", { name: "Dar feedback" })).toBeVisible();
    await pilot.setOffline(true);

    await p.getByRole("button", { name: "Dar feedback" }).click();
    const sheet = p.getByRole("dialog", { name: /Feedback da v1/ });
    await sheet.getByRole("group", { name: "Tração" }).getByText("4", { exact: true }).click();
    await sheet.getByRole("group", { name: "Travagem" }).getByText("2", { exact: true }).click();
    await sheet.getByLabel("Zona da pista").fill("Curva 3");
    await sheet.getByRole("button", { name: "Guardar feedback" }).click();
    await expect(p.getByText("Tração: 4 de 5")).toBeVisible();
    await expect(syncStatus(p)).toHaveAccessibleName(/Sem ligação · \d+ por enviar/);
    expect((await serverRows("feedback")).filter((r) => r.setup_id === setup?.id)).toEqual([]);

    await pilot.setOffline(false);
    await expect(syncStatus(p)).toHaveAccessibleName("Sincronizado");

    // On the server once, written by the pilot (the author is the session, not the payload).
    const feedback = (await serverRows("feedback")).filter((r) => r.setup_id === setup?.id);
    expect(feedback).toHaveLength(1);
    expect(feedback[0]).toMatchObject({ author_id: state.pilot.id, version: 1 });

    // And the mechanic's device receives it.
    await navigateInApp(m, setupUrl);
    await expect(m.getByText("Tração: 4 de 5")).toBeVisible({ timeout: 30_000 });
  } finally {
    await mechanic.close();
    await pilot.close();
  }
});
