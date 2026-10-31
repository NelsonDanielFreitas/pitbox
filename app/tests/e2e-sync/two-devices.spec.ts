import { expect, test } from "@playwright/test";

import { createCar, createSetup, openTeam, preloadRoutes, syncStatus, teamUrl, unique } from "./flows";
import { serverRows } from "./server";

test("a second device of the same person receives what the first one made", async ({ browser }) => {
  const first = await browser.newContext();
  const second = await browser.newContext(); // its own local database and device key
  try {
    const a = await first.newPage();
    const b = await second.newPage();
    await openTeam(a, first, "owner");
    await preloadRoutes(a);
    await openTeam(b, second, "owner");

    const name = unique("Pista");
    await createCar(a, unique("Carro"));
    await createSetup(a, name);
    await expect(syncStatus(a)).toHaveAccessibleName("Sincronizado");

    // The other device pulls it, from its own local database.
    await b.getByRole("link", { name: "Setups", exact: true }).click();
    await expect(b.getByRole("link", { name: new RegExp(name) })).toBeVisible({ timeout: 30_000 });
    expect(new Set((await serverRows("devices")).map((d) => d.id)).size).toBeGreaterThanOrEqual(2);
    await expect(b).toHaveURL(new RegExp(`${teamUrl}/setups`));
  } finally {
    await first.close();
    await second.close();
  }
});
