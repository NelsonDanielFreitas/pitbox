import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const native = vi.hoisted(() => ({ value: false }));
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => native.value }));

const generate = vi.hoisted(() => vi.fn());
vi.mock("@/features/auth/data/recovery-codes", () => ({ generateRecoveryCodes: generate }));

const loadSession = vi.hoisted(() => vi.fn());
vi.mock("@/auth/session-load", () => ({ loadSession }));

import { RecoveryNotice } from "./RecoveryNotice";

const online = { authenticated: true, user_id: "u", amr: ["user"], auth_time: "2026-10-13T12:00:00Z" };

/** The notice and the dialog both have a "generate" button: the dialog's is the last one. */
async function openDialogAndGenerate() {
  await userEvent.click(await screen.findByRole("button", { name: "Gerar códigos de recuperação" }));
  const buttons = await screen.findAllByRole("button", { name: "Gerar códigos de recuperação", hidden: true });
  const inDialog = buttons[buttons.length - 1];
  if (!inDialog) throw new Error("no button in the dialog");
  await userEvent.click(inDialog);
}

function renderNotice() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <RecoveryNotice />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  native.value = false;
});

describe("RecoveryNotice", () => {
  it.each([
    ["recovery_ready true", { ...online, recovery_ready: true }],
    ["recovery_ready unknown", online],
    ["offline session", { authenticated: true, offline: true, user_id: "u", amr: [] }],
    ["no session", { authenticated: false }],
  ])("shows nothing for %s", async (_name, session) => {
    loadSession.mockResolvedValue(session);
    renderNotice();
    await waitFor(() => expect(loadSession).toHaveBeenCalled());
    expect(screen.queryByText("Sem plano B para a tua conta")).toBeNull();
  });

  it("warns a passkey-only account without codes", async () => {
    loadSession.mockResolvedValue({ ...online, recovery_ready: false });
    renderNotice();
    expect(await screen.findByText("Sem plano B para a tua conta")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Gerar códigos de recuperação" })).toBeInTheDocument();
  });

  it("on the phone points to the web version instead of offering the button", async () => {
    native.value = true;
    loadSession.mockResolvedValue({ ...online, recovery_ready: false });
    renderNotice();
    expect(await screen.findByText(/versão web/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Gerar códigos de recuperação" })).toBeNull();
  });

  it("shows the codes once and forgets them when the dialog closes", async () => {
    loadSession.mockResolvedValue({ ...online, recovery_ready: false });
    generate.mockResolvedValue({ status: "ok", codes: ["AAAA-BBBB-CCCC", "DDDD-EEEE-FFFF"] });
    renderNotice();
    await openDialogAndGenerate();
    expect(await screen.findByText("AAAA-BBBB-CCCC")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Já guardei os códigos" }));
    expect(screen.queryByText("AAAA-BBBB-CCCC")).toBeNull();
    expect(screen.queryByText("Sem plano B para a tua conta")).toBeNull();
  });

  it("sends the user to confirm the identity when a strong factor is needed", async () => {
    loadSession.mockResolvedValue({ ...online, recovery_ready: false });
    generate.mockResolvedValue({ status: "stepUp" });
    renderNotice();
    await openDialogAndGenerate();
    expect(await screen.findByRole("link", { name: "Confirmar identidade", hidden: true })).toHaveAttribute(
      "href",
      expect.stringContaining("step_up=true"),
    );
  });

  it("says it failed when the codes could not be generated", async () => {
    loadSession.mockResolvedValue({ ...online, recovery_ready: false });
    generate.mockResolvedValue({ status: "unavailable" });
    renderNotice();
    await openDialogAndGenerate();
    expect(await screen.findByText("Não foi possível gerar os códigos. Tenta outra vez.", {}, { timeout: 2000 })).toBeInTheDocument();
  });
});
