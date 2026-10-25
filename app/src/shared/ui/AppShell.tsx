import { Link, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { AccountMenu } from "./AccountMenu";
import { LanguageSwitcher } from "./LanguageSwitcher";

export function AppShell() {
  const { t } = useTranslation();

  return (
    <div className="min-h-screen">
      <header className="border-b border-pitbox-line/70 bg-pitbox-card/95">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-3 sm:px-6">
          <Link to="/teams" className="text-lg font-black tracking-tight text-pitbox-ink">
            {t("app.name")}
          </Link>
          <div className="flex items-center gap-3">
            <LanguageSwitcher />
            <AccountMenu />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-8 sm:px-6">
        <Outlet />
      </main>
    </div>
  );
}
