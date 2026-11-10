import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const native = vi.hoisted(() => ({ value: false }));
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => native.value }));

const api = vi.hoisted(() => ({
  listSessions: vi.fn(),
  recoveryCodesStatus: vi.fn(),
  revokeSession: vi.fn(),
  revokeOtherSessions: vi.fn(),
}));
vi.mock("@/features/account/data/account-api", () => api);
vi.mock("@/features/auth/data/recovery-codes", () => ({ generateRecoveryCodes: vi.fn() }));
vi.mock("@/auth/native", () => ({ mobileAuth: () => ({ stepUp: vi.fn() }), LoginCancelledError: class {}, LoginRefusedError: class {} }));

const useSession = vi.hoisted(() => vi.fn());
vi.mock("@/auth/session-query", () => ({ useSession }));

import { AccountPage } from "./AccountPage";

const REF_A = "a".repeat(64);
const REF_B = "b".repeat(64);
const base = { created_at: "2026-11-10T20:30:16Z", auth_time: "2026-11-10T20:30:16Z", ip: "203.0.113.7" };
const here = { ...base, ref: REF_A, kind: "web", user_agent: "Firefox", current: true };
const phone = { ...base, ref: REF_B, kind: "mobile", user_agent: "okhttp", current: false };

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AccountPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  native.value = false;
  useSession.mockReturnValue({ data: { authenticated: true, user_id: "u", amr: ["user"], auth_time: base.auth_time } });
  api.listSessions.mockResolvedValue({ status: "ok", sessions: [here, phone] });
  api.recoveryCodesStatus.mockResolvedValue({ status: "ok", unused: 8 });
  api.revokeSession.mockResolvedValue({ status: "ok" });
  api.revokeOtherSessions.mockResolvedValue({ status: "ok", revoked: 1 });
});

describe("AccountPage", () => {
  it("lists the sessions, marks the current one and offers to end only the others", async () => {
    renderPage();
    const list = await screen.findByRole("list", { name: "Sessões abertas" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(within(items[0] as HTMLElement).getByText("Esta sessão")).toBeInTheDocument();
    expect(within(items[0] as HTMLElement).queryByRole("button")).toBeNull();
    expect(within(items[1] as HTMLElement).getByText("Telemóvel (app)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Terminar a sessão: Telemóvel (app)" })).toBeInTheDocument();
  });

  it("ends one session and reloads the list", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Terminar a sessão: Telemóvel (app)" }));
    await waitFor(() => expect(api.revokeSession).toHaveBeenCalledWith(REF_B));
    await waitFor(() => expect(api.listSessions).toHaveBeenCalledTimes(2));
  });

  it("ends all the other sessions", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Terminar todas as outras sessões" }));
    await waitFor(() => expect(api.revokeOtherSessions).toHaveBeenCalledOnce());
  });

  it("offers nothing to end when this is the only session", async () => {
    api.listSessions.mockResolvedValue({ status: "ok", sessions: [here] });
    renderPage();
    await screen.findByRole("list", { name: "Sessões abertas" });
    expect(screen.queryByRole("button", { name: "Terminar todas as outras sessões" })).toBeNull();
  });

  it("tells the user when an action failed", async () => {
    api.revokeSession.mockResolvedValue({ status: "unavailable" });
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Terminar a sessão: Telemóvel (app)" }));
    expect(await screen.findByText("Não foi possível concluir a ação. Tenta outra vez.")).toBeInTheDocument();
  });

  it("shows device text as text, never as markup", async () => {
    api.listSessions.mockResolvedValue({
      status: "ok",
      sessions: [{ ...here, user_agent: "<img src=x onerror=alert(1)>" }],
    });
    renderPage();
    expect(await screen.findByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });

  it.each([
    [8, "Tens 8 códigos por usar.", null],
    [1, "Tens 1 código por usar.", "Restam poucos códigos"],
    [0, "Tens 0 códigos por usar.", "Não tens códigos"],
  ])("says how many recovery codes are left (%i)", async (unused, text, warning) => {
    api.recoveryCodesStatus.mockResolvedValue({ status: "ok", unused });
    renderPage();
    expect(await screen.findByTestId("codes-left")).toHaveTextContent(text);
    if (warning) expect(screen.getByText(new RegExp(warning))).toBeInTheDocument();
    else expect(screen.queryByText(/Restam poucos|Não tens códigos/)).toBeNull();
  });

  it("says it could not load instead of showing a made-up number", async () => {
    api.recoveryCodesStatus.mockResolvedValue({ status: "unavailable" });
    api.listSessions.mockResolvedValue({ status: "unavailable" });
    renderPage();
    await waitFor(() =>
      expect(screen.getAllByText("Não foi possível carregar. Tenta outra vez mais tarde.")).toHaveLength(2),
    );
    expect(screen.queryByTestId("codes-left")).toBeNull();
  });

  it("asks nothing of the server in an offline session", async () => {
    useSession.mockReturnValue({ data: { authenticated: true, offline: true, user_id: "u", amr: [] } });
    renderPage();
    expect(await screen.findByText(/precisa de ligação ao servidor/)).toBeInTheDocument();
    expect(api.listSessions).not.toHaveBeenCalled();
    expect(api.recoveryCodesStatus).not.toHaveBeenCalled();
  });

  it("opens the identity provider's page for passkeys on the web, and points to the web on the phone", async () => {
    renderPage();
    const link = await screen.findByRole("link", { name: "Gerir passkeys e MFA" });
    expect(link).toHaveAttribute("href", "/auth/account-security");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("on the phone there is no link, only the pointer to the web version", async () => {
    native.value = true;
    renderPage();
    expect(await screen.findByText(/versão web/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Gerir passkeys e MFA" })).toBeNull();
  });
});
