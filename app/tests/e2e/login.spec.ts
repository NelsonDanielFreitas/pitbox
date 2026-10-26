import { expect, test } from "@playwright/test";

test("login, teams, team home, no tokens, logout", async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && /Content Security Policy|Refused to/.test(message.text())) {
      consoleErrors.push(message.text());
    }
  });
  page.on("pageerror", (error) => consoleErrors.push(error.message));

  await page.goto("/");
  await expect(page).toHaveURL(/\/login(\?.*)?$/);
  await expect(page.getByRole("heading", { name: /Setups, motores/ })).toBeVisible();

  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/teams$/);
  await expect(page.getByRole("heading", { name: "Escolhe a equipa" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /Equipa Beta/ })).toBeVisible();

  await page.getByRole("link", { name: /Equipa Alpha/ }).click();
  await expect(page).toHaveURL(/\/teams\/0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61$/);
  await expect(page.getByRole("link", { name: /tocar para mudar/ })).toBeVisible();
  await expect(page.getByText("Início em preparação")).toBeVisible();
  await page.getByRole("link", { name: /tocar para mudar/ }).click();
  await expect(page).toHaveURL(/\/teams$/);

  const csp = (await page.request.get("/")).headers()["content-security-policy"];
  expect(csp).toContain("script-src 'self'");
  expect(csp).not.toContain("'unsafe-eval'");
  expect(csp).not.toContain("unsafe-inline");

  expect(await page.evaluate(() => document.cookie)).not.toContain("__Host-pitbox_session");

  const storage = await page.evaluate(async () => ({
    local: Object.fromEntries(
      Array.from({ length: localStorage.length }, (_, i) => {
        const key = localStorage.key(i);
        return [key ?? "", key ? localStorage.getItem(key) : null];
      }),
    ),
    session: Object.fromEntries(
      Array.from({ length: sessionStorage.length }, (_, i) => {
        const key = sessionStorage.key(i);
        return [key ?? "", key ? sessionStorage.getItem(key) : null];
      }),
    ),
    indexed: await indexedDB.databases(),
  }));
  const serialized = JSON.stringify(storage);
  expect(serialized).not.toMatch(/access_token|refresh_token|id_token|eyJ/);

  await page.getByRole("button", { name: "Conta" }).click();
  await expect(page.getByRole("menuitem", { name: "Terminar sessão" })).toBeVisible();
  await page.getByRole("menuitem", { name: "Terminar sessão" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Apagar dados e terminar sessão" }).click();
  await expect(page).toHaveURL(/\/login\?logged_out=1$/);
  await expect(page.getByRole("status")).toContainText("Sessão terminada.");
  expect(await page.evaluate(() => document.cookie)).not.toContain("__Host-pitbox_session");

  // Logging in again immediately must reopen the local database without a transient
  // VFS lock error left by the logout tab.
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/teams$/);
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();

  expect(consoleErrors).toEqual([]);
});
