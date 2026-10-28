import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Browser, type Page } from "@playwright/test";

test.use({ viewport: { width: 390, height: 844 } });

const bff = "http://127.0.0.1:4010";

// The mock BFF is shared by every spec, so leave it in its seed state.
test.beforeEach(async ({ request }) => {
  expect((await request.post(`${bff}/__test/reset`)).ok()).toBe(true);
});
test.afterEach(async ({ request }) => {
  await request.post(`${bff}/__test/reset`);
});

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/teams$/);
}

async function expectNoSeriousA11y(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(
    results.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical"),
  ).toEqual([]);
}

async function secondUser(browser: Browser) {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const page = await context.newPage();
  return { context, page };
}

test("the app never sends a referrer to other origins", async ({ request }) => {
  const response = await request.get("/invite");
  expect(response.headers()["referrer-policy"]).toBe("same-origin");
});

test("owner creates a team, invites by link and a second account joins", async ({ page, request, browser, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await signIn(page);

  await page.getByRole("link", { name: "Criar equipa" }).click();
  await expect(page).toHaveURL(/\/teams\/new$/);
  await expectNoSeriousA11y(page);
  await page.getByRole("textbox", { name: "Nome da equipa" }).fill("Equipa Gama");
  await page.getByRole("button", { name: "Criar equipa" }).click();
  await expect(page).toHaveURL(/\/teams\/[0-9a-f-]{36}$/);
  const teamUrl = new URL(page.url()).pathname;
  await expect(page.getByRole("heading", { name: "Garagem" })).toBeVisible();

  await page.getByRole("link", { name: "Mais", exact: true }).click();
  await page.getByRole("link", { name: /Equipa/ }).filter({ hasText: "Membros e permissões" }).click();
  await expect(page.getByText(/Nelson Freitas/)).toBeVisible();
  await expectNoSeriousA11y(page);

  await page.getByRole("button", { name: "Convidar membro" }).click();
  await expect(page.getByRole("dialog", { name: "Convidar para a equipa" })).toBeVisible();
  await expectNoSeriousA11y(page);
  await page.getByRole("button", { name: "Criar link" }).click();
  await page.getByRole("button", { name: "Copiar" }).click();
  const link = await page.evaluate(() => navigator.clipboard.readText());
  const token = new URL(link).hash.replace("#t=", "");
  expect(token).toMatch(/^[A-Za-z0-9_-]{43}$/);
  await expect(page.locator("body")).not.toContainText(token);

  expect((await request.post(`${bff}/__test/next-user?session=mock-session-2`)).ok()).toBe(true);
  const second = await secondUser(browser);
  try {
    await signIn(second.page);
    await second.page.goto(link);
    await expect(second.page).toHaveURL(/\/invite$/);
    expect(second.page.url()).not.toContain(token);
    await expect(second.page.getByRole("heading", { name: "Foste convidado para uma equipa" })).toBeVisible();
    await expectNoSeriousA11y(second.page);
    await second.page.getByRole("button", { name: "Aceitar convite" }).click();
    await expect(second.page.getByRole("heading", { name: "Já fazes parte da equipa" })).toBeVisible();
    // Role comes from the server response (the default invite role is pilot).
    await expect(second.page.getByText("A tua função nesta equipa é Piloto.")).toBeVisible();
    await second.page.getByRole("button", { name: "Abrir a equipa" }).click();
    await expect(second.page).toHaveURL(new RegExp(`${teamUrl}$`));

    // The link is single use: the same capability is refused with the generic message.
    await second.page.goto(link);
    await second.page.getByRole("button", { name: "Aceitar convite" }).click();
    await expect(second.page.getByRole("alert")).toContainText("Este convite não é válido");
  } finally {
    await second.context.close();
  }

  await page.reload();
  await expect(page.getByText("Piloto Teste")).toBeVisible();
});
