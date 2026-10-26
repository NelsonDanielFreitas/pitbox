import { forwardRef, type ComponentPropsWithoutRef } from "react";

import { cn } from "@/shared/lib/cn";

type Props = ComponentPropsWithoutRef<"select"> & {
  invalid?: boolean;
};

export const SelectInput = forwardRef<HTMLSelectElement, Props>(
  ({ className, invalid, ...props }, ref) => (
    <select
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(
        "min-h-12 w-full rounded-xl border border-line bg-surface px-3 text-base text-fg outline-none transition-colors focus:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
        invalid && "border-crit",
        className,
      )}
      {...props}
    />
  ),
);
SelectInput.displayName = "SelectInput";
