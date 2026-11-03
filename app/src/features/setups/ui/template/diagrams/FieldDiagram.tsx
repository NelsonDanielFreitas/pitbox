import type { TemplateField } from "../../../domain/template";

import { Angle, HoleGrid, HoleRow, Length, Part } from "./Diagrams";

/**
 * The drawing of a field's current value. `onSelect` makes the hole kinds the selector itself;
 * the other kinds only show the value.
 */
export function FieldDiagram({
  field,
  value,
  label,
  onSelect,
}: {
  field: TemplateField;
  value: unknown;
  label: string;
  onSelect?: (option: string) => void;
}) {
  const d = field.diagram;
  if (!d) return null;
  const options = field.options ?? [];
  const text = typeof value === "string" ? value : undefined;
  const num = typeof value === "number" ? value : undefined;
  switch (d.kind) {
    case "hole_row":
      return <HoleRow options={options} value={text} label={label} orientation={d.orientation} {...(onSelect ? { onSelect } : {})} />;
    case "hole_grid":
      return <HoleGrid options={options} value={text} label={label} rows={d.rows ?? 1} cols={d.cols ?? options.length} {...(onSelect ? { onSelect } : {})} />;
    case "angle":
      return d.axis ? <Angle axis={d.axis} value={num} /> : null;
    case "length":
      return d.measure ? <Length measure={d.measure} value={num} min={field.min ?? 0} max={field.max ?? 1} /> : null;
    case "part":
      return d.shape ? <Part shape={d.shape} on={typeof value === "boolean" ? value : undefined} /> : null;
  }
}
