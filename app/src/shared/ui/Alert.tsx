import type { ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

export type AlertTone = "ok" | "warn" | "crit" | "info";

const toneClasses: Record<AlertTone, string> = {
  ok: "border-ok/30 bg-ok/12 text-ok",
  warn: "border-warn/30 bg-warn/12 text-warn",
  crit: "border-crit/30 bg-crit/12 text-crit",
  info: "border-info/30 bg-info/12 text-info",
};

export function Alert({
  tone = "info",
  title,
  children,
  className,
}: {
  tone?: AlertTone;
  title?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      role={tone === "crit" ? "alert" : "status"}
      className={cn("rounded-xl border px-4 py-3 text-sm", toneClasses[tone], className)}
    >
      {title ? <h2 className="font-semibold">{title}</h2> : null}
      <div className={cn(title && "mt-1")}>{children}</div>
    </div>
  );
}
