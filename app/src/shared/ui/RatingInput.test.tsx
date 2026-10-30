import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RatingInput } from "./RatingInput";

function Harness({ onChange }: { onChange?: (value: number | undefined) => void }) {
  const [value, setValue] = useState<number | undefined>(undefined);
  return (
    <RatingInput
      legend="Tração"
      value={value}
      clearLabel="Limpar"
      onChange={(next) => {
        setValue(next);
        onChange?.(next);
      }}
    />
  );
}

describe("RatingInput", () => {
  it("is a radio group of 1 to 5 labelled by its legend", () => {
    render(<Harness />);
    const group = screen.getByRole("group", { name: "Tração" });
    expect(group).toBeVisible();
    expect(screen.getAllByRole("radio").map((r) => r.getAttribute("value"))).toEqual(["1", "2", "3", "4", "5"]);
    expect(screen.queryByRole("button", { name: "Limpar" })).not.toBeInTheDocument();
  });

  it("selects with a click and clears again", async () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    await userEvent.click(screen.getByRole("radio", { name: "4" }));
    expect(screen.getByRole("radio", { name: "4" })).toBeChecked();
    expect(onChange).toHaveBeenLastCalledWith(4);

    await userEvent.click(screen.getByRole("button", { name: "Limpar" }));
    expect(screen.getByRole("radio", { name: "4" })).not.toBeChecked();
    expect(onChange).toHaveBeenLastCalledWith(undefined);
  });

  it("moves with the arrow keys", async () => {
    render(<Harness />);
    await userEvent.tab();
    await userEvent.keyboard("{ArrowRight}");
    expect(screen.getAllByRole("radio").filter((r) => (r as HTMLInputElement).checked)).toHaveLength(1);
  });
});
