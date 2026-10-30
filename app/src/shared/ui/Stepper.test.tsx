import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { Stepper } from "./Stepper";

function Harness({ wholeOnly, initial = null }: { wholeOnly: boolean; initial?: number | null }) {
  const [value, setValue] = useState<number | null>(initial);
  return (
    <>
      <Stepper
        value={value}
        onChange={setValue}
        wholeOnly={wholeOnly}
        inputLabel="Contado"
        decreaseLabel="Menos"
        increaseLabel="Mais"
      />
      <output data-testid="value">{value === null ? "null" : value}</output>
    </>
  );
}

describe("Stepper", () => {
  it("steps by one unit and never goes below zero", async () => {
    render(<Harness wholeOnly initial={100} />);
    await userEvent.click(screen.getByRole("button", { name: "Mais" }));
    expect(screen.getByLabelText("Contado")).toHaveValue("2");
    await userEvent.click(screen.getByRole("button", { name: "Menos" }));
    await userEvent.click(screen.getByRole("button", { name: "Menos" }));
    await userEvent.click(screen.getByRole("button", { name: "Menos" }));
    expect(screen.getByTestId("value")).toHaveTextContent("0");
  });

  it("starts from zero when nothing was counted", async () => {
    render(<Harness wholeOnly />);
    expect(screen.getByLabelText("Contado")).toHaveValue("");
    await userEvent.click(screen.getByRole("button", { name: "Mais" }));
    expect(screen.getByTestId("value")).toHaveTextContent("100");
  });

  it("keeps only digits for whole units", async () => {
    render(<Harness wholeOnly />);
    await userEvent.type(screen.getByLabelText("Contado"), "1a,5");
    expect(screen.getByLabelText("Contado")).toHaveValue("15");
    expect(screen.getByTestId("value")).toHaveTextContent("1500");
  });

  it("accepts up to two decimals, with a comma or a dot", async () => {
    render(<Harness wholeOnly={false} />);
    await userEvent.type(screen.getByLabelText("Contado"), "1.256");
    expect(screen.getByLabelText("Contado")).toHaveValue("1,25");
    expect(screen.getByTestId("value")).toHaveTextContent("125");
  });

  it("clears the value when the text is erased", async () => {
    render(<Harness wholeOnly initial={300} />);
    await userEvent.clear(screen.getByLabelText("Contado"));
    expect(screen.getByTestId("value")).toHaveTextContent("null");
  });
});
