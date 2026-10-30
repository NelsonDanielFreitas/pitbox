import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

test.use({ viewport: { width: 390, height: 844 } });

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const pilotTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62";
const teamUrl = `/teams/${teamId}`;
// Screenshots are only rewritten on request: PITBOX_CAPTURE=1 pnpm e2e inventory
const capture = process.env.PITBOX_CAPTURE === "1";

async function shot(page: Page, name: string) {
  if (capture) await page.screenshot({ path: `../docs/design/f1-11b/${name}-390x844.png` });
}

/** Client-side navigation, so the chunks already loaded in this page stay loaded. */
async function navigateInApp(page: Page, path: string) {
  await page.evaluate((to) => {
    window.history.pushState({}, "", to);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, path);
}

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/teams$/);
  await page.getByRole("link", { name: /Equipa Alpha/ }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}$`));
}

async function expectNoSeriousA11y(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    results.violations.filter(
      (violation) => violation.impact === "serious" || violation.impact === "critical",
    ),
  ).toEqual([]);
}

test("owner mounts, counts and buys parts while offline", async ({ page, context }) => {
  await signIn(page);
  await page.getByRole("link", { name: "Garagem", exact: true }).click();

  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByLabel("Alcunha").fill("Carro A");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: "Carro A" })).toBeVisible();

  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Peças", exact: true }).click();
  await page.getByRole("link", { name: "Nova peça" }).first().click();
  await page.getByLabel("Descrição").fill("Vela OS P3");
  await page.getByLabel(/Stock mínimo/).fill("2");
  await page.getByLabel(/Repor até/).fill("6");
  await page.getByLabel(/Quantidade inicial/).fill("5");
  await page.getByLabel("Tem vida útil").check();
  await expectNoSeriousA11y(page);
  await shot(page, "part-form");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: "Vela OS P3" })).toBeVisible();

  // Route chunks are loaded on demand and there is no service worker yet, so a route that
  // was never opened in this page load cannot load offline. Visit every route the flow needs
  // while still online, by client-side navigation (a reload would drop the loaded chunks).
  await navigateInApp(page, `${teamUrl}/garage/parts/count`);
  await expect(page.getByText("Não há nenhuma contagem aberta.")).toBeVisible();
  await navigateInApp(page, `${teamUrl}/garage/shopping`);
  await expect(page.getByRole("heading", { name: /Lista de compras/ })).toBeVisible();
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: /Carro A/ }).click();
  await expect(page.getByRole("heading", { name: "Peças montadas" })).toBeVisible();
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Peças", exact: true }).click();
  await page.getByRole("link", { name: /Vela OS P3/ }).click();
  await expect(page.getByRole("heading", { name: "Vela OS P3" })).toBeVisible();

  // From here on nothing needs the network.
  await context.setOffline(true);

  // Mount the part in the car: one unit leaves the stock.
  await page.getByRole("button", { name: "Montar peça" }).click();
  const mount = page.getByRole("dialog", { name: "Montar peça" });
  await mount.getByLabel("Carro ou motor").selectOption({ label: "Carro A" });
  await expectNoSeriousA11y(page);
  await shot(page, "mount-sheet");
  await mount.getByRole("button", { name: "Montar", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Montadas agora" })).toBeVisible();
  await expect(page.getByText("4 un").first()).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "part-detail");

  // It shows in the car page and goes back to the stock when dismounted.
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: /Carro A/ }).click();
  await expect(page.getByRole("heading", { name: "Peças montadas" })).toBeVisible();
  await expect(page.getByRole("listitem").getByText("Vela OS P3")).toBeVisible();
  await page.getByRole("button", { name: "Desmontar" }).click();
  const unmount = page.getByRole("dialog", { name: "Desmontar peça" });
  await expectNoSeriousA11y(page);
  await shot(page, "unmount-sheet");
  await unmount.getByRole("button", { name: "Desmontar", exact: true }).click();
  await expect(page.getByText("Nenhuma peça montada.")).toBeVisible();

  // Count: the stock really holds 1, not the 5 recorded.
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Peças", exact: true }).click();
  await page.getByRole("button", { name: "Contar inventário" }).click();
  await expectNoSeriousA11y(page);
  await shot(page, "count-scope");
  await page.getByRole("button", { name: "Começar contagem" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/parts/count$`));
  await page.getByLabel("Contado: Vela OS P3").fill("1");
  await expect(page.getByText("1 diferença")).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "count");

  await page.getByRole("button", { name: "Rever" }).click();
  await expect(page.getByRole("heading", { name: "Rever diferenças" })).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "count-review");
  await page.getByRole("button", { name: "Aplicar 1 ajuste" }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "Aplicar 1 ajuste" })
    .click();
  await expect(page.getByText("Contagem aplicada · 1 ajuste")).toBeVisible();

  // Stock 1 is below the minimum: the part is on the shopping list, to restock to 6.
  await page.getByRole("link", { name: "Voltar às peças" }).click();
  await page.getByRole("link", { name: /Lista de compras/ }).click();
  await expect(page.getByRole("heading", { name: "Lista de compras · 1" })).toBeVisible();
  await expect(page.getByText(/comprar 5 un/)).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "shopping");

  await page.getByRole("button", { name: "Comprado" }).click();
  const purchase = page.getByRole("dialog", { name: "Movimento de stock" });
  await expect(purchase.getByLabel(/Quantidade/)).toHaveValue("5");
  await purchase.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByText("Nada para comprar.")).toBeVisible();

  await context.setOffline(false);
});

test("pilot cannot count, mount or change the shopping list", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: /tocar para mudar/ }).click();
  await page.getByRole("link", { name: /Equipa Beta/ }).click();
  await expect(page).toHaveURL(new RegExp(`${pilotTeamId}$`));

  await page.goto(`/teams/${pilotTeamId}/garage/parts`);
  await expect(page.getByRole("heading", { name: "Peças", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Contar inventário" })).toHaveCount(0);

  await page.goto(`/teams/${pilotTeamId}/garage/parts/count`);
  await expect(page.getByText("Não tens permissão para alterar a garagem.")).toBeVisible();

  await page.goto(`/teams/${pilotTeamId}/garage/shopping`);
  await expect(page.getByRole("heading", { name: /Lista de compras/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Adicionar à lista" })).toHaveCount(0);
  await expectNoSeriousA11y(page);
});

test("the count draft survives a reload and can be discarded", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Peças", exact: true }).click();
  await page.getByRole("link", { name: "Nova peça" }).first().click();
  await page.getByLabel("Descrição").fill("Rolamento 5x10");
  await page.getByLabel(/Quantidade inicial/).fill("4");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: "Rolamento 5x10" })).toBeVisible();

  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Peças", exact: true }).click();
  await page.getByRole("button", { name: "Contar inventário" }).click();
  await page.getByRole("button", { name: "Começar contagem" }).click();
  await page.getByLabel("Contado: Rolamento 5x10").fill("3");
  await expect(page.getByText("1 de 1 contadas")).toBeVisible();

  // The draft lives in the local database: a reload keeps it.
  await page.reload();
  await expect(page.getByLabel("Contado: Rolamento 5x10")).toHaveValue("3");

  await page.getByRole("button", { name: "Descartar" }).click();
  await expect(page.getByText("Não há nenhuma contagem aberta.")).toBeVisible();
});
