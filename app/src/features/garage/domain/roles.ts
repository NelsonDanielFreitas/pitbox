import type { TeamRole } from "@/features/teams/domain/team";

export function canReadGarage(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic" || role === "pilot";
}

export function canWriteGarage(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic";
}

/** Costs are only read for owners and mechanics (ADR 0023); pilots never get them. */
export function canSeeCosts(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic";
}
