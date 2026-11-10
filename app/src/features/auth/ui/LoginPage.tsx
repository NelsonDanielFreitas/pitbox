import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { loginHref } from "@/auth/session";
import { sessionQueryOptions } from "@/auth/session-query";
import {
  AuthTabUnavailableError,
  LoginCancelledError,
  LoginRefusedError,
  mobileAuth,
} from "@/auth/native";
import { NetworkError } from "@/shared/lib/errors";
import { isNativePlatform } from "@/shared/lib/platform";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { Logo } from "@/shared/ui/Logo";

type NativeFailure = "error" | "cancelled" | "noAuthTab" | "offline" | "refused";

const failureKey = {
  error: "auth.nativeError",
  cancelled: "auth.nativeCancelled",
  noAuthTab: "auth.nativeNoAuthTab",
  offline: "auth.nativeOffline",
  refused: "auth.nativeRefused",
} as const satisfies Record<NativeFailure, string>;

function failureOf(error: unknown): NativeFailure {
  if (error instanceof LoginCancelledError) return "cancelled";
  if (error instanceof AuthTabUnavailableError) return "noAuthTab";
  if (error instanceof NetworkError) return "offline";
  if (error instanceof LoginRefusedError && (error.reason === "not_allowed" || error.reason === "email_not_verified")) {
    return "refused";
  }
  return "error";
}

export function LoginPage({ returnTo, loggedOut }: { returnTo: string; loggedOut: boolean }) {
  const { t } = useTranslation();
  const native = isNativePlatform();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<NativeFailure | null>(null);

  // The system browser (Auth Tab) does the sign-in, never this WebView (ADR 0032).
  const signInNative = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await mobileAuth().login();
      // Read the new session now. invalidateQueries is not enough: the guards of the routes use
      // ensureQueryData, which keeps answering with the cached "no session" of the login screen
      // and sends the user straight back here.
      await queryClient.fetchQuery({ ...sessionQueryOptions, staleTime: 0 });
      await navigate({ to: returnTo });
    } catch (error) {
      setFailure(failureOf(error));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="flex min-h-screen flex-col items-center justify-center px-4 py-10">
      <div className="flex w-full max-w-md flex-col items-center text-center">
        <Logo size="lg" />
        <h1 className="mt-8 text-3xl font-bold tracking-tight text-fg">{t("auth.title")}</h1>
        <p className="mt-4 text-base text-muted">{t("auth.description")}</p>
        {loggedOut ? (
          <Alert tone="ok" className="mt-6 w-full text-left">
            {t("auth.loggedOut")}
          </Alert>
        ) : null}
        {failure ? (
          <Alert tone="crit" className="mt-6 w-full text-left">
            {t(failureKey[failure])}
          </Alert>
        ) : null}
        {native ? (
          <Button size="lg" full className="mt-8" disabled={busy} onClick={() => void signInNative()}>
            {busy ? t("auth.nativeBusy") : t("auth.cta")}
          </Button>
        ) : (
          <Button asChild size="lg" full className="mt-8">
            <a href={loginHref(returnTo)}>{t("auth.cta")}</a>
          </Button>
        )}
      </div>
    </main>
  );
}
