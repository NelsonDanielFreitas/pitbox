import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import type { VersionView } from "../../domain/compare";
import { parseTemplate, type Template } from "../../domain/template";
import { applyTemplateTexts, initialTemplateTexts, type TemplateTexts } from "../../domain/template-form";
import { TemplateForm } from "./TemplateForm";
import { TemplateValues } from "./TemplateValues";
import { shownValue } from "./text";

const lt = (pt: string) => ({ pt, en: pt });
const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a09";

function build(groups: unknown[]): Template {
  const t = parseTemplate({
    format: 1, id, slug: "multi-table", version: 1, make: "Acme", model: "M8", scale: "1/8", powertrain: "nitro", name: lt("Acme M8"), groups,
  });
  if (!t) throw new Error("fixture template does not parse");
  return t;
}

const parts = {
  key: "parts",
  label: lt("Peças"),
  fields: [
    { key: "kingpin", label: lt("Kingpin"), type: "multi", options: ["E0160", "E0159", "E2218", "E2146"], kit: ["E0159", "E0160"] },
  ],
};
const shocks = {
  key: "shocks",
  label: lt("Amortecedores"),
  table: {
    columns: [lt("Frente"), lt("Trás")],
    rows: [
      { label: lt("Óleo"), cells: ["oil_front", "oil_rear"] },
      { label: lt("Mola"), cells: ["spring_front", ""] },
    ],
  },
  fields: [
    { key: "oil_front", label: lt("Óleo frente"), type: "int", unit: "cSt", min: 100, max: 2000 },
    { key: "oil_rear", label: lt("Óleo trás"), type: "int", unit: "cSt", min: 100, max: 2000 },
    { key: "spring_front", label: lt("Mola frente"), type: "number", min: 0, max: 20 },
  ],
};
const template = build([parts, shocks]);

function Harness({ tpl = template, initial = {}, base = {} }: { tpl?: Template; initial?: TemplateTexts; base?: Record<string, unknown> }) {
  const [texts, setTexts] = useState<TemplateTexts>(initial);
  const result = applyTemplateTexts(tpl, texts);
  return (
    <>
      <TemplateForm template={tpl} texts={texts} base={base} issues={result.issues} onChange={(k, v) => setTexts((p) => ({ ...p, [k]: v }))} />
      <output data-testid="values">{JSON.stringify(result.values)}</output>
    </>
  );
}
const values = (): unknown => JSON.parse(screen.getByTestId("values").textContent ?? "{}");

describe("multi field", () => {
  it("ticks and unticks parts, always stored in the order of the options", async () => {
    render(<Harness />);
    const group = screen.getByRole("group", { name: "Kingpin" });
    expect(screen.getByText("Nenhuma marcada")).toBeVisible();
    await userEvent.click(within(group).getByRole("button", { name: /E2146/ }));
    await userEvent.click(within(group).getByRole("button", { name: /E0160/ }));
    expect(values()).toEqual({ kingpin: ["E0160", "E2146"] });
    expect(within(group).getByRole("button", { name: /E0160/ })).toHaveAttribute("aria-pressed", "true");
    expect(within(group).getByRole("button", { name: /E2218/ })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByText("2 marcadas")).toBeVisible();
    await userEvent.click(within(group).getByRole("button", { name: /E2146/ }));
    expect(screen.getByText("1 marcada")).toBeVisible();
    await userEvent.click(within(group).getByRole("button", { name: /E0160/ }));
    expect(values()).toEqual({}); // nothing ticked is not stored
  });

  it("clears the whole selection in one tap", async () => {
    render(<Harness initial={{ kingpin: JSON.stringify(["E0160", "E2218"]) }} />);
    await userEvent.click(screen.getByRole("button", { name: "Limpar seleção" }));
    expect(values()).toEqual({});
    expect(screen.queryByRole("button", { name: "Limpar seleção" })).not.toBeInTheDocument();
  });

  it("offers the kit and recognises the same ticks in another order", async () => {
    render(<Harness initial={{ kingpin: JSON.stringify(["E0160", "E0159"]) }} />);
    // The kit lists them as [E0159, E0160]; shown in the order of the options, and equal to the ticks.
    expect(screen.getByText("Kit: E0160, E0159")).toBeVisible();
    expect(screen.queryByText("Difere do kit")).not.toBeInTheDocument();
    await userEvent.click(within(screen.getByRole("group", { name: "Kingpin" })).getByRole("button", { name: /E2218/ }));
    expect(screen.getByText("Difere do kit")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Igual ao kit" }));
    expect(values()).toEqual({ kingpin: ["E0160", "E0159"] });
  });

  it("every option is a tap target of at least 48 px high and says it is ticked without colour", () => {
    render(<Harness initial={{ kingpin: JSON.stringify(["E0160"]) }} />);
    const on = within(screen.getByRole("group", { name: "Kingpin" })).getByRole("button", { name: /E0160/ });
    expect(on.className).toContain("min-h-12");
    expect(on).toHaveTextContent("✓");
  });

  it("is shown as a list in the read-only sheet and in a difference", () => {
    const view: VersionView = { template, values: { kingpin: ["E0160", "E2146"], oil_front: 750 }, refs: {} };
    render(<TemplateValues view={view} names={new Map()} />);
    expect(screen.getByText("E0160, E2146")).toBeVisible();
    expect(shownValue(template.groups[0]?.fields[0] ?? ({} as never), ["E0160", "E2146"], "Sim", "Não")).toBe("E0160, E2146");
  });

  it("counts a change in a multi field once, whatever the order of the stored list", () => {
    expect(initialTemplateTexts({ kingpin: ["E0160", "E2146"] })).toEqual({ kingpin: JSON.stringify(["E0160", "E2146"]) });
  });
});

describe("table layout", () => {
  it("shows one row per line of the sheet, with the column above each cell, and fills them", async () => {
    render(<Harness />);
    const oil = screen.getByRole("group", { name: "Óleo" });
    expect(within(oil).getAllByText("Frente", { exact: false }).length).toBeGreaterThan(0);
    await userEvent.type(within(oil).getByRole("textbox", { name: /Óleo frente/ }), "750");
    await userEvent.type(within(oil).getByRole("textbox", { name: /Óleo trás/ }), "600");
    await userEvent.type(within(screen.getByRole("group", { name: "Mola" })).getByRole("textbox", { name: /Mola frente/ }), "9,25");
    expect(values()).toEqual({ oil_front: 750, oil_rear: 600, spring_front: 9.25 });
  });

  it("an empty cell shows nothing and the unit stays visible in the cell", () => {
    render(<Harness />);
    const spring = screen.getByRole("group", { name: "Mola" });
    expect(within(spring).getAllByRole("textbox")).toHaveLength(1);
    expect(within(screen.getByRole("group", { name: "Óleo" })).getAllByText("cSt")).toHaveLength(2);
  });

  it("keeps a section with a problem open so that the message is seen", () => {
    const flags = longSheet().groups;
    const withNumber = build([
      ...flags,
      { key: "sizes", label: lt("Medidas"), fields: [{ key: "size", label: lt("Tamanho"), type: "int", unit: "mm", min: 1, max: 5 }] },
    ]);
    render(<Harness tpl={withNumber} initial={{ size: "99" }} />);
    expect(screen.getByRole("button", { name: /^Medidas \d/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText(/Fora do intervalo/)).toBeVisible();
    expect(screen.getByRole("button", { name: /^Secção 3 \d/ })).toHaveAttribute("aria-expanded", "false");
  });
});

/** 8 sections of 10 flags: a sheet as long as a real one. */
function longSheet(): Template {
  return build(
    Array.from({ length: 8 }, (_, g) => ({
      key: `s_${String(g).padStart(2, "0")}`,
      label: lt(`Secção ${g + 1}`),
      fields: Array.from({ length: 10 }, (_, f) => ({
        key: `f_${String(g).padStart(2, "0")}_${String(f).padStart(2, "0")}`,
        label: lt(`Campo ${g + 1}.${f + 1}`),
        type: "flag",
      })),
    })),
  );
}

describe("a long sheet", () => {
  it("opens with the first section only, and the others open on demand", async () => {
    render(<Harness tpl={longSheet()} />);
    expect(screen.getByRole("button", { name: /^Secção 1 \d/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: /^Secção 2 \d/ })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Campo 2.1")).not.toBeInTheDocument(); // closed sections render nothing
    await userEvent.click(screen.getByRole("button", { name: /^Secção 2 \d/ }));
    expect(screen.getByText("Campo 2.1")).toBeVisible();
  });

  it("the shortcuts open a section, and 'Abrir tudo' / 'Fechar tudo' toggle every one", async () => {
    render(<Harness tpl={longSheet()} />);
    const nav = screen.getByRole("navigation", { name: "Secções da ficha" });
    await userEvent.click(within(nav).getByRole("button", { name: "Secção 5" }));
    expect(screen.getByText("Campo 5.1")).toBeVisible();
    await userEvent.click(within(nav).getByRole("button", { name: "Abrir tudo" }));
    expect(screen.getByText("Campo 8.1")).toBeVisible();
    await userEvent.click(within(nav).getByRole("button", { name: "Fechar tudo" }));
    expect(screen.queryByText("Campo 1.1")).not.toBeInTheDocument();
  });

  it("each section says how much of it is filled", () => {
    render(<Harness tpl={longSheet()} initial={{ f_00_00: "true", f_00_01: "false" }} />);
    expect(screen.getByRole("button", { name: /^Secção 1 2 de 10 preenchidos/ })).toBeVisible();
    expect(screen.getByRole("button", { name: /^Secção 2 0 de 10 preenchidos/ })).toBeVisible();
  });

  it("a short sheet opens whole and has no shortcuts", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: /Peças/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: /Amortecedores/ })).toHaveAttribute("aria-expanded", "true");
    expect(screen.queryByRole("navigation", { name: "Secções da ficha" })).not.toBeInTheDocument();
  });
});
