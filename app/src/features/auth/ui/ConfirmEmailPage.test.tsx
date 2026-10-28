import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithQuery } from "@/test/render";

import { ConfirmEmailPage } from "./ConfirmEmailPage";

describe("ConfirmEmailPage", () => {
  it("offers a safe way back to login", () => {
    renderWithQuery(<ConfirmEmailPage returnTo="/teams" />);
    expect(screen.getByRole("heading", { name: "Confirma o teu email" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Já confirmei, entrar outra vez" })).toHaveAttribute(
      "href",
      "/auth/login?return_to=%2Fteams",
    );
  });
});
