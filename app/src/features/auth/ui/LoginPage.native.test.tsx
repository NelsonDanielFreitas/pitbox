import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => true }));

const login = vi.hoisted(() => vi.fn());
vi.mock("@/auth/native", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  mobileAuth: () => ({ login }),
}));

const loadSession = vi.hoisted(() => vi.fn());
vi.mock("@/auth/session-load", () => ({ loadSession }));

import { LoginCancelledError, AuthTabUnavailableError } from "@/auth/native/mobile-auth";
import { sessionQueryKey } from "@/auth/session-key";

import { LoginPage } from "./LoginPage";

function renderPage(queryClient = new QueryClient()) {
  render(
    <QueryClientProvider client={queryClient}>
      <LoginPage returnTo="/teams" loggedOut={false} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("LoginPage on the phone", () => {
  it("reads the NEW session before going on, even when a 'no session' answer is cached", async () => {
    login.mockResolvedValue(undefined);
    loadSession.mockResolvedValue({ authenticated: true, user_id: "u" });
    const queryClient = new QueryClient();
    queryClient.setQueryData(sessionQueryKey, { authenticated: false }); // what the login screen cached
    renderPage(queryClient);
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: "/teams" }));
    expect(loadSession).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(sessionQueryKey)).toEqual({ authenticated: true, user_id: "u" });
  });

  it("never stays on 'A entrar…': the button comes back after a failure", async () => {
    login.mockRejectedValue(new LoginCancelledError());
    renderPage();
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("cancelado");
    expect(screen.getByRole("button", { name: "Entrar" })).toBeEnabled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it("says to update Chrome when there is no Auth Tab", async () => {
    login.mockRejectedValue(new AuthTabUnavailableError());
    renderPage();
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Chrome");
  });
});
