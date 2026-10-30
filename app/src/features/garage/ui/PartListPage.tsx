import { useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Badge } from "@/shared/ui/Badge";
import { SearchInput } from "@/shared/ui/SearchInput";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";
import { cn } from "@/shared/lib/cn";
import { normalizeSearch } from "@/shared/lib/normalize-search";

import { countDraftQueryOptions, partsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatQuantity } from "../domain/format";
import { partKinds, type PartWithStock } from "../domain/models";
import { canSeeCosts, canWriteGarage } from "../domain/roles";
import { CountScopeSheet } from "./CountScopeSheet";
import { GarageEmpty, GaragePageHeader } from "./common";

export function PartListPage({
  teamId,
  role,
  repository,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const parts = useQuery(partsQueryOptions(repository, teamId, { costs: canSeeCosts(role) }));
  const [filter, setFilter] = useState<"all" | "low">("all");
  const [search, setSearch] = useState("");
  const draft = useQuery(countDraftQueryOptions(repository, teamId));
  const [scopeOpen, setScopeOpen] = useState(false);
  const navigate = useNavigate();
  const openCount = () => void navigate({ to: "/teams/$teamId/garage/parts/count", params: { teamId } });
  const canWrite = canWriteGarage(role);

  const lowCount = parts.data?.filter((part) => part.belowMin).length ?? 0;
  const visible = useMemo(
    () => {
      const needle = normalizeSearch(search);
      return (parts.data ?? []).filter(
        (part) =>
          (filter === "all" || part.belowMin) &&
          (needle === "" ||
            normalizeSearch(
              [part.description, part.brand, part.reference, t(`garage.kinds.${part.kind}`)]
                .filter(Boolean)
                .join(" "),
            ).includes(needle)),
      );
    },
    [parts.data, filter, search, t],
  );
  const groups = useMemo(() => groupParts(visible), [visible]);

  return (
    <>
      <GaragePageHeader
        title={t("garage.sections.parts")}
        subtitle={t("garage.subtitle")}
        action={
          canWrite ? (
            <Link
              to="/teams/$teamId/garage/parts/new"
              params={{ teamId }}
              className="flex min-h-12 shrink-0 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
            >
              {t("garage.actions.newPart")}
            </Link>
          ) : null
        }
      />
      <div className="mb-3">
        <Link
          to="/teams/$teamId/garage/shopping"
          params={{ teamId }}
          className="flex min-h-12 items-center justify-between gap-3 rounded-2xl border border-line bg-surface px-4 text-sm font-semibold text-fg"
        >
          <span>{t("garage.shopping.open")}</span>
          {lowCount > 0 ? <span className="tabular-nums text-warn">{lowCount}</span> : null}
        </Link>
      </div>
      {canWrite ? (
        <div className="mb-3">
          {draft.data ? (
            <Link
              to="/teams/$teamId/garage/parts/count"
              params={{ teamId }}
              className="flex min-h-12 items-center justify-between gap-3 rounded-2xl border border-warn/40 bg-warn/10 px-4 text-sm font-semibold text-fg"
            >
              <span>{t("garage.count.pending")}</span>
              <span className="text-primary">{t("garage.count.resume")}</span>
            </Link>
          ) : (
            <button
              type="button"
              onClick={() => setScopeOpen(true)}
              className="flex min-h-12 w-full items-center justify-center rounded-2xl border border-line bg-surface px-4 text-sm font-semibold text-fg"
            >
              {t("garage.count.title")}
            </button>
          )}
        </div>
      ) : null}
      <div className="mb-3">
        <SearchInput
          label={t("garage.search")}
          placeholder={t("garage.search")}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      </div>
      <div className="mb-4 flex gap-2" role="group" aria-label={t("garage.sections.stock")}>
        <FilterButton active={filter === "all"} onClick={() => setFilter("all")}>
          {t("garage.all")}
        </FilterButton>
        <FilterButton active={filter === "low"} onClick={() => setFilter("low")}>
          {t("garage.belowMinFilter")}
          {lowCount > 0 ? <span className="ml-1 tabular-nums">({lowCount})</span> : null}
        </FilterButton>
      </div>
      {parts.isPending ? (
        <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
          <Spinner />
          <span>{t("garage.loading")}</span>
        </div>
      ) : parts.isError ? (
        <ErrorState
          title={t("garage.errorTitle")}
          body={t("garage.errorBody")}
          onRetry={() => void parts.refetch()}
        />
      ) : visible.length === 0 ? (
        <GarageEmpty
          title={t("garage.empty.parts")}
          body={t("garage.empty.partsBody")}
          actionLabel={canWrite ? t("garage.actions.newPart") : undefined}
          to={canWrite ? "parts" : undefined}
          teamId={teamId}
        />
      ) : (
        <div className="space-y-6">
          {groups.map(([kind, items]) => (
            <section key={kind} aria-labelledby={`part-kind-${kind}`}>
              <h2 id={`part-kind-${kind}`} className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">
                {t(`garage.kinds.${kind}`)}
              </h2>
              <ul className="grid gap-2">
                {items.map((part) => (
                  <li key={part.id}>
                    <Link
                      to="/teams/$teamId/garage/parts/$partId"
                      params={{ teamId, partId: part.id }}
                      className="flex min-h-16 items-center gap-3 rounded-2xl border border-line bg-surface p-4"
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-semibold text-fg">
                          {part.description}
                        </span>
                        <span className="block truncate text-xs text-muted">
                          {[part.brand, part.reference].filter(Boolean).join(" · ") ||
                            t(`garage.kinds.${part.kind}`)}
                        </span>
                        {part.belowMin ? (
                          <span className="mt-1 block text-xs font-semibold text-warn">
                            {t("garage.part.belowMin", {
                              min: formatQuantity(part.minStockCenti, part.unit, i18n.language),
                            })}
                          </span>
                        ) : (
                          <span className="mt-1 block text-xs text-muted">
                            {t("garage.part.minStock", {
                              min: formatQuantity(part.minStockCenti, part.unit, i18n.language),
                            })}
                          </span>
                        )}
                      </span>
                      <span className="shrink-0 text-right">
                        <span className="block text-base font-bold tabular-nums text-fg">
                          {formatQuantity(part.qtyCenti, part.unit, i18n.language)}
                        </span>
                        {part.belowMin ? (
                          <Badge tone="warn">{t("garage.belowMinFilter")}</Badge>
                        ) : null}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
      {canWrite ? (
        <CountScopeSheet
          open={scopeOpen}
          onClose={() => setScopeOpen(false)}
          onStarted={openCount}
          teamId={teamId}
          repository={repository}
        />
      ) : null}
    </>
  );
}

function FilterButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "min-h-11 rounded-xl border px-3 text-sm font-semibold",
        active ? "border-primary bg-primary/15 text-primary" : "border-line bg-surface text-muted",
      )}
    >
      {children}
    </button>
  );
}

function groupParts(parts: PartWithStock[]): Array<[PartWithStock["kind"], PartWithStock[]]> {
  const map = new Map<PartWithStock["kind"], PartWithStock[]>();
  for (const part of parts) {
    const bucket = map.get(part.kind) ?? [];
    bucket.push(part);
    map.set(part.kind, bucket);
  }
  return partKinds.flatMap((kind) => {
    const items = map.get(kind);
    return items ? [[kind, items] as const] : [];
  });
}
