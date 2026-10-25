import { Link, Outlet } from "@tanstack/react-router";
import { AccountMenu } from "./AccountMenu";
import { LanguageSwitcher } from "./LanguageSwitcher";
import { Logo } from "./Logo";

export function AppShell() {
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 h-16 border-b border-line bg-bg/90 backdrop-blur">
        <div className="mx-auto flex h-full max-w-5xl items-center justify-between gap-4 px-4 sm:px-6">
          <Link to="/teams" activeOptions={{ exact: true }}>
            <Logo />
          </Link>
          <div className="flex items-center gap-2">
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
