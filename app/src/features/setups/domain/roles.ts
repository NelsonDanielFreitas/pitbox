import type { TeamRole } from "@/features/teams/domain/team";

export function canReadSetups(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic" || role === "pilot";
}

/** Pilots read setups but never change them (the server enforces this too). */
export function canWriteSetups(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic";
}

/** Feedback is the one thing a pilot writes. */
export function canGiveFeedback(role: TeamRole): boolean {
  return role === "owner" || role === "mechanic" || role === "pilot";
}
