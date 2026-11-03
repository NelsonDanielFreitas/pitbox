import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { parseTemplate, type Template, type TemplateField } from "../../domain/template";
import { applyTemplateTexts, initialTemplateTexts, type TemplateTexts } from "../../domain/template-form";
import { Angle, HoleGrid, HoleRow, Length, Part } from "./diagrams/Diagrams";
import { FieldDiagram } from "./diagrams/FieldDiagram";
import { TemplateFieldView } from "./TemplateField";
import { TemplateForm } from "./TemplateForm";

const lt = (pt: string) => ({ pt, en: pt });
const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4a01";

const raw = {
  format: 1,
  id,
  slug: "model-a",
  version: 1,
  make: "Acme",
  model: "A8",
  scale: "1/8",
  powertrain: "nitro",
  name: lt("Acme A8"),
  groups: [
    {
      key: "front",
      label: lt("Frente"),
      fields: [
        { key: "front_cst", label: lt("Diferencial"), type: "int", unit: "cSt", min: 100, max: 1000000, kit: 500 },
        { key: "tower", label: lt("Torre"), type: "choice", options: ["1", "2", "3"], diagram: { kind: "hole_row", count: 3 } },
        { key: "grid", label: lt("Posição"), type: "choice", options: ["a", "b", "c", "d"], diagram: { kind: "hole_grid", rows: 2, cols: 2 } },
        { key: "stiffener", label: lt("Reforço"), type: "flag", part_ref: "T2311", diagram: { kind: "part", shape: "brace" } },
        { key: "camber", label: lt("Camber"), type: "number", unit: "°", min: -5, max: 5, step: 0.5, diagram: { kind: "angle", axis: "camber" } },
        { key: "ride", label: lt("Altura"), type: "number", unit: "mm", min: 0, max: 20, diagram: { kind: "length", measure: "ride_height" } },
        { key: "remarks", label: lt("Notas"), type: "text", max_len: 10 },
      ],
    },
  ],
};
const parsed = parseTemplate(raw);
if (!parsed) throw new Error("fixture template does not parse");
const template: Template = parsed;
const field = (key: string): TemplateField => {
  const f = template.groups.flatMap((g) => g.fields).find((x) => x.key === key);
  if (!f) throw new Error(key);
  return f;
};

describe("drawings", () => {
  it("hole_row", () => {
    const { container } = render(<HoleRow options={["1", "2", "3"]} value="2" label="Torre" />);
    expect(container).toMatchSnapshot();
  });
  it("hole_grid", () => {
    const { container } = render(<HoleGrid rows={2} cols={2} options={["a", "b", "c", "d"]} value="c" label="Posição" />);
    expect(container).toMatchSnapshot();
  });
  it("angle", () => {
    const { container } = render(<Angle axis="camber" value={-1.5} />);
    expect(container).toMatchSnapshot();
  });
  it("length", () => {
    const { container } = render(<Length measure="ride_height" value={7} min={0} max={20} />);
    expect(container).toMatchSnapshot();
  });
  it("part", () => {
    const { container } = render(<Part shape="brace" on />);
    expect(container).toMatchSnapshot();
  });

  it("read-only drawings are images with a text alternative made from the value", () => {
    render(
      <>
        <FieldDiagram field={field("tower")} value="2" label="Torre" />
        <FieldDiagram field={field("camber")} value={-1.5} label="Camber" />
        <FieldDiagram field={field("stiffener")} value={true} label="Reforço" />
      </>,
    );
    expect(screen.getByRole("img", { name: /Fila de 3 furos\. Escolhido: 2/ })).toBeVisible();
    expect(screen.getByRole("img", { name: "Camber: -1.5°" })).toBeVisible();
    expect(screen.getByRole("img", { name: /Reforço: Sim/ })).toBeVisible();
  });

  it("tapping hole 3 of a row picks option 3", async () => {
    let picked = "";
    render(<HoleRow options={["1", "2", "3"]} value={undefined} label="Torre" onSelect={(o) => (picked = o)} />);
    await userEvent.click(screen.getByRole("button", { name: /Furo 3/ }));
    expect(picked).toBe("3");
  });

  it("tapping hole 3 of a grid picks option 3 (row-major)", async () => {
    let picked = "";
    render(<HoleGrid rows={2} cols={2} options={["a", "b", "c", "d"]} value={undefined} label="Posição" onSelect={(o) => (picked = o)} />);
    await userEvent.click(screen.getByRole("button", { name: /Furo 3/ }));
    expect(picked).toBe("c");
  });

  it("every hole is at least 44 px", () => {
    render(<HoleRow options={["1", "2", "3"]} value={undefined} label="Torre" onSelect={() => undefined} />);
    for (const button of screen.getAllByRole("button")) {
      expect(parseInt(button.style.width, 10)).toBeGreaterThanOrEqual(44);
    }
  });
});

function Harness({ initial = {} }: { initial?: TemplateTexts }) {
  const [texts, setTexts] = useState<TemplateTexts>(initial);
  const result = applyTemplateTexts(template, texts);
  return (
    <>
      <TemplateForm template={template} texts={texts} base={{}} issues={result.issues} onChange={(k, v) => setTexts((p) => ({ ...p, [k]: v }))} />
      <output data-testid="values">{JSON.stringify(result.values)}</output>
      <output data-testid="issues">{JSON.stringify(result.issues)}</output>
    </>
  );
}

describe("TemplateForm", () => {
  it("fills every kind of field", async () => {
    render(<Harness />);
    await userEvent.type(screen.getByRole("textbox", { name: /^Diferencial/ }), "600");
    await userEvent.click(within(screen.getByRole("group", { name: "Torre" })).getByRole("button", { name: /Furo 3/ }));
    await userEvent.click(within(screen.getByRole("group", { name: "Reforço" })).getByRole("button", { name: "Sim" }));
    await userEvent.type(screen.getByRole("textbox", { name: /^Camber/ }), "-1,5");
    await userEvent.type(screen.getByRole("textbox", { name: /^Notas/ }), "ok");
    expect(JSON.parse(screen.getByTestId("values").textContent ?? "{}")).toEqual({
      front_cst: 600,
      tower: "3",
      stiffener: true,
      camber: -1.5,
      remarks: "ok",
    });
    expect(screen.getByTestId("issues")).toHaveTextContent("{}");
  });

  it("shows the reason without the value, and the kit shortcut", async () => {
    render(<Harness />);
    await userEvent.type(screen.getByRole("textbox", { name: /^Diferencial/ }), "5");
    expect(await screen.findByText(/Fora do intervalo/)).toBeVisible();
    expect(screen.getByText("Kit: 500 cSt")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Igual ao kit" }));
    expect(screen.getByRole("textbox", { name: /^Diferencial/ })).toHaveValue("500");
    expect(screen.queryByText(/Fora do intervalo/)).not.toBeInTheDocument();
  });

  it("a number off its step is refused", async () => {
    render(<Harness />);
    await userEvent.type(screen.getByRole("textbox", { name: /^Camber/ }), "0,3");
    expect(await screen.findByText(/múltiplo de 0,5/)).toBeVisible();
  });
});

describe("the help sheet", () => {
  it("shows a <script> in help as literal text and never as an element", async () => {
    const hostile: TemplateField = {
      ...field("front_cst"),
      help: { pt: "<script>window.__pwned = 1</script><img src=x onerror=alert(1)>" },
    };
    const { container } = render(<TemplateFieldView field={hostile} text="" previous={undefined} issue={undefined} onChange={() => undefined} />);
    await userEvent.click(screen.getByRole("button", { name: /Ajuda: Diferencial/ }));
    expect(screen.getByText(/<script>window\.__pwned = 1<\/script>/)).toBeVisible();
    expect(document.querySelector("script")).toBeNull();
    expect(document.querySelector("img")).toBeNull();
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
    expect(container.querySelector("script")).toBeNull();
  });

  it("opens with the drawing of the current value and closes", async () => {
    render(<TemplateFieldView field={field("camber")} text="-1,5" previous={undefined} issue={undefined} onChange={() => undefined} />);
    await userEvent.click(screen.getByRole("button", { name: /Ajuda: Camber/ }));
    expect(screen.getAllByRole("img", { name: "Camber: -1.5°" }).length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("button", { name: "Fechar" }));
  });
});

describe("initial texts", () => {
  it("shows stored values the way they are typed", () => {
    expect(initialTemplateTexts({ camber: -1.5, stiffener: true, tower: "3" })).toEqual({ camber: "-1,5", stiffener: "true", tower: "3" });
  });
});
