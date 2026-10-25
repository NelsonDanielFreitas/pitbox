import { useTranslation } from "react-i18next";

import { loginHref } from "@/auth/session";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";

export function LoginPage({ returnTo, loggedOut }: { returnTo: string; loggedOut: boolean }) {
  const { t } = useTranslation();

  return (
    <main className="mx-auto flex min-h-screen max-w-5xl items-center justify-center px-4 py-10">
      <Card className="w-full max-w-md">
        <p className="text-sm font-semibold uppercase tracking-widest text-pitbox-accent">
          {t("app.name")}
        </p>
        <h1 className="mt-3 text-3xl font-bold tracking-tight text-pitbox-ink">
          {t("auth.title")}
        </h1>
        <p className="mt-3 text-pitbox-muted">{t("auth.description")}</p>
        {loggedOut ? (
          <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-900" role="status">
            {t("auth.loggedOut")}
          </p>
        ) : null}
        <Button asChild className="mt-6 w-full">
          <a href={loginHref(returnTo)}>{t("auth.cta")}</a>
        </Button>
      </Card>
    </main>
  );
}
