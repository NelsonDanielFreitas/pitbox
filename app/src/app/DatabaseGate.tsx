import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { rememberUserId } from "@/auth/local-data";
import { sessionQueryKey } from "@/auth/session-query";
import { Spinner } from "@/shared/ui/Spinner";
import { ErrorState } from "@/shared/ui/ErrorState";

import { isUnauthenticated } from "@/shared/api/errors";

import { closeDatabase, deleteUserDatabase, openDatabase } from "@/db/client";
import { CorruptLocalDataError } from "@/db/errors";
import { subscribeToLogout } from "@/db/session-channel";
import { bootstrapTeams, hasLocalTeams } from "@/sync/teams";

type GateState = { status: "loading" } | { status: "ready" } | { status: "error" };

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function openWithRetry(userId: string): Promise<void> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 5; attempt += 1) {
    try {
      await openDatabase(userId);
      return;
    } catch (error) {
      lastError = error;
      if (error instanceof CorruptLocalDataError) throw error;
      await sleep(Math.min(100 * 2 ** attempt, 1_000));
    }
  }
  throw lastError;
}

export function DatabaseGate({ userId, children }: { userId: string; children: ReactNode }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [state, setState] = useState<GateState>({ status: "loading" });
  const previousUserId = useRef<string | undefined>(undefined);

  useEffect(() => {
    let active = true;
    rememberUserId(userId);
    if (previousUserId.current !== undefined && previousUserId.current !== userId) {
      queryClient.clear();
    }
    previousUserId.current = userId;

    void (async () => {
      try {
        await openWithRetry(userId);
        try {
          const forceRefresh = new URLSearchParams(window.location.search).get("refresh") === "teams";
          await bootstrapTeams(forceRefresh);
        } catch (error) {
          if (isUnauthenticated(error)) {
            queryClient.removeQueries({ queryKey: sessionQueryKey });
            queryClient.setQueryData(sessionQueryKey, { authenticated: false });
            const returnTo = `${window.location.pathname}${window.location.search}`;
            void navigate({ to: "/login", search: { returnTo } });
            return;
          }
          if (!(await hasLocalTeams())) throw error;
        }
        await queryClient.invalidateQueries({ queryKey: ["teams", "mine"] });
        if (active) setState({ status: "ready" });
      } catch (error) {
        if (isUnauthenticated(error)) {
          queryClient.removeQueries({ queryKey: sessionQueryKey });
          queryClient.setQueryData(sessionQueryKey, { authenticated: false });
          const returnTo = `${window.location.pathname}${window.location.search}`;
          void navigate({ to: "/login", search: { returnTo } });
          return;
        }
        if (active) setState({ status: "error" });
      }
    })();
    return () => {
      active = false;
    };
  }, [navigate, queryClient, userId]);

  useEffect(
    () =>
      subscribeToLogout(() => {
        void closeDatabase();
        queryClient.clear();
        queryClient.setQueryData(sessionQueryKey, { authenticated: false });
        void navigate({ to: "/login", search: { logged_out: 1 } });
      }),
    [navigate, queryClient],
  );

  if (state.status === "loading") {
    return (
      <main className="flex min-h-screen items-center justify-center gap-3 text-muted" role="status">
        <Spinner />
        <span>{t("local.loading")}</span>
      </main>
    );
  }

  if (state.status === "error") {
    return (
      <main className="mx-auto max-w-5xl px-4 py-10">
        <ErrorState
          title={t("local.errorTitle")}
          body={t("local.errorBody")}
          onRetry={() => window.location.reload()}
          onClear={() =>
            void deleteUserDatabase(userId)
              .then(() => window.location.reload())
              .catch(() => undefined)
          }
        />
      </main>
    );
  }

  return <>{children}</>;
}
