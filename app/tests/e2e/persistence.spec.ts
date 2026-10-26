import { chromium, expect, test } from "@playwright/test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

test("local data survives closing and reopening the browser", async () => {
  const userDataDir = await mkdtemp(join(tmpdir(), "pitbox-e2e-"));
  let context = await chromium.launchPersistentContext(userDataDir, { headless: true });
  try {
    let page = await context.newPage();
    await page.goto("http://localhost:4173/login");
    await page.getByRole("link", { name: "Entrar" }).click();
    await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();
    await context.close();

    context = await chromium.launchPersistentContext(userDataDir, { headless: true });
    page = await context.newPage();
    await page.goto("http://localhost:4173/teams");
    await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();
    await expect(page.getByRole("link", { name: /Equipa Beta/ })).toBeVisible();
  } finally {
    await context.close();
    await rm(userDataDir, { recursive: true, force: true });
  }
});
