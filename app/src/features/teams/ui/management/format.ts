import type { TeamRole } from "../../domain/team";
import type { AssignableTeamRole } from "../../domain/team-schemas";

export const assignableRoles: AssignableTeamRole[] = ["mechanic", "pilot", "guest"];

export const roleOrder: Record<TeamRole, number> = { owner: 0, mechanic: 1, pilot: 2, guest: 3, unknown: 4 };

export function initials(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toUpperCase();
}

export function assignableRole(role: TeamRole): AssignableTeamRole {
  return role === "mechanic" || role === "pilot" || role === "guest" ? role : "pilot";
}

export function formatDate(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
