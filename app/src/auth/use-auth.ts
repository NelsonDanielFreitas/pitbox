import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";

import { sessionQueryKey, useSession } from "./session-query";

export function useAuth() {
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const refresh = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: sessionQueryKey });
    await navigate({ to: "/login", search: { returnTo: "/teams" } });
  }, [navigate, queryClient]);

  return { ...session, refresh };
}
