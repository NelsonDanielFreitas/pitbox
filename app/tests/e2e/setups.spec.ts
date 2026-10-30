import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

test.use({ viewport: { width: 390, height: 844 } });

const teamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61";
const pilotTeamId = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62";
const teamUrl = `/teams/${teamId}`;
// Screenshots are only rewritten on request: PITBOX_CAPTURE=1 pnpm e2e setups
const capture = process.env.PITBOX_CAPTURE === "1";

async function shot(page: Page, name: string) {
  if (capture) await page.screenshot({ path: `../docs/design/f1-12/${name}-390x844.png` });
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

async function group(page: Page, name: RegExp) {
  await page.getByRole("button", { name }).click();
  return page.getByRole("region", { name });
}

test("owner fills, versions, compares and rates a setup while offline", async ({ page, context }) => {
  await signIn(page);

  // A car to hang the setups on.
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByLabel("Alcunha").fill("Carro A");
  await page.getByRole("button", { name: "Guardar" }).click();
  await expect(page.getByRole("heading", { name: "Carro A" })).toBeVisible();

  // First setup, blank.
  await page.getByRole("link", { name: "Setups", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Ainda sem setups" })).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "list-empty");
  await page.getByRole("link", { name: "Novo setup" }).first().click();
  await page.getByLabel("Nome").fill("Pista seca");
  await expectNoSeriousA11y(page);
  await shot(page, "new");
  await page.getByRole("button", { name: "Criar setup" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();

  // Fill a realistic setup and time it. Playwright types far faster than a person, so this is
  // a floor; the figure that matters is how many fields and taps a full sheet takes.
  const started = Date.now();
  let fields = 0;
  const diffs = await group(page, /^Diferenciais/);
  for (const [label, value] of [
    [/Óleo do diferencial da frente/, "500"],
    [/Óleo do diferencial central/, "400"],
    [/Óleo do diferencial de trás/, "300"],
  ] as const) {
    await diffs.getByLabel(label).fill(value);
    fields++;
  }
  const front = await group(page, /^Amortecedores da frente/);
  await front.getByLabel(/^Óleo/).fill("400");
  await front.getByLabel(/Furos do pistão/).fill("3");
  fields += 2;
  const rear = await group(page, /^Amortecedores de trás/);
  await rear.getByLabel(/^Óleo/).fill("350");
  await rear.getByLabel(/Furos do pistão/).fill("2");
  fields += 2;
  const geometry = await group(page, /^Geometria/);
  await geometry.getByLabel(/Camber da frente/).fill("-2");
  await geometry.getByLabel(/Convergência da frente/).fill("1,5");
  await geometry.getByLabel(/Altura ao solo/).fill("18");
  fields += 3;
  const transmission = await group(page, /^Transmissão/);
  await transmission.getByLabel(/Dentes da campana/).fill("13");
  await transmission.getByLabel(/Dentes da coroa/).fill("46");
  fields += 2;
  await expect(page.getByText("Relação de transmissão: 3,54")).toBeVisible();
  const engine = await group(page, /^Motor e combustível/);
  await engine.getByLabel(/Agulha alta/).fill("2,5");
  await engine.getByLabel(/Agulha baixa/).fill("3");
  await engine.getByLabel(/Nitro do combustível/).fill("25");
  fields += 3;
  const radio = await group(page, /^Rádio/);
  await radio.getByLabel(/EPA da direção/).fill("100");
  await radio.getByLabel(/Expo da direção/).fill("20");
  fields += 2;
  await page.getByLabel("O que mudaste (opcional)").fill("Setup base");
  await expect(page.getByText(`${fields} alterações`, { exact: true }).last()).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "edit");
  await page.getByRole("button", { name: "Guardar nova versão" }).click();
  await expect(page.getByRole("heading", { name: "Pista seca" })).toBeVisible();
  const seconds = (Date.now() - started) / 1000;
  test.info().annotations.push({
    type: "setup-entry",
    description: `${fields} fields in ${seconds.toFixed(1)} s automated (human target: < 120 s)`,
  });
  expect(seconds).toBeLessThan(120);
  await expect(page.getByText("3,54")).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "detail-v2");

  // Route chunks load on demand and there is no service worker yet: visit every route the
  // rest of the flow needs while still online, by client-side navigation.
  await navigateInApp(page, `${teamUrl}/setups`);
  await expect(page.getByRole("link", { name: /Pista seca/ })).toBeVisible();
  await navigateInApp(page, `${teamUrl}/setups/new`);
  await expect(page.getByRole("heading", { name: "Novo setup" })).toBeVisible();
  await page.getByRole("link", { name: "Cancelar" }).click();
  await page.getByRole("link", { name: /Pista seca/ }).click();
  await page.getByRole("link", { name: "Editar" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();
  await page.getByRole("link", { name: "Cancelar" }).click();
  await page.getByRole("link", { name: "Comparar versões" }).click();
  await expect(page.getByRole("heading", { name: "Comparar versões" })).toBeVisible();

  // From here on nothing needs the network.
  await context.setOffline(true);

  await page.getByRole("link", { name: "Setups", exact: true }).click();
  await page.getByRole("link", { name: "Novo setup" }).first().click();
  await page.getByLabel("Nome").fill("Pista molhada");
  await page.getByRole("radio", { name: /Último setup usado: Pista seca/ }).check();
  await page.getByRole("button", { name: "Criar setup" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();

  // The copy starts from the last sheet: change just two things.
  const wetDiffs = await group(page, /^Diferenciais/);
  await expect(wetDiffs.getByLabel(/Óleo do diferencial da frente/)).toHaveValue("500");
  await wetDiffs.getByLabel(/Óleo do diferencial da frente/).fill("700");
  await expect(page.getByText("Antes: 500 cSt")).toBeVisible();
  const wetGeometry = await group(page, /^Geometria/);
  await wetGeometry.getByLabel(/Camber da frente/).fill("-1,5");
  await page.getByLabel("O que mudaste (opcional)").fill("Mais grosso para o molhado");
  await page.getByRole("button", { name: "Guardar nova versão" }).click();

  // "What changed" sits on top of the sheet and the history keeps both versions.
  await expect(page.getByRole("heading", { name: "Pista molhada" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "O que mudou desde a v1" })).toBeVisible();
  await expect(page.getByText("Mais grosso para o molhado").first()).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "detail-changes");

  await page.getByRole("link", { name: "Comparar versões" }).click();
  await expect(page.getByText("2 diferenças entre a v1 e a v2")).toBeVisible();
  await expect(page.getByText("Alterado").first()).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "compare");

  // Feedback on the version being looked at.
  await page.getByRole("link", { name: "Voltar ao setup" }).click();
  await page.getByRole("button", { name: "Dar feedback" }).click();
  const sheet = page.getByRole("dialog", { name: "Feedback da v2" });
  await sheet.getByRole("group", { name: "Tração" }).getByText("4", { exact: true }).click();
  await sheet.getByRole("group", { name: "Travagem" }).getByText("2", { exact: true }).click();
  await sheet.getByLabel("Zona da pista").fill("Curva 3");
  await expectNoSeriousA11y(page);
  await shot(page, "feedback-sheet");
  await sheet.getByRole("button", { name: "Guardar feedback" }).click();
  await expect(page.getByText("Tração: 4 de 5")).toBeVisible();
  await expect(page.getByText("Travagem: 2 de 5")).toBeVisible();

  // An older version opens read-only.
  await page.getByRole("link", { name: /^v1 ·/ }).click();
  await expect(page.getByText("A ver a v1 (não é a atual)")).toBeVisible();
  await expect(page.getByRole("link", { name: "Editar" })).toHaveCount(0);

  await page.getByRole("link", { name: "Setups", exact: true }).click();
  await expect(page.getByRole("link", { name: /Pista molhada/ })).toBeVisible();
  await expectNoSeriousA11y(page);
  await shot(page, "list");

  await context.setOffline(false);
});

test("pilot reads setups but cannot create or edit them", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: /tocar para mudar/ }).click();
  await page.getByRole("link", { name: /Equipa Beta/ }).click();
  await expect(page).toHaveURL(new RegExp(`${pilotTeamId}$`));

  await page.getByRole("link", { name: "Setups", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Setups", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Novo setup" })).toHaveCount(0);
  await expectNoSeriousA11y(page);

  await page.goto(`/teams/${pilotTeamId}/setups/new`);
  await expect(page.getByText("Sem permissão")).toBeVisible();
  await page.goto(`/teams/${pilotTeamId}/setups/0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4dff/edit`);
  await expect(page.getByText("Sem permissão")).toBeVisible();
  await expectNoSeriousA11y(page);
});

test("the setup list does not leak into another team", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: "Garagem", exact: true }).click();
  await page.getByRole("link", { name: "Novo carro" }).first().click();
  await page.getByLabel("Marca", { exact: true }).fill("Mugen");
  await page.getByLabel("Modelo", { exact: true }).fill("MBX8");
  await page.getByRole("button", { name: "Guardar" }).click();
  await page.getByRole("link", { name: "Setups", exact: true }).click();
  await page.getByRole("link", { name: "Novo setup" }).first().click();
  await page.getByLabel("Nome").fill("Só da Alpha");
  await page.getByRole("button", { name: "Criar setup" }).click();
  await expect(page.getByRole("heading", { name: "Editar setup" })).toBeVisible();

  await page.goto(`/teams/${pilotTeamId}/setups`);
  await expect(page.getByRole("heading", { name: "Setups", exact: true })).toBeVisible();
  await expect(page.getByText("Só da Alpha")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Ainda sem setups" })).toBeVisible();
});
