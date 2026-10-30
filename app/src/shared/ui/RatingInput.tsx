import { useId } from "react";

import { cn } from "@/shared/lib/cn";

/**
 * A 1–5 scale as a group of radio buttons: keyboard arrows move between values, every target
 * is at least 44 px, and the chosen value is a filled button with the number (not only colour).
 * The answer is optional, so a value can be cleared.
 */
export function RatingInput({
  legend,
  value,
  onChange,
  clearLabel,
  max = 5,
}: {
  legend: string;
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  clearLabel: string;
  max?: number;
}) {
  const name = useId();
  return (
    <fieldset className="min-w-0">
      <legend className="mb-1.5 text-sm font-semibold text-fg">{legend}</legend>
      <div className="flex items-center gap-2">
        <div className="flex flex-1 gap-2">
          {Array.from({ length: max }, (_, index) => index + 1).map((n) => (
            <label key={n} className="flex-1">
              <input
                type="radio"
                name={name}
                value={n}
                checked={value === n}
                onChange={() => onChange(n)}
                className="peer sr-only"
              />
              <span
                className={cn(
                  "flex min-h-11 min-w-11 cursor-pointer items-center justify-center rounded-xl border border-line bg-surface-2 text-base font-semibold text-fg transition-colors",
                  "peer-checked:border-primary peer-checked:bg-primary peer-checked:text-on-primary",
                  "peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary",
                )}
              >
                {n}
              </span>
            </label>
          ))}
        </div>
        {value !== undefined ? (
          <button
            type="button"
            onClick={() => onChange(undefined)}
            className="min-h-11 shrink-0 rounded-xl px-3 text-sm font-semibold text-muted underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            {clearLabel}
          </button>
        ) : null}
      </div>
    </fieldset>
  );
}
