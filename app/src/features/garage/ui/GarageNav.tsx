import { Link, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

const sections = ["cars", "engines", "tyres", "parts"] as const;
type Section = (typeof sections)[number];

const labels = {
  cars: "garage.sections.cars",
  engines: "garage.sections.engines",
  tyres: "garage.sections.tyres",
  parts: "garage.sections.parts",
} as const satisfies Record<Section, `garage.sections.${Section}`>;

export function GarageNav({ teamId }: { teamId: string }) {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  return (
    <nav aria-label={t("garage.title")} className="mb-5 overflow-x-auto">
      <div className="flex min-w-max gap-1 rounded-2xl border border-line bg-surface p-1">
        {sections.map((section) => {
          const href = `/teams/${teamId}/garage/${section}`;
          const active = pathname === href || pathname.startsWith(`${href}/`);
          return (
            <Link
              key={section}
              to={`/teams/$teamId/garage/${section}` as const}
              params={{ teamId }}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex min-h-11 items-center rounded-xl px-3 text-sm font-semibold",
                active ? "bg-primary text-on-primary" : "text-muted hover:bg-surface-2 hover:text-fg",
              )}
            >
              {t(labels[section])}
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
