import { createContext, useContext } from "react";

import type { Team } from "../domain/team";

export const TeamContext = createContext<Team | null>(null);

export function useTeamContext(): Team {
  const team = useContext(TeamContext);
  if (!team) throw new Error("useTeamContext must be used inside the team layout");
  return team;
}
