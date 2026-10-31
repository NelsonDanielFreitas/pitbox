import { expect, test } from "@playwright/test";

import { createCar, createSetup, openTeam, preloadRoutes, syncStatus, unique } from "./flows";
import { serverRows } from "./server";

const push = "**/pitbox.sync.v1.SyncService/Push";

test("resending a queue the server already applied does not duplicate anything", async ({ page, context }) => {
  await openTeam(page, context, "owner");
  await preloadRoutes(page);
  const carName = unique("Carro");
  await createCar(page, carName);
  await expect(syncStatus(page)).toHaveAccessibleName("Sincronizado");

  // The server applies the push, but the browser never gets the answer: the worst moment to
  // lose the network. The queue is still full from the browser's point of view.
  let pushes = 0;
  let dropped = false;
  await page.route(push, async (route) => {
    pushes++;
    if (!dropped) {
      dropped = true;
      await route.fetch(); // the server has applied it
      await route.abort("connectionreset");
      return;
    }
    await route.continue();
  });

  const setupName = unique("Pista");
  await createSetup(page, setupName);
  await expect.poll(() => dropped).toBe(true);
  await expect.poll(async () => (await serverRows("setups")).filter((r) => r.name === setupName).length).toBe(1);

  // The engine retries with backoff; "Sincronizar agora" asks for it now.
  await page.getByRole("status").click();
  await page.getByRole("button", { name: "Sincronizar agora" }).click();
  await expect(syncStatus(page)).toHaveAccessibleName("Sincronizado");
  expect(pushes).toBeGreaterThanOrEqual(2);

  const setups = (await serverRows("setups")).filter((r) => r.name === setupName);
  expect(setups).toHaveLength(1);
  const operations = (await serverRows("operations")).filter((r) => r.entity_id === setups[0]?.id);
  // The same op_id came twice; the server answered from its record and applied nothing again.
  expect(new Set(operations.map((o) => o.op_id)).size).toBe(operations.length);
  for (const operation of operations) expect(["applied", "merged"]).toContain(operation.status);
  expect((await serverRows("versions")).filter((r) => r.setup_id === setups[0]?.id)).toHaveLength(1);
});
