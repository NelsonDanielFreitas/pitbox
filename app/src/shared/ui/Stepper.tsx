import { useState } from "react";

import { cn } from "@/shared/lib/cn";

const MAX_CENTI = 9_999_999_999;

const button =
  "grid size-14 shrink-0 place-items-center rounded-2xl border border-line bg-surface-2 text-2xl font-bold text-fg outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary active:bg-line disabled:opacity-50";

/**
 * Quantity input with big − and + buttons for use with one hand at the box. The value is in
 * hundredths (like the stock); `null` means "not counted yet". Typed text is cleaned as the
 * user types so only digits and, when allowed, up to two decimals get through.
 */
export function Stepper({
  value,
  onChange,
  wholeOnly,
  inputLabel,
  decreaseLabel,
  increaseLabel,
  className,
}: {
  value: number | null;
  onChange: (next: number | null) => void;
  wholeOnly: boolean;
  inputLabel: string;
  decreaseLabel: string;
  increaseLabel: string;
  className?: string;
}) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? (value === null ? "" : format(value, wholeOnly));

  const step = (delta: number) => {
    setText(null);
    onChange(Math.min(MAX_CENTI, Math.max(0, (value ?? 0) + delta)));
  };

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <button type="button" aria-label={decreaseLabel} className={button} onClick={() => step(-100)}>
        <span aria-hidden="true">−</span>
      </button>
      <input
        aria-label={inputLabel}
        value={shown}
        placeholder="—"
        inputMode={wholeOnly ? "numeric" : "decimal"}
        autoComplete="off"
        onChange={(event) => {
          const cleaned = clean(event.target.value, wholeOnly);
          setText(cleaned);
          const parsed = cleaned === "" ? null : toCenti(cleaned);
          onChange(parsed === null || parsed > MAX_CENTI ? null : parsed);
        }}
        onBlur={() => setText(null)}
        className="h-14 w-20 rounded-2xl border border-line bg-surface text-center text-2xl font-bold tabular-nums text-fg outline-none focus:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      />
      <button type="button" aria-label={increaseLabel} className={button} onClick={() => step(100)}>
        <span aria-hidden="true">+</span>
      </button>
    </div>
  );
}

function clean(raw: string, wholeOnly: boolean): string {
  if (wholeOnly) return raw.replace(/\D/g, "").slice(0, 10);
  const swapped = raw.replace(".", ",").replace(/[^\d,]/g, "");
  const [whole = "", ...rest] = swapped.split(",");
  const decimals = rest.join("").slice(0, 2);
  return (rest.length > 0 ? `${whole.slice(0, 10)},${decimals}` : whole.slice(0, 10));
}

function toCenti(text: string): number | null {
  const value = Number(text.replace(",", "."));
  return Number.isFinite(value) ? Math.round(value * 100) : null;
}

function format(centi: number, wholeOnly: boolean): string {
  if (wholeOnly) return String(Math.trunc(centi / 100));
  return (centi / 100).toFixed(2).replace(/\.?0+$/, "").replace(".", ",");
}
