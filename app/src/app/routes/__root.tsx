import { createRootRouteWithContext, Outlet, type ErrorComponentProps } from "@tanstack/react-router";

import { useTranslation } from "react-i18next";

import { ErrorState } from "@/shared/ui/ErrorState";

import type { RouterContext } from "../router-context";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Root,
  errorComponent: RootError,
  notFoundComponent: NotFound,
});

function Root() {
  return <Outlet />;
}

function RootError({ error }: ErrorComponentProps) {
  const { t } = useTranslation();
  return (
    <main className="mx-auto max-w-5xl px-4 py-10">
      <ErrorState
        title={t("teams.errorTitle")}
        body={t("errors.generic")}
        onRetry={() => window.location.reload()}
      />
      {import.meta.env.DEV ? <pre className="mt-4 text-xs text-red-900">{error instanceof Error ? error.message : String(error)}</pre> : null}
    </main>
  );
}

function NotFound() {
  const { t } = useTranslation();
  return (
    <main className="mx-auto max-w-5xl px-4 py-10">
      <h1 className="text-2xl font-bold">{t("teams.errorTitle")}</h1>
      <p className="mt-2 text-pitbox-muted">{t("errors.generic")}</p>
    </main>
  );
}
