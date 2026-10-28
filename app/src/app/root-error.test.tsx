import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AuthUnavailableError, OriginMismatchError } from "@/shared/lib/errors";

import { RootError } from "./routes/__root";
import { NotFound } from "@/shared/ui/NotFound";

describe("root error page", () => {
  it("shows the unavailable message with a retry action", () => {
    render(<RootError error={new AuthUnavailableError()} />);
    expect(screen.getByRole("heading", { name: "Algo correu mal" })).toBeVisible();
    expect(screen.getByText("A autenticação está temporariamente indisponível.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Tentar novamente" })).toBeVisible();
  });

  it("shows the local origin mismatch in development", () => {
    render(<RootError error={new OriginMismatchError()} />);
    expect(screen.getByText(/Configuração local/)).toBeVisible();
  });

  it("shows the generic message for unknown errors", () => {
    render(<RootError error={new Error("boom")} />);
    expect(screen.getByText("Ocorreu um erro inesperado.")).toBeVisible();
  });
});

describe("not found page", () => {
  it("uses the not-found title", () => {
    render(<NotFound />);
    expect(screen.getByRole("heading", { name: "Página não encontrada" })).toBeVisible();
  });
});
