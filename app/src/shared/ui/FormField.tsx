import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { fieldErrorText } from "./form-errors";


export function FormField({
  id,
  label,
  error,
  hint,
  unit,
  children,
  className,
}: {
  id: string;
  label: string;
  error?: string | undefined;
  hint?: string | undefined;
  unit?: string | undefined;
  children: ReactNode;
  className?: string;
}) {
  const { t } = useTranslation();
  const errorText = fieldErrorText(t, error);
  return (
    <div className={cn("space-y-1.5", className)}>
      <label htmlFor={id} className="block text-sm font-semibold text-fg">
        {label}
        {unit ? <span className="ml-1 font-normal text-muted">({unit})</span> : null}
      </label>
      {children}
      {hint && !errorText ? (
        <p id={`${id}-hint`} className="text-xs text-muted">
          {hint}
        </p>
      ) : null}
      {errorText ? (
        <p id={`${id}-error`} className="text-sm font-medium text-crit">
          {errorText}
        </p>
      ) : null}
    </div>
  );
}

export function FormRootError({ error }: { error: string | undefined }) {
  const { t } = useTranslation();
  const text = fieldErrorText(t, error);
  if (!text) return null;
  return (
    <p role="alert" className="rounded-xl border border-crit/30 bg-crit/12 px-4 py-3 text-sm text-crit">
      {text}
    </p>
  );
}
