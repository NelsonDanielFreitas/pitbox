import { chromium, expect, test, type BrowserContext, type Page } from "@playwright/test";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

// F1-14b / ADR 0028: the installed app opens cold without network, from the shell cached by
// the service worker and the local database, but only with a valid offline pass.

const app = "http://localhost:4173";
const bff = "http://127.0.0.1:4010";
const lastUserKey = "pitbox.last-user.v1";
const userOne = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";

let userDataDir: string;
let context: BrowserContext;

test.beforeEach(async () => {
  await fetch(`${bff}/__test/reset`, { method: "POST" });
  userDataDir = await mkdtemp(join(tmpdir(), "pitbox-offline-"));
  context = await chromium.launchPersistentContext(userDataDir, { headless: true });
});

test.afterEach(async () => {
  await context.close().catch(() => undefined);
  await rm(userDataDir, { recursive: true, force: true });
  await fetch(`${bff}/__test/reset`, { method: "POST" });
});

async function reopen(): Promise<Page> {
  await context.close();
  context = await chromium.launchPersistentContext(userDataDir, { headless: true });
  return context.newPage();
}

async function pulls(): Promise<number> {
  const response = await fetch(`${bff}/__test/stats`);
  const body = (await response.json()) as { pulls: number };
  return body.pulls;
}

// Signs in, waits for the service worker to control the app and for the first sync, which is
// what leaves the offline pass in the local database.
async function signInAndSettle(page: Page): Promise<void> {
  await page.goto(`${app}/login`);
  await page.getByRole("link", { name: "Entrar" }).click();
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  await expect.poll(pulls).toBeGreaterThan(0);
}

async function cacheKeys(page: Page): Promise<string[]> {
  return page.evaluate(async () => {
    const paths: string[] = [];
    for (const name of await caches.keys()) {
      const cache = await caches.open(name);
      for (const request of await cache.keys()) paths.push(new URL(request.url).pathname);
    }
    return paths;
  });
}

async function databaseFolders(page: Page): Promise<string[]> {
  return page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    try {
      const pitbox = await root.getDirectoryHandle("pitbox");
      const names: string[] = [];
      for await (const [name] of pitbox.entries()) names.push(name);
      return names.sort();
    } catch {
      return [];
    }
  });
}

test("cold start without network shows the local data when the offline pass is valid", async () => {
  let page = await context.newPage();
  await signInAndSettle(page);

  page = await reopen();
  await context.setOffline(true);
  await page.goto(`${app}/teams`);
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /Equipa Beta/ })).toBeVisible();
  await expect(page.getByText(/Sessão offline/)).toBeVisible();
});

test("cold start without network and without a valid pass shows «Sem rede» and no data", async () => {
  await fetch(`${bff}/__test/pass-ttl?seconds=-60`, { method: "POST" });
  let page = await context.newPage();
  await signInAndSettle(page);

  page = await reopen();
  await context.setOffline(true);
  await page.goto(`${app}/teams`);
  await expect(page.getByRole("heading", { name: "Sem rede" })).toBeVisible();
  await expect(page.getByText("Sem rede. Liga-te para continuar.")).toBeVisible();
  await expect(page.getByText(/Equipa (Alpha|Beta)/)).toHaveCount(0);
});

test("a 401 goes to the login and keeps the local database", async () => {
  const page = await context.newPage();
  await signInAndSettle(page);
  const before = await databaseFolders(page);
  expect(before).toHaveLength(1);

  await context.clearCookies();
  await page.reload();
  await expect(page).toHaveURL(/\/login/);
  expect(await page.evaluate((key) => localStorage.getItem(key), lastUserKey)).toBeNull();
  expect(await databaseFolders(page)).toEqual(before);
});

test("/auth/* and /pitbox.* never reach the Cache Storage", async () => {
  const page = await context.newPage();
  await signInAndSettle(page);
  await page.getByRole("link", { name: /Equipa Alpha/ }).click();

  const keys = await cacheKeys(page);
  expect(keys).toContain("/index.html");
  expect(keys.some((path) => path.endsWith(".wasm"))).toBe(true);
  expect(keys.filter((path) => path.startsWith("/auth/") || path.startsWith("/pitbox."))).toEqual([]);
  const manifest = await page.evaluate(() => document.querySelector('link[rel="manifest"]')?.getAttribute("href"));
  expect(manifest).toBe("/manifest.webmanifest");
});

test("the manifest makes the app installable and the service worker controls the page", async () => {
  const page = await context.newPage();
  await signInAndSettle(page);
  const manifest = (await (await page.request.get(`${app}/manifest.webmanifest`)).json()) as {
    display: string;
    icons: { sizes: string; purpose?: string }[];
  };
  expect(manifest.display).toBe("standalone");
  expect(manifest.icons.map((icon) => `${icon.sizes}:${icon.purpose}`)).toEqual(
    expect.arrayContaining(["192x192:any", "512x512:any", "512x512:maskable"]),
  );
  await page.reload();
  expect(await page.evaluate(() => navigator.serviceWorker.controller !== null)).toBe(true);
});

test("a new version shows the notice and does not activate by itself", async () => {
  const page = await context.newPage();
  await signInAndSettle(page);
  await page.reload();

  // A new build is the same worker plus a comment: the browser sees different bytes, installs
  // it, and it must wait. (Playwright cannot rewrite the worker request, so the file served by
  // `vite preview` is changed on disk for the duration of this test.)
  const worker = join(process.cwd(), "dist", "sw.js");
  const original = await readFile(worker, "utf8");
  try {
    await writeFile(worker, `${original}\n// next version ${Date.now()}`);
    await page.evaluate(async () => {
      const registration = await navigator.serviceWorker.getRegistration();
      await registration?.update();
    });
    await expectUpdateNotice(page);
  } finally {
    await writeFile(worker, original);
  }
});

async function expectUpdateNotice(page: Page): Promise<void> {
  await expect(page.getByText("Há uma versão nova — atualizar")).toBeVisible();
  const state = await page.evaluate(async () => {
    const registration = await navigator.serviceWorker.getRegistration();
    return { waiting: registration?.waiting !== null, active: registration?.active?.state };
  });
  expect(state).toEqual({ waiting: true, active: "activated" });

  // Only the click activates it; the page reloads on the new version and the notice is gone.
  await page.getByRole("button", { name: "Atualizar" }).click();
  await expect(page.getByText("Há uma versão nova — atualizar")).toHaveCount(0);
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();
  const waiting = await page.evaluate(async () => {
    const registration = await navigator.serviceWorker.getRegistration();
    return registration?.waiting ?? null;
  });
  expect(waiting).toBeNull();
}

test("logout clears the last user", async () => {
  const page = await context.newPage();
  await signInAndSettle(page);
  expect(await page.evaluate((key) => localStorage.getItem(key), lastUserKey)).toBe(
    JSON.stringify({ user_id: userOne }),
  );

  await page.getByRole("button", { name: "Conta" }).click();
  await page.getByRole("menuitem", { name: "Terminar sessão" }).click();
  await page.getByRole("button", { name: "Apagar dados e terminar sessão" }).click();
  await expect(page).toHaveURL(/\/login\?logged_out=1$/);
  expect(await page.evaluate((key) => localStorage.getItem(key), lastUserKey)).toBeNull();
});

test("a tampered last user does not open any data", async () => {
  let page = await context.newPage();
  await signInAndSettle(page);
  const folders = await databaseFolders(page);

  for (const tampered of [
    JSON.stringify({ user_id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d99" }), // a valid id with no database
    JSON.stringify({ user_id: "../../pitbox" }),
    "not json",
  ]) {
    await page.evaluate(([key, value]) => localStorage.setItem(key ?? "", value ?? ""), [lastUserKey, tampered]);
    page = await reopen();
    await context.setOffline(true);
    await page.goto(`${app}/teams`);
    await expect(page.getByRole("heading", { name: "Sem rede" })).toBeVisible();
    await expect(page.getByText(/Equipa (Alpha|Beta)/)).toHaveCount(0);
    await context.setOffline(false);
    await page.goto(`${app}/login`); // any online page: only to read the folders again
    expect(await databaseFolders(page)).toEqual(folders);
  }
});

test("when the network returns, the same user carries on and another user starts clean", async () => {
  const page = await context.newPage();
  await signInAndSettle(page);

  await context.setOffline(true);
  await page.reload();
  await expect(page.getByText(/Sessão offline/)).toBeVisible();
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();

  // Network back, same account: the banner goes away and the data stays.
  await context.setOffline(false);
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.getByText(/Sessão offline/)).toHaveCount(0);
  await expect(page.getByRole("link", { name: /Equipa Alpha/ })).toBeVisible();

  // Offline again, and while offline another account signs in on this browser.
  await context.setOffline(true);
  await page.reload();
  await expect(page.getByText(/Sessão offline/)).toBeVisible();
  await fetch(`${bff}/__test/next-user?session=mock-session-2`, { method: "POST" });
  const login = await context.request.get(`${app}/auth/login?return_to=%2Fteams`);
  expect(login.ok()).toBe(true);
  await context.setOffline(false);
  await page.evaluate(() => window.dispatchEvent(new Event("online")));

  await expect(page.getByText(/Sessão offline/)).toHaveCount(0, { timeout: 15_000 });
  await expect(page.getByText(/Equipa (Alpha|Beta)/)).toHaveCount(0);
});
