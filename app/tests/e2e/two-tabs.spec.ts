import { expect, test } from "@playwright/test";

test("logout clears the local database and signs out the other tab", async ({ page, context }) => {
  await page.goto("http://localhost:4173/login");
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();

  const other = await context.newPage();
  await other.goto("http://localhost:4173/teams");
  await expect(other.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();

  await page.getByRole("button", { name: "Conta" }).click();
  await page.getByRole("menuitem", { name: "Terminar sessão" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Apagar dados e terminar sessão" }).click();

  await expect(page).toHaveURL(/\/login\?logged_out=1$/);
  await expect(other).toHaveURL(/\/login/);

  const directories = await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    try {
      const pitbox = await root.getDirectoryHandle("pitbox");
      const names: string[] = [];
      for await (const [name] of pitbox.entries()) names.push(name);
      return names;
    } catch {
      return [];
    }
  });
  expect(directories).toEqual([]);
});
