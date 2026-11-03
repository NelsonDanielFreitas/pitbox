import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

// F2-00 (ADR 0030): a setup filled on a manufacturer-style sheet, on a phone, without network.
test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const teamUrl = `/teams/${teamId}`;

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
  expect(results.violations.filter((v) => v.impact === "serious" || v.impact === "critical")).toEqual([]);
}

test("fills a whole example sheet offline, with drawings, kit values and plain-text help", async ({ page, context }) => {
  await signIn(page);

  // The car picks its sheet from the catalogue that the first Pull brought.
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Acme");
  await page.getByLabel("Modelo", { exact: true }).fill("A8");
  await page.getByLabel("Alcunha").fill("Carro A");
  await page.getByLabel("Ficha do carro").selectOption({ label: "Acme A8 — ficha de exemplo" });
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: "Carro A" })).toBeVisible();

  await page.getByRole("link", { name: "Setups", exact: true }).click();
  await page.getByRole("link", { name: "Novo setup" }).first().click();
  await expect(page.getByText("Ficha: Acme A8 — ficha de exemplo")).toBeVisible();
  await page.getByLabel("Nome").fill("Pista seca");
  await page.getByRole("button", { name: "Criar setup" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();

  // Visit the screens the flow needs while still online (no service worker yet), then go offline.
  await page.getByRole("link", { name: "Cancelar" }).click();
  await expect(page.getByRole("heading", { name: "Pista seca" })).toBeVisible();
  await page.getByRole("link", { name: "Editar" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();
  await context.setOffline(true);

  const started = Date.now();
  // Positions: the drawing is the selector.
  await page.getByRole("group", { name: "Posição do amortecedor" }).getByRole("button", { name: /Furo 3/ }).click();
  await expect(page.getByRole("group", { name: "Posição do amortecedor" }).getByRole("button", { name: /Furo 3/ })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("group", { name: "Posição do braço" }).getByRole("button", { name: /Furo 2/ }).click();
  // Numbers with units, and the kit value as a shortcut.
  await page.getByRole("textbox", { name: /^Caster/ }).fill("2,5");
  await page.getByRole("textbox", { name: /^Camber/ }).fill("-1");
  await page.getByRole("textbox", { name: /^Altura ao solo/ }).fill("7");
  await expect(page.getByText("Kit: 5000 cSt").first()).toBeVisible();
  await page.getByRole("button", { name: "Igual ao kit" }).first().click();
  await page.getByRole("textbox", { name: /^Óleo diferencial de trás/ }).fill("400");
  // An optional part, a choice of parts and a note.
  await page.getByRole("group", { name: "Disco" }).getByRole("button", { name: "Sim" }).click();
  await page.getByRole("group", { name: "Reforço do chassis" }).getByRole("button", { name: "T2311" }).click();
  // Several parts at once, and a table laid out like the paper sheet (the column above each cell).
  const pivot = page.getByRole("group", { name: "Peças do pivô" });
  await pivot.getByRole("button", { name: /E2146/ }).click();
  await pivot.getByRole("button", { name: /E0159/ }).click();
  await expect(pivot.getByRole("button", { name: /E2146/ })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText("2 marcadas")).toBeVisible();
  const oil = page.getByRole("group", { name: "Óleo dos amortecedores" });
  await oil.getByRole("textbox", { name: /^Óleo amortecedor frente/ }).fill("750");
  await oil.getByRole("textbox", { name: /^Óleo amortecedor trás/ }).fill("600");
  await page.getByRole("group", { name: "Mola" }).getByRole("textbox", { name: /^Mola frente/ }).fill("9,25");
  await page.getByRole("textbox", { name: /^Notas/ }).fill("Boa tração");

  // The help is text: a tag in it is shown as typed, and nothing runs.
  await page.getByRole("button", { name: "Ajuda: Caster" }).click();
  await expect(page.getByText("<script>alert(1)</script>", { exact: false })).toBeVisible();
  expect(await page.locator("dialog script, dialog img").count()).toBe(0);
  await expectNoSeriousA11y(page);
  await page.locator("dialog").getByRole("button", { name: "Fechar" }).click();

  await expectNoSeriousA11y(page);
  await page.getByLabel("O que mudaste (opcional)").fill("Setup base");
  await page.getByRole("button", { name: "Guardar nova versão" }).click();
  await expect(page.getByRole("heading", { name: "Pista seca" })).toBeVisible();
  const seconds = (Date.now() - started) / 1000;
  test.info().annotations.push({ type: "sheet-entry", description: `${seconds.toFixed(1)} s automated (human target: < 120 s)` });
  expect(seconds).toBeLessThan(120);

  // The saved version reads back through the sheet of its version.
  await expect(page.getByText("Ficha: Acme A8 — ficha de exemplo")).toBeVisible();
  await expect(page.getByRole("definition").filter({ hasText: "7 mm" })).toBeVisible();
  await expect(page.getByRole("definition").filter({ hasText: "T2311" })).toBeVisible();
  await expect(page.getByRole("definition").filter({ hasText: "E0159, E2146" })).toBeVisible();
  await expect(page.getByRole("definition").filter({ hasText: "750 cSt" })).toBeVisible();
  await expect(page.getByRole("definition").filter({ hasText: "Boa tração" })).toBeVisible();
  await expectNoSeriousA11y(page);
});
