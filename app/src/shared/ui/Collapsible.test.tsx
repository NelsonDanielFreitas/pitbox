import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { Collapsible } from "./Collapsible";

describe("Collapsible", () => {
  it("starts closed and opens with the button, announcing its state", async () => {
    render(
      <Collapsible title="Diferenciais" summary="2 alterações">
        <p>conteúdo</p>
      </Collapsible>,
    );
    const button = screen.getByRole("button", { name: /Diferenciais/ });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("conteúdo")).not.toBeInTheDocument();

    await userEvent.click(button);
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("region", { name: /Diferenciais/ })).toHaveTextContent("conteúdo");

    await userEvent.click(button);
    expect(screen.queryByText("conteúdo")).not.toBeInTheDocument();
  });

  it("can start open and shows the summary inside the button", () => {
    render(
      <Collapsible title="Geometria" summary="3 alterações" defaultOpen>
        <p>campos</p>
      </Collapsible>,
    );
    expect(screen.getByRole("button", { name: /3 alterações/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("campos")).toBeVisible();
  });

  it("works from the keyboard", async () => {
    render(
      <Collapsible title="Rádio">
        <p>epa</p>
      </Collapsible>,
    );
    await userEvent.tab();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByText("epa")).toBeInTheDocument();
  });
});
