import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

// Our own drawings for the closed set of diagram kinds (ADR 0030). Plain SVG from numeric and
// enum parameters only: nothing from the template is ever put in the markup as HTML, a URL or
// an image. The same components draw the help sheet (read-only) and the field selector.

const HOLE = 48; // touch target and drawing cell, in px (>= 44 required)

function Hole({ selected }: { selected: boolean }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="size-7">
      <circle cx="12" cy="12" r="9" fill={selected ? "currentColor" : "none"} stroke="currentColor" strokeWidth="2" />
    </svg>
  );
}

interface HolesProps {
  options: readonly string[];
  value: string | undefined;
  label: string;
  /** With a handler every hole is a button; without it the drawing is read-only. */
  onSelect?: (option: string) => void;
}

function HoleButtons({ options, value, onSelect }: Pick<HolesProps, "options" | "value" | "onSelect">) {
  const { t } = useTranslation();
  return options.map((option, index) => {
    const selected = option === value;
    const text = t("setups.template.hole", { n: index + 1 });
    if (!onSelect) {
      return (
        <span key={option} className={cn("flex items-center justify-center", selected ? "text-primary" : "text-muted")} style={{ width: HOLE, height: HOLE }}>
          <Hole selected={selected} />
        </span>
      );
    }
    return (
      <button
        key={option}
        type="button"
        aria-pressed={selected}
        aria-label={`${text}: ${option}`}
        onClick={() => onSelect(option)}
        style={{ width: HOLE, height: HOLE }}
        className={cn(
          "flex flex-col items-center justify-center rounded-xl border text-xs font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
          selected ? "border-primary bg-primary/12 text-primary" : "border-line bg-surface text-muted",
        )}
      >
        <Hole selected={selected} />
        <span aria-hidden="true">{option}</span>
      </button>
    );
  });
}

export function HoleRow({
  orientation = "horizontal",
  ...props
}: HolesProps & { orientation?: "horizontal" | "vertical" | undefined }) {
  const { t } = useTranslation();
  const chosen = props.value ?? t("setups.template.notChosen");
  const label = t("setups.template.holeRow", { count: props.options.length, value: chosen });
  const layout = cn("inline-flex gap-1", orientation === "vertical" ? "flex-col" : "flex-row");
  return props.onSelect ? (
    <div role="group" aria-label={props.label} className={layout}>
      <HoleButtons {...props} />
    </div>
  ) : (
    <div role="img" aria-label={label} className={layout}>
      <HoleButtons {...props} />
    </div>
  );
}

export function HoleGrid({ rows, cols, ...props }: HolesProps & { rows: number; cols: number }) {
  const { t } = useTranslation();
  const chosen = props.value ?? t("setups.template.notChosen");
  const label = t("setups.template.holeGrid", { rows, cols, value: chosen });
  const style = { gridTemplateColumns: `repeat(${cols}, ${HOLE}px)` };
  return props.onSelect ? (
    <div role="group" aria-label={props.label} className="inline-grid gap-1" style={style}>
      <HoleButtons {...props} />
    </div>
  ) : (
    <div role="img" aria-label={label} className="inline-grid gap-1" style={style}>
      <HoleButtons {...props} />
    </div>
  );
}

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n));

/** A wheel/arm seen from the axis the angle is measured on, rotated by the value. */
export function Angle({ axis, value }: { axis: "camber" | "caster" | "toe" | "kickup" | "anti_squat"; value: number | undefined }) {
  const { t } = useTranslation();
  const degrees = clamp(value ?? 0, -45, 45);
  const label = t("setups.template.angle", {
    axis: t(`setups.template.axes.${axis}`),
    value: value === undefined ? t("setups.template.noValue") : `${value}°`,
  });
  return (
    <svg role="img" aria-label={label} viewBox="0 0 80 80" className="size-20 text-primary" data-axis={axis}>
      <line x1="40" y1="8" x2="40" y2="72" stroke="currentColor" strokeWidth="1" strokeDasharray="3 3" opacity="0.5" />
      <g transform={`rotate(${degrees} 40 40)`}>
        <rect x="34" y="12" width="12" height="56" rx="3" fill="none" stroke="currentColor" strokeWidth="3" />
      </g>
    </svg>
  );
}

/** A bar whose length follows the value inside the field's range. */
export function Length({
  measure,
  value,
  min,
  max,
}: {
  measure: "ride_height" | "droop" | "shock_length" | "limiter";
  value: number | undefined;
  min: number;
  max: number;
}) {
  const { t } = useTranslation();
  const span = max - min || 1;
  const fraction = value === undefined ? 0 : clamp((value - min) / span, 0, 1);
  const label = t("setups.template.length", {
    measure: t(`setups.template.measures.${measure}`),
    value: value === undefined ? t("setups.template.noValue") : String(value),
  });
  const height = 8 + fraction * 56;
  return (
    <svg role="img" aria-label={label} viewBox="0 0 80 80" className="size-20 text-primary" data-measure={measure}>
      <line x1="16" y1="72" x2="64" y2="72" stroke="currentColor" strokeWidth="2" />
      <rect x="34" y={72 - height} width="12" height={height} rx="2" fill="currentColor" opacity="0.8" />
    </svg>
  );
}

/** An optional part, drawn in outline, or filled when it is fitted. */
export function Part({ shape, on }: { shape: "disk" | "brace" | "spacer" | "weight" | "other"; on: boolean | undefined }) {
  const { t } = useTranslation();
  const label = t("setups.template.part", {
    shape: t(`setups.template.shapes.${shape}`),
    value: on === undefined ? t("setups.template.noValue") : on ? t("setups.template.flagOn") : t("setups.template.flagOff"),
  });
  const fill = on ? "currentColor" : "none";
  return (
    <svg role="img" aria-label={label} viewBox="0 0 80 80" className="size-20 text-primary" data-shape={shape}>
      {shape === "disk" ? <circle cx="40" cy="40" r="24" fill={fill} fillOpacity="0.3" stroke="currentColor" strokeWidth="3" /> : null}
      {shape === "brace" ? <path d="M12 60 L68 20" fill="none" stroke="currentColor" strokeWidth="6" strokeLinecap="round" opacity={on ? 1 : 0.5} /> : null}
      {shape === "spacer" ? <rect x="24" y="30" width="32" height="20" rx="3" fill={fill} fillOpacity="0.3" stroke="currentColor" strokeWidth="3" /> : null}
      {shape === "weight" ? <path d="M24 60 L32 24 H48 L56 60 Z" fill={fill} fillOpacity="0.3" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" /> : null}
      {shape === "other" ? <rect x="20" y="20" width="40" height="40" rx="8" fill={fill} fillOpacity="0.3" stroke="currentColor" strokeWidth="3" /> : null}
    </svg>
  );
}
