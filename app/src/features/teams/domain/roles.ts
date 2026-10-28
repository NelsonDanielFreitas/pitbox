import type { TeamRole } from "./team";
import type { AssignableTeamRole } from "./team-schemas";

export function canViewTeamSettings(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic" || role === "pilot";
}

export function canManageTeam(role: TeamRole): boolean {
  return role === "owner";
}

export function hasStrongFactor(amr: readonly string[]): boolean {
  return amr.some((factor) => factor === "user" || factor === "otp");
}

export type TeamRoleTranslationKey =
  | "roles.owner"
  | "roles.mechanic"
  | "roles.pilot"
  | "roles.guest"
  | "roles.unknown";

export function teamRoleTranslationKey(role: TeamRole): TeamRoleTranslationKey {
  switch (role) {
    case "owner":
      return "roles.owner";
    case "mechanic":
      return "roles.mechanic";
    case "pilot":
      return "roles.pilot";
    case "guest":
      return "roles.guest";
    case "unknown":
      return "roles.unknown";
  }
}

export function assignableRoleInfoTranslationKey(
  role: AssignableTeamRole,
): "teams.invite.roleInfo.mechanic" | "teams.invite.roleInfo.pilot" | "teams.invite.roleInfo.guest" {
  switch (role) {
    case "mechanic":
      return "teams.invite.roleInfo.mechanic";
    case "pilot":
      return "teams.invite.roleInfo.pilot";
    case "guest":
      return "teams.invite.roleInfo.guest";
  }
}
