import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Entrar com Zitadel" }).click();
  await expect(page).toHaveURL(/\/teams$/);
  await expect(page.getByRole("heading", { name: "As minhas equipas" })).toBeVisible();
}

test("the teams screen has no serious accessibility violations", async ({ page }) => {
  await signIn(page);
  await page.getByRole("button", { name: "Conta" }).click();
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  const serious = results.violations.filter(
    (violation) => violation.impact === "serious" || violation.impact === "critical",
  );
  expect(serious).toEqual([]);
});

test("an expired session returns to login", async ({ page, context }) => {
  await signIn(page);
  await context.clearCookies();
  await page.reload();
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("heading", { name: "Entrar no PitBox" })).toBeVisible();
});
