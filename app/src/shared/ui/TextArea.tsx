import { forwardRef, type ComponentPropsWithoutRef } from "react";

import { cn } from "@/shared/lib/cn";

type Props = ComponentPropsWithoutRef<"textarea"> & {
  invalid?: boolean;
};

export const TextArea = forwardRef<HTMLTextAreaElement, Props>(
  ({ className, invalid, ...props }, ref) => (
    <textarea
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(
        "min-h-28 w-full rounded-xl border border-line bg-surface px-3 py-3 text-base text-fg outline-none transition-colors placeholder:text-muted focus:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
        invalid && "border-crit",
        className,
      )}
      {...props}
    />
  ),
);
TextArea.displayName = "TextArea";
