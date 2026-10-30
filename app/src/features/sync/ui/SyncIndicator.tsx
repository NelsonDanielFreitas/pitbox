import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

import { useSyncStatus } from "../data/use-sync";

type Tone = "ok" | "warn" | "crit" | "info";

const dot: Record<Tone, string> = {
  ok: "bg-ok",
  warn: "bg-warn",
  crit: "bg-crit",
  info: "bg-info",
};

/** Always-visible sync state: synced / N waiting / offline / needs attention. */
export function SyncIndicator() {
  const { t } = useTranslation();
  const status = useSyncStatus();
  const params = useParams({ strict: false });
  const teamId = typeof params.teamId === "string" ? params.teamId : undefined;

  let tone: Tone = "ok";
  let label: string = t("sync.status.synced");
  if (status.conflicts > 0 || status.failed > 0 || status.phase === "blocked" || status.phase === "auth") {
    tone = "crit";
    label = status.phase === "auth" ? t("sync.status.signIn") : t("sync.status.attention");
  } else if (status.phase === "error") {
    tone = "warn";
    label = status.problem === "service" ? t("sync.status.service") : t("sync.status.error");
  } else if (status.phase === "offline") {
    tone = "warn";
    label = status.pending > 0 ? t("sync.status.offlinePending", { count: status.pending }) : t("sync.status.offline");
  } else if (status.pending > 0) {
    tone = "info";
    label = t("sync.status.pending", { count: status.pending });
  } else if (status.phase === "syncing") {
    tone = "info";
    label = t("sync.status.syncing");
  }

  const content = (
    <>
      <span aria-hidden="true" className={cn("size-2.5 rounded-full", dot[tone])} />
      <span className="hidden text-sm sm:inline">{label}</span>
    </>
  );
  const className =
    "flex min-h-12 items-center gap-2 rounded-lg px-2 text-muted outline-none hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary";

  // role=status: a screen reader hears the change without being interrupted.
  return teamId ? (
    <Link to="/teams/$teamId/sync" params={{ teamId }} className={className} aria-label={label} role="status">
      {content}
    </Link>
  ) : (
    <span className={className} aria-label={label} role="status">
      {content}
    </span>
  );
}
