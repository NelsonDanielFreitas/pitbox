export type TeamRole = "owner" | "mechanic" | "pilot" | "guest" | "unknown";

export interface Team {
  id: string;
  name: string;
  category: string;
  club?: string | undefined;
  countryCode: string;
  role: TeamRole;
}

export function teamInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  return words
    .slice(0, 2)
    .map((word) => word[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}
