import { expect, test } from "@playwright/test";

import { navigateInApp, openTeam, preloadRoutes, syncStatus, teamUrl, unique } from "./flows";
import { serverRows } from "./server";

// F2-00: the real Mugen MBX8 sheet from the catalogue, filled offline, validated and stored by the
// real server (multi-choice, table layout and canonical fields included).
test("the MBX8 sheet is applied by itself, filled offline and stored by the server as it was typed", async ({
  page,
  context,
}) => {
  await openTeam(page, context, "owner");
  await preloadRoutes(page);

  const carName = unique("MBX8");
  const setupName = unique("MBX8 seca");

  await context.setOffline(true);
  await navigateInApp(page, `${teamUrl}/garage`);
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByLabel("Alcunha").fill(carName);
  await expect(page.getByText(/Ficha aplicada: Mugen MBX8/)).toBeVisible();
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: carName })).toBeVisible();

  await navigateInApp(page, `${teamUrl}/setups/new`);
  await page.getByLabel("Nome").fill(setupName);
  await page.getByRole("button", { name: "Criar setup" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();
  await expect(page.getByText(/Mugen MBX8 — ficha Ver\. 1801E/)).toBeVisible();

  // A long sheet opens with its first section; the shortcuts bring the others.
  const nav = page.getByRole("navigation", { name: "Secções da ficha" });
  await nav.getByRole("button", { name: "Condições da pista", exact: true }).click();
  await page.getByRole("group", { name: "Superfície" }).getByRole("button", { name: /Bumpy/ }).click();
  await page.getByRole("group", { name: "Superfície" }).getByRole("button", { name: /Smooth/ }).click();

  await nav.getByRole("button", { name: "Suspensão da frente", exact: true }).click();
  await page.getByRole("textbox", { name: /^Altura ao solo \(frente\)/ }).fill("24");
  const parts = page.getByRole("group", { name: /^Peças da frente, cima \(coluna da esquerda\)/ });
  await parts.getByRole("button", { name: /E2146/ }).click();
  await parts.getByRole("button", { name: /E0160/ }).click();

  await nav.getByRole("button", { name: "Diferenciais e relações", exact: true }).click();
  const front = page.getByRole("group", { name: "Frente", exact: true });
  await front.getByRole("button", { name: "42/12T" }).click();
  await front.getByRole("textbox", { name: /^Óleo da frente/ }).fill("6000");
  await page.getByRole("group", { name: "Centro", exact: true }).getByRole("textbox", { name: /^Óleo do centro/ }).fill("7000");

  await page.getByRole("button", { name: "Guardar nova versão" }).click();
  await expect(page.getByRole("heading", { name: setupName })).toBeVisible();
  await expect(syncStatus(page)).toHaveAccessibleName(/Sem ligação · \d+ por enviar/);

  await context.setOffline(false);
  await expect(syncStatus(page)).toHaveAccessibleName("Sincronizado");

  // The server accepted it: the right sheet, the multi values in the order of the options, and
  // the diff oils copied to the canonical (generic) paths by the server, not by the device.
  const setup = (await serverRows("setups")).find((r) => r.name === setupName);
  expect(setup).toBeDefined();
  const versions = (await serverRows("versions")).filter((r) => r.setup_id === setup?.id);
  const latest = versions.at(-1);
  expect(latest).toMatchObject({ schema_version: 2 });
  const data = latest?.data as { template: { version: number }; values: Record<string, unknown>; canonical: Record<string, unknown> };
  expect(data.template.version).toBe(1);
  expect(data.values).toMatchObject({
    track_surface: ["Smooth", "Bumpy"],
    front_ride_height: 24,
    front_parts_upper_a: ["E0160", "E2146"],
    diff_front_gear: "42/12T",
    diff_front_oil: 6000,
    diff_center_oil: 7000,
  });
  expect(JSON.stringify(data.canonical)).toContain("6000");
  const operations = (await serverRows("operations")).filter((r) => r.entity_id === setup?.id);
  for (const operation of operations) expect(["applied", "merged"]).toContain(operation.status);
});
