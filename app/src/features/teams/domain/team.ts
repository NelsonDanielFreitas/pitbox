export type TeamRole = "owner" | "mechanic" | "pilot" | "guest" | "unknown";

export interface Team {
  id: string;
  name: string;
  category: string;
  club?: string | undefined;
  countryCode: string;
  role: TeamRole;
}
