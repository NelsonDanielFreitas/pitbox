import type { TeamRole } from "@/features/teams/domain/team";

export function canReadGarage(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic" || role === "pilot";
}

export function canWriteGarage(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic";
}
