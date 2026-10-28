import { createFileRoute, Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/shared/ui/PageHeader";
import { useTeamContext } from "@/features/teams/ui/team-context";

export const Route = createFileRoute("/_authenticated/teams_/$teamId/more/")({
  component: MoreIndexRoute,
});

function MoreIndexRoute() {
  const { t } = useTranslation();
  const team = useTeamContext();

  return (
    <main className="space-y-4">
      <PageHeader title={t("teams.moreTitle")} subtitle={t("teams.moreSubtitle")} />
      <Link
        to="/teams/$teamId/more/team"
        params={{ teamId: team.id }}
        className="flex min-h-16 items-center gap-3 rounded-2xl border border-line bg-surface p-4 transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      >
        <span className="flex size-11 items-center justify-center rounded-xl bg-surface-2 text-muted" aria-hidden="true">
          <svg viewBox="0 0 24 24" className="size-5" fill="none">
            <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM20 8v6M23 11h-6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
        <span className="min-w-0 flex-1">
          <span className="block font-semibold text-fg">{t("teams.members.title")}</span>
          <span className="block text-sm text-muted">{t("teams.members.subtitle")}</span>
        </span>
        <span aria-hidden="true" className="text-muted">›</span>
      </Link>
    </main>
  );
}
