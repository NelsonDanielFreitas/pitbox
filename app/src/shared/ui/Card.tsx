import type { ComponentPropsWithoutRef } from "react";

import { cn } from "@/shared/lib/cn";

export function Card({ className, ...props }: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      className={cn(
        "rounded-2xl border border-pitbox-line/80 bg-pitbox-card p-5 shadow-sm",
        className,
      )}
      {...props}
    />
  );
}
