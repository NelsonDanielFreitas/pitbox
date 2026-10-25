import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentPropsWithoutRef } from "react";

import { cn } from "@/shared/lib/cn";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 font-semibold transition-colors disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary: "bg-primary text-on-primary hover:bg-primary-strong",
        secondary: "border border-line bg-surface-2 text-fg hover:border-muted",
        ghost: "text-fg hover:bg-surface-2",
        danger: "bg-crit text-white",
      },
      size: {
        md: "h-12 rounded-xl px-4 text-[15px]",
        lg: "h-14 rounded-xl px-5 text-base",
      },
    },
    defaultVariants: { variant: "primary", size: "md" },
  },
);

type ButtonProps = ComponentPropsWithoutRef<"button"> &
  VariantProps<typeof buttonVariants> & { asChild?: boolean; full?: boolean };

export function Button({
  className,
  variant,
  size,
  full = false,
  asChild = false,
  type = "button",
  ...props
}: ButtonProps) {
  const Component = asChild ? Slot : "button";
  return (
    <Component
      className={cn(
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
        buttonVariants({ variant, size }),
        full && "w-full",
        className,
      )}
      type={asChild ? undefined : type}
      {...props}
    />
  );
}
