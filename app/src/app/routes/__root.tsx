import { createRootRouteWithContext, Outlet, type ErrorComponentProps } from "@tanstack/react-router";

import { useTranslation } from "react-i18next";

import { AuthUnavailableError, OriginMismatchError } from "@/shared/lib/errors";
import { ErrorState } from "@/shared/ui/ErrorState";
import { NotFound } from "@/shared/ui/NotFound";

import type { RouterContext } from "../router-context";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Root,
  errorComponent: RootError,
  notFoundComponent: NotFound,
});

function Root() {
  return <Outlet />;
}

export function RootError({ error }: Pick<ErrorComponentProps, "error">) {
  const { t } = useTranslation();
  const unavailable = error instanceof AuthUnavailableError;
  const originMismatch = error instanceof OriginMismatchError;
  const showOriginMismatch = originMismatch && import.meta.env.DEV;

  return (
    <main className="mx-auto max-w-5xl px-4 py-10">
      <ErrorState
        title={t("errors.title")}
        body={
          unavailable
            ? t("errors.unavailable")
            : showOriginMismatch
              ? t("errors.originMismatch")
              : t("errors.generic")
        }
        onRetry={unavailable ? () => window.location.reload() : undefined}
      />
      {import.meta.env.DEV ? (
        <pre className="mt-4 text-xs text-crit">
          {error instanceof Error ? error.message : String(error)}
        </pre>
      ) : null}
    </main>
  );
}
