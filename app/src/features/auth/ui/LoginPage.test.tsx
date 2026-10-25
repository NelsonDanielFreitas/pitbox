import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithQuery } from "@/test/render";

import { LoginPage } from "./LoginPage";

describe("LoginPage", () => {
  it("links to the BFF with an internal return path", () => {
    renderWithQuery(<LoginPage returnTo="/teams" loggedOut={false} />);
    expect(screen.getByRole("link", { name: "Entrar" })).toHaveAttribute(
      "href",
      "/auth/login?return_to=%2Fteams",
    );
  });

  it("shows the signed-out message", () => {
    renderWithQuery(<LoginPage returnTo="/teams" loggedOut />);
    expect(screen.getByRole("status")).toHaveTextContent("Sessão terminada.");
  });
});
