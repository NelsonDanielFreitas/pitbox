import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const native = vi.hoisted(() => ({ value: false }));
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => native.value }));

const generate = vi.hoisted(() => vi.fn());
vi.mock("@/features/auth/data/recovery-codes", () => ({ generateRecoveryCodes: generate }));

const stepUp = vi.hoisted(() => vi.fn());
vi.mock("@/auth/native", () => {
  class LoginCancelledError extends Error {}
  class LoginRefusedError extends Error {
    constructor(readonly reason: string) {
      super("refused");
    }
  }
  return { mobileAuth: () => ({ stepUp }), LoginCancelledError, LoginRefusedError };
});

import { LoginCancelledError, LoginRefusedError } from "@/auth/native";

import { RecoveryCodesDialog } from "./RecoveryCodesDialog";

function renderDialog(onGenerated = vi.fn()) {
  render(<RecoveryCodesDialog open onClose={vi.fn()} onGenerated={onGenerated} />);
  return onGenerated;
}

async function generateNow() {
  const buttons = await screen.findAllByRole("button", { name: "Gerar códigos de recuperação", hidden: true });
  const last = buttons[buttons.length - 1];
  if (!last) throw new Error("no generate button");
  await userEvent.click(last);
}

beforeEach(() => {
  vi.clearAllMocks();
  native.value = false;
});

describe("RecoveryCodesDialog on the phone", () => {
  it("asks the phone to reauthenticate, then generates again", async () => {
    native.value = true;
    generate.mockResolvedValueOnce({ status: "stepUp" }).mockResolvedValueOnce({ status: "ok", codes: ["AAAA-BBBB"] });
    stepUp.mockResolvedValue(undefined);
    const onGenerated = renderDialog();
    await generateNow();
    await userEvent.click(await screen.findByRole("button", { name: "Confirmar identidade", hidden: true }));
    expect(await screen.findByText("AAAA-BBBB")).toBeInTheDocument();
    expect(stepUp).toHaveBeenCalledOnce();
    expect(generate).toHaveBeenCalledTimes(2);
    expect(onGenerated).toHaveBeenCalledOnce();
  });

  it("says a password is not enough when the server refuses a weak factor", async () => {
    native.value = true;
    generate.mockResolvedValue({ status: "stepUp" });
    stepUp.mockRejectedValue(new LoginRefusedError("weak_factor"));
    renderDialog();
    await generateNow();
    await userEvent.click(await screen.findByRole("button", { name: "Confirmar identidade", hidden: true }));
    expect(await screen.findByText(/precisa de uma passkey/)).toBeInTheDocument();
    expect(generate).toHaveBeenCalledOnce();
  });

  it("stays quiet when the user cancels the browser", async () => {
    native.value = true;
    generate.mockResolvedValue({ status: "stepUp" });
    stepUp.mockRejectedValue(new LoginCancelledError());
    renderDialog();
    await generateNow();
    await userEvent.click(await screen.findByRole("button", { name: "Confirmar identidade", hidden: true }));
    expect(screen.queryByText(/Não foi possível confirmar/)).toBeNull();
  });

  it("does not report generated codes when the server refused", async () => {
    generate.mockResolvedValue({ status: "unavailable" });
    const onGenerated = renderDialog();
    await generateNow();
    expect(await screen.findByText("Não foi possível gerar os códigos. Tenta outra vez.")).toBeInTheDocument();
    expect(onGenerated).not.toHaveBeenCalled();
  });
});
