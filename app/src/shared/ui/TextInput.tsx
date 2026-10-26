import { forwardRef, type ComponentPropsWithoutRef } from "react";

import { cn } from "@/shared/lib/cn";

const inputClass =
  "min-h-12 w-full rounded-xl border border-line bg-surface px-3 text-base text-fg outline-none transition-colors placeholder:text-muted focus:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:opacity-60";

type Props = ComponentPropsWithoutRef<"input"> & {
  invalid?: boolean;
};

export const TextInput = forwardRef<HTMLInputElement, Props>(
  ({ className, invalid, ...props }, ref) => (
    <input
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(inputClass, invalid && "border-crit", className)}
      {...props}
    />
  ),
);
TextInput.displayName = "TextInput";
