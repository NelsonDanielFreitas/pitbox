import type { ReactNode } from "react";
import { Link, Outlet } from "@tanstack/react-router";
import { SyncIndicator } from "@/features/sync/ui/SyncIndicator";

import { AccountMenu } from "./AccountMenu";
import { LanguageSwitcher } from "./LanguageSwitcher";
import { Logo } from "./Logo";

export function AppShell({ notices }: { notices?: ReactNode }) {
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 h-16 border-b border-line bg-bg/90 backdrop-blur">
        <div className="mx-auto flex h-full max-w-5xl items-center justify-between gap-2 px-3 sm:gap-4 sm:px-6">
          <Link to="/teams" activeOptions={{ exact: true }}>
            <Logo />
          </Link>
          <div className="flex items-center gap-1 sm:gap-2">
            <SyncIndicator />
            <LanguageSwitcher />
            <AccountMenu />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-8 sm:px-6">
        {notices}
        <Outlet />
      </main>
    </div>
  );
}
