import { Link, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

export function TeamTabBar({
  teamId,
  showGarage,
  showMore,
}: {
  teamId: string;
  showGarage: boolean;
  showMore: boolean;
}) {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  if (/\/new$/.test(pathname) || /\/edit$/.test(pathname)) return null;

  return (
    <nav
      aria-label={t("garage.navLabel")}
      className="fixed inset-x-0 bottom-0 z-30 border-t border-line bg-surface/95 px-4 pb-[calc(0.5rem+env(safe-area-inset-bottom))] pt-2 backdrop-blur"
    >
      <div className="mx-auto flex max-w-md gap-2">
        <TabLink
          to="/teams/$teamId"
          params={{ teamId }}
          label={t("garage.navHome")}
          active={pathname === `/teams/${teamId}`}
        />
        {showGarage ? (
          <TabLink
            to="/teams/$teamId/garage/cars"
            params={{ teamId }}
            label={t("garage.navGarage")}
            active={pathname.startsWith(`/teams/${teamId}/garage`)}
          />
        ) : null}
        {showMore ? (
          <TabLink
            to="/teams/$teamId/more"
            params={{ teamId }}
            label={t("teams.more")}
            active={pathname.startsWith(`/teams/${teamId}/more`)}
          />
        ) : null}
      </div>
    </nav>
  );
}

function TabLink({
  to,
  params,
  label,
  active,
}: {
  to: "/teams/$teamId" | "/teams/$teamId/garage/cars" | "/teams/$teamId/more";
  params: { teamId: string };
  label: string;
  active: boolean;
}) {
  return (
    <Link
      to={to}
      params={params}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex min-h-12 flex-1 items-center justify-center rounded-xl px-3 text-sm font-semibold",
        active ? "bg-primary/15 text-primary" : "text-muted hover:bg-surface-2 hover:text-fg",
      )}
    >
      {label}
    </Link>
  );
}
