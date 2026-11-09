import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const isNative = vi.fn<() => boolean>();
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => isNative() }));

const state = vi.hoisted(() => ({
  listener: undefined as ((s: { isActive: boolean }) => void) | undefined,
  removed: false,
}));
vi.mock("@capacitor/app", () => ({
  App: {
    addListener: (_event: string, fn: (s: { isActive: boolean }) => void) => {
      state.listener = fn;
      return Promise.resolve({ remove: () => Promise.resolve((state.removed = true)) });
    },
  },
}));

const authenticate = vi.hoisted(() => vi.fn());
vi.mock("pitbox-secure", () => ({ PitboxSecure: { authenticate } }));

const closeDatabase = vi.hoisted(() => vi.fn());
vi.mock("@/db/client", () => ({ closeDatabase }));

import { AppLockGate } from "./AppLockGate";

function renderGate() {
  const queryClient = new QueryClient();
  const clear = vi.spyOn(queryClient, "clear");
  render(
    <QueryClientProvider client={queryClient}>
      <AppLockGate>
        <p>dados da equipa</p>
      </AppLockGate>
    </QueryClientProvider>,
  );
  return { clear };
}

const refusal = (code: string) => Object.assign(new Error("refused"), { code });

beforeEach(() => {
  vi.clearAllMocks();
  state.listener = undefined;
  state.removed = false;
  isNative.mockReturnValue(true);
  closeDatabase.mockResolvedValue(undefined);
});

describe("AppLockGate", () => {
  it("shows the content straight away on the web, with no lock and no listener", () => {
    isNative.mockReturnValue(false);
    renderGate();
    expect(screen.getByText("dados da equipa")).toBeInTheDocument();
    expect(authenticate).not.toHaveBeenCalled();
    expect(state.listener).toBeUndefined();
  });

  it("starts locked on the phone, asks for the biometrics and shows nothing until they pass", async () => {
    let finish: () => void = () => undefined;
    authenticate.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));
    const { clear } = renderGate();
    expect(screen.queryByText("dados da equipa")).not.toBeInTheDocument();
    await waitFor(() => expect(authenticate).toHaveBeenCalledTimes(1));
    expect(clear).toHaveBeenCalled();
    expect(closeDatabase).toHaveBeenCalled();
    finish();
    expect(await screen.findByText("dados da equipa")).toBeInTheDocument();
  });

  it("stays locked when the biometrics fail or are cancelled, and lets the user try again", async () => {
    authenticate.mockRejectedValueOnce(refusal("BIOMETRIC_CANCELLED"));
    renderGate();
    expect(await screen.findByText("Não foi possível confirmar a tua identidade.")).toBeInTheDocument();
    expect(screen.queryByText("dados da equipa")).not.toBeInTheDocument();

    authenticate.mockResolvedValueOnce(undefined);
    await userEvent.click(screen.getByRole("button", { name: "Tentar outra vez" }));
    expect(await screen.findByText("dados da equipa")).toBeInTheDocument();
  });

  it("says what to do when the phone has no screen lock, and still shows nothing", async () => {
    authenticate.mockRejectedValue(refusal("BIOMETRIC_UNAVAILABLE"));
    renderGate();
    expect(await screen.findByText(/Define um bloqueio de ecrã/)).toBeInTheDocument();
    expect(screen.queryByText("dados da equipa")).not.toBeInTheDocument();
  });

  it("treats an error without a code as a failure (fail closed)", async () => {
    authenticate.mockRejectedValue("not even an Error");
    renderGate();
    expect(await screen.findByText("Não foi possível confirmar a tua identidade.")).toBeInTheDocument();
  });

  it("locks again after 15 minutes in the background and drops the readable state", async () => {
    authenticate.mockResolvedValue(undefined);
    const now = vi.spyOn(Date, "now");
    now.mockReturnValue(0);
    const { clear } = renderGate();
    await screen.findByText("dados da equipa");
    clear.mockClear();
    closeDatabase.mockClear();

    act(() => state.listener?.({ isActive: false }));
    now.mockReturnValue(15 * 60 * 1000);
    authenticate.mockReturnValue(new Promise(() => undefined)); // the second prompt stays open
    act(() => state.listener?.({ isActive: true }));

    await waitFor(() => expect(screen.queryByText("dados da equipa")).not.toBeInTheDocument());
    expect(clear).toHaveBeenCalled();
    expect(closeDatabase).toHaveBeenCalled();
  });

  it("does not lock after a short trip to the background", async () => {
    authenticate.mockResolvedValue(undefined);
    const now = vi.spyOn(Date, "now");
    now.mockReturnValue(0);
    renderGate();
    await screen.findByText("dados da equipa");

    act(() => state.listener?.({ isActive: false }));
    now.mockReturnValue(60_000);
    act(() => state.listener?.({ isActive: true }));

    expect(screen.getByText("dados da equipa")).toBeInTheDocument();
    expect(authenticate).toHaveBeenCalledTimes(1);
  });
});
