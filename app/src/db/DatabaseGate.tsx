import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { sessionQueryKey } from "@/auth/session-query";
import { Spinner } from "@/shared/ui/Spinner";
import { ErrorState } from "@/shared/ui/ErrorState";

import { closeDatabase, openDatabase } from "./client";
import { subscribeToLogout } from "./session-channel";
import { bootstrapTeams, hasLocalTeams } from "@/sync/teams";

type GateState = { status: "loading" } | { status: "ready" } | { status: "error" };

export function DatabaseGate({ userId, children }: { userId: string; children: ReactNode }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [state, setState] = useState<GateState>({ status: "loading" });

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        await openDatabase(userId);
        try {
          await bootstrapTeams();
        } catch (error) {
          if (!(await hasLocalTeams())) throw error;
        }
        await queryClient.invalidateQueries({ queryKey: ["teams", "mine"] });
        if (active) setState({ status: "ready" });
      } catch {
        if (active) setState({ status: "error" });
      }
    })();
    return () => {
      active = false;
    };
  }, [queryClient, userId]);

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
        />
      </main>
    );
  }

  return <>{children}</>;
}
