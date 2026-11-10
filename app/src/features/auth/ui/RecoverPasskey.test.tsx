import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RecoverPasskey } from "./RecoverPasskey";

afterEach(() => {
  vi.unstubAllGlobals();
});

async function ask(status: number, email = "ana@example.com") {
  const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status }));
  vi.stubGlobal("fetch", fetchMock);
  render(<RecoverPasskey />);
  await userEvent.click(screen.getByRole("button", { name: "Perdi a passkey" }));
  await userEvent.type(screen.getByLabelText("Email da conta"), email);
  await userEvent.click(screen.getByRole("button", { name: "Enviar link" }));
  return fetchMock;
}

describe("RecoverPasskey", () => {
  it("starts closed and opens a form with a labelled email field", async () => {
    render(<RecoverPasskey />);
    expect(screen.queryByLabelText("Email da conta")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Perdi a passkey" }));
    expect(screen.getByLabelText("Email da conta")).toHaveAttribute("type", "email");
  });

  it("says the same thing whether the account exists or not, and never claims an email was sent", async () => {
    await ask(202);
    const alert = await screen.findByRole("status");
    expect(alert).toHaveTextContent("Se existir uma conta com passkey");
    expect(alert).not.toHaveTextContent("enviámos um email para ana@example.com");
    expect(alert).not.toHaveTextContent("ana@example.com");
  });

  it("says to wait when the server is rate limiting", async () => {
    await ask(429);
    expect(await screen.findByRole("status")).toHaveTextContent("Demasiados pedidos");
  });

  it("says it could not send when the server fails", async () => {
    await ask(503);
    expect(await screen.findByRole("status")).toHaveTextContent("Não foi possível enviar");
  });

  it("does not call the server for an address that is not valid", async () => {
    const fetchMock = await ask(202, "nao-e-email");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(await screen.findByRole("status")).toHaveTextContent("email válido");
    expect(screen.getByLabelText("Email da conta")).toHaveAttribute("aria-invalid", "true");
  });
});
