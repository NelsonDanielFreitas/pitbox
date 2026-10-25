import { useTranslation } from "react-i18next";

import type { Team, TeamRole } from "../domain/team";
import { Card } from "@/shared/ui/Card";

function roleLabel(role: TeamRole, t: (key: `roles.${TeamRole}`) => string): string {
  return t(`roles.${role}`);
}

export function TeamCard({ team }: { team: Team }) {
  const { t } = useTranslation();

  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-pitbox-ink">{team.name}</h2>
          <p className="mt-1 text-sm text-pitbox-muted">
            {team.category}
            {team.club ? ` · ${team.club}` : ""}
          </p>
        </div>
        <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-semibold text-pitbox-muted">
          {roleLabel(team.role, t)}
        </span>
      </div>
    </Card>
  );
}
