import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { teamInitials, type Team } from "../domain/team";

export function TeamTopBar({ team }: { team: Team }) {
  const { t } = useTranslation();

  return (
    <Link
      to="/teams"
      activeOptions={{ exact: true }}
      className="flex min-h-12 w-full rounded-2xl border border-line bg-surface p-3 text-left transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
    >
      <span className="flex w-full items-center gap-3">
        <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-primary text-sm font-bold text-on-primary">
          {teamInitials(team.name)}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-semibold text-fg">{team.name}</span>
          <span className="block truncate text-xs text-muted">
            {team.category}
            {team.club ? ` · ${team.club}` : ""}
          </span>
        </span>
        <svg className="size-5 shrink-0 text-muted" viewBox="0 0 24 24" fill="none">
          <path d="m6 9 6 6 6-6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span className="sr-only">, {t("teams.changeTeamHint")}</span>
      </span>
    </Link>
  );
}
