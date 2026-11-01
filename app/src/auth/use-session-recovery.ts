import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

import { NetworkError } from "@/shared/lib/errors";

import { sessionQueryKey } from "./session-query";
import { fetchRememberedSession } from "./session-load";

const retryEveryMs = 30_000;
// The browser says it is online but the request still failed (the link is coming up): try again soon.
const retrySoonMs = 2_000;

/**
 * While the session is offline (ADR 0028), ask the server again when the network may be
 * back: same user carries on, another user starts clean, 401 goes to the login. The local
 * database and the outbox are never deleted here.
 */
export function useSessionRecovery(offlineUserId: string | undefined): void {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  useEffect(() => {
    if (!offlineUserId) return;
    let stopped = false;
    let running = false;
    let soon: number | undefined;

    const attempt = async () => {
      if (running || stopped) return;
      running = true;
      try {
        const fresh = await fetchRememberedSession();
        if (stopped) return;
        if (!fresh.authenticated) {
          queryClient.setQueryData(sessionQueryKey, { authenticated: false });
          const returnTo = `${window.location.pathname}${window.location.search}`;
          void navigate({ to: "/login", search: { returnTo } });
          return;
        }
        // Another account signed in on the same browser: nothing cached for the previous
        // user may be shown (ADR 0018). The gate is keyed by user id and opens the new base.
        // Not clear(): that would detach the session observers from the query set below.
        if (fresh.user_id !== offlineUserId) {
          queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== sessionQueryKey[0] });
        }
        queryClient.setQueryData(sessionQueryKey, fresh);
      } catch (error) {
        // Still no network, or the server answered with an error: stay offline, fail closed
        // for anything new, keep working with what is on the phone.
        if (error instanceof NetworkError && navigator.onLine && !stopped) {
          window.clearTimeout(soon);
          soon = window.setTimeout(() => void attempt(), retrySoonMs);
        }
      } finally {
        running = false;
      }
    };

    const onVisible = () => {
      if (document.visibilityState === "visible") void attempt();
    };
    window.addEventListener("online", attempt);
    document.addEventListener("visibilitychange", onVisible);
    const timer = window.setInterval(() => void attempt(), retryEveryMs);
    return () => {
      stopped = true;
      window.clearTimeout(soon);
      window.removeEventListener("online", attempt);
      document.removeEventListener("visibilitychange", onVisible);
      window.clearInterval(timer);
    };
  }, [navigate, offlineUserId, queryClient]);
}
