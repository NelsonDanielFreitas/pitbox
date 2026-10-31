import { expect, type BrowserContext, type Page } from "@playwright/test";

import { signInAs, state, type Actor } from "./server";

export const teamUrl = `/teams/${state.teamId}`;

/** Client-side navigation, so the chunks already loaded in this page stay loaded. */
export async function navigateInApp(page: Page, path: string) {
  await page.evaluate((to) => {
    window.history.pushState({}, "", to);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, path);
}

export const syncStatus = (page: Page) =>
  page.getByRole("status", { name: /Sincronizado|por enviar|Sem ligação|A sincronizar|Erro|indisponível|atenção/ });

/** A name no other test uses, so the tests can share one server. */
export function unique(label: string): string {
  return `${label} ${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

/** Logs `actor` in and opens the team; the first cycle then registers the device. */
export async function openTeam(page: Page, context: BrowserContext, actor: Actor) {
  await signInAs(context, actor);
  await page.goto("/teams");
  await page.getByRole("link", { name: /Equipa E2E/ }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}$`));
  await expect(syncStatus(page)).toHaveAccessibleName("Sincronizado");
}

/** There is no service worker yet, so route chunks load on demand: visit them while online.
 * Ids that do not exist still load the screen's code. */
export async function preloadRoutes(page: Page) {
  const unknown = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4dff";
  for (const route of [
    "garage",
    "garage/cars/new",
    `garage/cars/${unknown}`,
    "setups",
    "setups/new",
    `setups/${unknown}`,
    `setups/${unknown}/edit`,
  ]) {
    await navigateInApp(page, `${teamUrl}/${route}`);
    await expect(page.getByRole("heading").first()).toBeVisible();
  }
  await navigateInApp(page, `${teamUrl}/garage`);
}

export async function createCar(page: Page, nickname: string) {
  await navigateInApp(page, `${teamUrl}/garage`);
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByLabel("Alcunha").fill(nickname);
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: nickname })).toBeVisible();
}

export async function createSetup(page: Page, name: string) {
  await navigateInApp(page, `${teamUrl}/setups/new`);
  await page.getByLabel("Nome").fill(name);
  await page.getByRole("button", { name: "Criar setup" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();
}
