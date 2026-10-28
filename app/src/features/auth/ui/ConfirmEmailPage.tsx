import { useTranslation } from "react-i18next";

import { loginHref } from "@/auth/session";
import { Button } from "@/shared/ui/Button";
import { Logo } from "@/shared/ui/Logo";

export function ConfirmEmailPage({ returnTo }: { returnTo: string }) {
  const { t } = useTranslation();

  return (
    <main className="flex min-h-screen flex-col items-center justify-center px-4 py-10">
      <div className="flex w-full max-w-md flex-col items-center text-center">
        <Logo size="lg" />
        <h1 className="mt-8 text-3xl font-bold tracking-tight text-fg">
          {t("auth.confirmEmailTitle")}
        </h1>
        <p className="mt-4 text-base text-muted">{t("auth.confirmEmailBody")}</p>
        <p className="mt-2 text-sm text-muted">{t("auth.confirmEmailResend")}</p>
        <Button asChild size="lg" full className="mt-8">
          <a href={loginHref(returnTo)}>{t("auth.confirmEmailCta")}</a>
        </Button>
      </div>
    </main>
  );
}
