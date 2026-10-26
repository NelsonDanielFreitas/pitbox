import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

test.use({ viewport: { width: 390, height: 844 } });

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const teamUrl = `/teams/${teamId}`;

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/teams$/);
  await page.getByRole("link", { name: /Equipa Alpha/ }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}$`));
}

async function openGarage(page: Page) {
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/cars$`));
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

test("owner registers a complete car, mounts the engine and keeps data offline", async ({
  page,
  context,
}) => {
  await signIn(page);
  await openGarage(page);
  await expect(page.getByRole("heading", { name: "Carros", exact: true })).toBeVisible();
  await expectNoSeriousA11y(page);
  const flowStarted = Date.now();

  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await expectNoSeriousA11y(page);
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByLabel("Alcunha").fill("Carro A");
  await page.getByLabel("Transponder").fill("1234567");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/cars/[0-9a-f-]+$`));
  await expect(page.getByRole("heading", { name: "Carro A" })).toBeVisible();
  await expectNoSeriousA11y(page);

  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Motores", exact: true }).click();
  await page.getByRole("link", { name: "Novo motor" }).first().click();
  await expectNoSeriousA11y(page);
  await page.getByLabel("Marca", { exact: true }).fill("Novarossi");
  await page.getByLabel("Modelo", { exact: true }).fill("Plus 21");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/engines/[0-9a-f-]+$`));

  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: /Carro A/ }).click();
  await page.getByRole("button", { name: "Montar motor" }).click();
  await page.getByRole("button", { name: /Novarossi Plus 21/ }).click();
  await expect(page.getByText("Novarossi Plus 21")).toBeVisible();

  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Pneus", exact: true }).click();
  await page.getByRole("link", { name: "Novo conjunto de pneus" }).first().click();
  await expectNoSeriousA11y(page);
  await page.getByLabel("Etiqueta").fill("j4");
  await page.getByLabel("Marca", { exact: true }).fill("Pro-Line");
  await page.getByLabel("Modelo", { exact: true }).fill("Badlands");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/tyres/[0-9a-f-]+$`));
  await expect(page.getByRole("heading", { name: /^J4/ })).toBeVisible();
  test.info().annotations.push({
    type: "garage-complete-car",
    description: `${Date.now() - flowStarted} ms`,
  });
  await expectNoSeriousA11y(page);

  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Peças", exact: true }).click();
  await page.getByRole("link", { name: "Nova peça" }).first().click();
  await expectNoSeriousA11y(page);
  await page.getByLabel("Descrição").fill("Vela OS P3");
  await page.getByLabel(/Stock mínimo/).fill("2");
  await page.getByLabel(/Quantidade inicial/).fill("1");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/parts/[0-9a-f-]+$`));
  await expect(page.getByText("Abaixo do mínimo")).toBeVisible();

  // A second car created while offline must stay in the local database.
  await context.setOffline(true);
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Carros", exact: true }).click();
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Kyosho");
  await page.getByLabel("Modelo", { exact: true }).fill("Inferno");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/cars/[0-9a-f-]+$`));
  await expect(page.getByRole("heading", { name: "Kyosho Inferno" })).toBeVisible();
  await context.setOffline(false);

  await page.reload();
  await expect(page.getByRole("heading", { name: "Kyosho Inferno" })).toBeVisible();
});

test("pilot reads the garage but cannot write", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: /tocar para mudar/ }).click();
  await page.getByRole("link", { name: /Equipa Beta/ }).click();
  await expect(page).toHaveURL(/\/teams\/0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62$/);
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Carros", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Novo carro" })).toHaveCount(0);

  await page.goto("/teams/0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62/garage/cars/new");
  await expect(page.getByText("Não tens permissão para alterar a garagem.")).toBeVisible();
});

test("an edit from another tab shows the stale conflict", async ({ page, context }) => {
  await signIn(page);
  await openGarage(page);
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByLabel("Alcunha").fill("Concorrência");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/cars/[0-9a-f-]+$`));
  await page.getByRole("link", { name: "Editar" }).click();
  const editUrl = page.url();

  const second = await context.newPage();
  await second.goto(editUrl);
  await expect(second.getByRole("heading", { name: "Editar carro" })).toBeVisible();

  await page.getByLabel("Alcunha").fill("A");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page).toHaveURL(new RegExp(`${teamUrl}/garage/cars/[0-9a-f-]+$`));

  await second.getByLabel("Alcunha").fill("B");
  await second.getByRole("button", { name: "Guardar" }).click();
  await expect(
    second.getByText("Este registo foi alterado noutra aba. Recarrega e tenta outra vez."),
  ).toBeVisible();
});
