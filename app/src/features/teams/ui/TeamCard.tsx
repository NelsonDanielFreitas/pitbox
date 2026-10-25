import { Link } from "@tanstack/react-router";
import { RoleBadge } from "@/shared/ui/Badge";

import { teamInitials, type Team } from "../domain/team";

export function TeamCard({ team }: { team: Team }) {
  const details = `${team.category}${team.club ? ` · ${team.club}` : ""}`;

  return (
    <Link
      to="/teams/$teamId"
      params={{ teamId: team.id }}
      className="flex min-h-16 rounded-2xl border border-line bg-surface p-3.5 transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
    >
      <span className="flex w-full items-center gap-3">
        <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-surface-2 text-sm font-bold text-muted">
          {teamInitials(team.name)}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-semibold text-fg">{team.name}</span>
          <span className="block truncate text-xs text-muted">{details}</span>
        </span>
        <RoleBadge role={team.role} />
        <svg className="size-5 shrink-0 text-muted" viewBox="0 0 24 24" fill="none">
          <path d="m9 6 6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </span>
    </Link>
  );
}
