import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

async function expectNoSeriousViolations(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  const serious = results.violations.filter(
    (violation) => violation.impact === "serious" || violation.impact === "critical",
  );
  expect(serious).toEqual([]);
}

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/teams$/);
  await expect(page.getByRole("heading", { name: "Escolhe a equipa" })).toBeVisible();
}

test("login, teams and team home have no serious accessibility violations", async ({ page }) => {
  await page.goto("/login");
  await expectNoSeriousViolations(page);

  await signIn(page);
  await page.getByRole("button", { name: "Conta" }).click();
  await expectNoSeriousViolations(page);
  await page.keyboard.press("Escape");

  await page.getByRole("link", { name: /Equipa Alpha/ }).click();
  await expect(page.getByRole("link", { name: /tocar para mudar/ })).toBeVisible();
  await expectNoSeriousViolations(page);
});

test("an expired session returns to login", async ({ page, context }) => {
  await signIn(page);
  await context.clearCookies();
  await page.reload();
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("heading", { name: /Setups, motores/ })).toBeVisible();
});

test("a revoked RPC session returns to login with the current path", async ({ page }) => {
  await signIn(page);
  const response = await page.request.post("http://127.0.0.1:4010/__test/expire-rpc");
  expect(response.status()).toBe(204);

  await page.reload();
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fteams$/);
  await expect(page.getByRole("heading", { name: /Setups, motores/ })).toBeVisible();
});
