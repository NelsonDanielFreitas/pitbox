import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

export type BadgeTone = "neutral" | "ok" | "warn" | "crit" | "info" | "primary";
export type RoleBadgeRole = "owner" | "mechanic" | "pilot" | "guest" | "unknown";

const toneClasses: Record<BadgeTone, string> = {
  neutral: "border-line/30 bg-surface-2 text-muted",
  ok: "border-ok/30 bg-ok/12 text-ok",
  warn: "border-warn/30 bg-warn/12 text-warn",
  crit: "border-crit/30 bg-crit/12 text-crit",
  info: "border-info/30 bg-info/12 text-info",
  primary: "border-primary/30 bg-primary/12 text-primary",
};

const roleTones: Record<RoleBadgeRole, BadgeTone> = {
  owner: "primary",
  mechanic: "info",
  pilot: "ok",
  guest: "neutral",
  unknown: "neutral",
};

export function Badge({
  tone = "neutral",
  children,
  className,
}: {
  tone?: BadgeTone;
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center rounded-md border px-2 text-xs font-semibold",
        toneClasses[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function RoleBadge({ role }: { role: RoleBadgeRole }) {
  const { t } = useTranslation();
  return <Badge tone={roleTones[role]}>{t(`roles.${role}`)}</Badge>;
}
