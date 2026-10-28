import { useTranslation } from "react-i18next";

import { RoleBadge } from "@/shared/ui/Badge";

import type { TeamMember } from "../../domain/team-schemas";
import { formatDate, initials } from "./format";

export function MemberRow({
  member,
  isCurrentUser,
  canManage,
  onChangeRole,
  onRemove,
}: {
  member: TeamMember;
  isCurrentUser: boolean;
  canManage: boolean;
  onChangeRole: (member: TeamMember) => void;
  onRemove: (member: TeamMember) => void;
}) {
  const { t, i18n } = useTranslation();
  const showActions = canManage && member.role !== "owner" && !isCurrentUser;

  return (
    <li className="flex min-h-16 flex-wrap items-center gap-3 rounded-2xl border border-line bg-surface p-3">
      <span aria-hidden="true" className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-surface-2 text-sm font-bold text-muted">
        {initials(member.displayName)}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-semibold text-fg">
          {member.displayName}
          {isCurrentUser ? ` · ${t("teams.members.you")}` : ""}
        </span>
        <span className="block text-xs text-muted">{t("teams.members.joined", { date: formatDate(member.joinedAt, i18n.language) })}</span>
      </span>
      <RoleBadge role={member.role} />
      {showActions ? (
        <span className="flex w-full justify-end gap-1">
          <button
            type="button"
            aria-label={t("teams.members.changeRoleFor", { name: member.displayName })}
            className="min-h-11 rounded-lg px-3 text-sm hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary"
            onClick={() => onChangeRole(member)}
          >
            {t("teams.members.changeRole")}
          </button>
          <button
            type="button"
            aria-label={t("teams.members.removeFor", { name: member.displayName })}
            className="min-h-11 rounded-lg px-3 text-sm text-crit hover:bg-crit/10 focus-visible:outline-2 focus-visible:outline-primary"
            onClick={() => onRemove(member)}
          >
            {t("teams.members.remove")}
          </button>
        </span>
      ) : null}
    </li>
  );
}
