import { expect, test } from "@playwright/test";

import { createCar, createSetup, openTeam, preloadRoutes, syncStatus, unique } from "./flows";
import { serverRows, state } from "./server";

test("a setup made in airplane mode reaches the server once, signed, with the right author", async ({
  page,
  context,
}) => {
  const devicesBefore = (await serverRows("devices")).length;
  await openTeam(page, context, "owner");
  // The first cycle registered this browser as a device and fetched the offline pass.
  await expect.poll(async () => (await serverRows("devices")).length).toBe(devicesBefore + 1);
  await preloadRoutes(page);

  const carName = unique("Carro");
  const setupName = unique("Pista");

  // Airplane mode from here on.
  await context.setOffline(true);
  await createCar(page, carName);
  await createSetup(page, setupName);

  // The work is queued, not lost, and the UI says so; the server knows nothing yet.
  await expect(syncStatus(page)).toHaveAccessibleName(/Sem ligação · \d+ por enviar/);
  expect((await serverRows("setups")).filter((r) => r.name === setupName)).toEqual([]);
  expect((await serverRows("cars")).filter((r) => r.nickname === carName)).toEqual([]);

  // Network back: it syncs by itself, without a reload.
  await context.setOffline(false);
  await expect(syncStatus(page)).toHaveAccessibleName("Sincronizado");

  expect((await serverRows("cars")).filter((r) => r.nickname === carName)).toHaveLength(1);
  const setups = (await serverRows("setups")).filter((r) => r.name === setupName);
  expect(setups).toHaveLength(1);
  expect(setups[0]).toMatchObject({ team_id: state.teamId });
  const versions = (await serverRows("versions")).filter((r) => r.setup_id === setups[0]?.id);
  expect(versions).toHaveLength(1);
  expect(versions[0]).toMatchObject({ version: 1, created_by: state.owner.id });

  // Every operation of this setup was signed by the device and accepted.
  const operations = (await serverRows("operations")).filter((r) => r.entity_id === setups[0]?.id);
  expect(operations.length).toBeGreaterThanOrEqual(1);
  for (const operation of operations) {
    expect(operation).toMatchObject({ signed: true });
    expect(["applied", "merged"]).toContain(operation.status);
  }

  // The session is an HttpOnly cookie: no script, and no storage, ever sees it.
  const visible = await page.evaluate(() => JSON.stringify([document.cookie, { ...localStorage }, { ...sessionStorage }]));
  expect(visible).not.toContain(state.owner.session);
});
