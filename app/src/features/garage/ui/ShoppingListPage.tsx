import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { TeamRole } from "@/features/teams/domain/team";
import { Alert } from "@/shared/ui/Alert";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Sheet } from "@/shared/ui/Sheet";
import { Spinner } from "@/shared/ui/Spinner";

import { useSession } from "@/auth/session-query";

import { partsQueryOptions, shoppingQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { centiToInput, formatQuantity } from "../domain/format";
import type { ShoppingLine } from "../domain/models";
import { canSeeCosts, canWriteGarage } from "../domain/roles";
import {
  groupByBrand,
  lineCostCents,
  shoppingShareText,
  totalCostCents,
} from "../domain/shopping";
import { todayIso } from "../domain/today";
import { GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";
import { MovementSheet } from "./MovementSheet";
import { ShopQtySheet } from "./ShopQtySheet";

export function ShoppingListPage({
  teamId,
  teamName,
  role,
  repository,
}: {
  teamId: string;
  teamName: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const costs = canSeeCosts(role);
  const canWrite = canWriteGarage(role);
  const list = useQuery(shoppingQueryOptions(repository, teamId, { costs }));
  const parts = useQuery(partsQueryOptions(repository, teamId, { costs }));
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const [addOpen, setAddOpen] = useState(false);
  const [editing, setEditing] = useState<ShoppingLine | null>(null);
  const [buying, setBuying] = useState<ShoppingLine | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
  const money = (cents: number) =>
    new Intl.NumberFormat(i18n.language, { style: "currency", currency: "EUR" }).format(cents / 100);

  if (list.isPending || parts.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (list.isError || parts.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => {
          void list.refetch();
          void parts.refetch();
        }}
      />
    );
  }

  const lines = list.data;
  const groups = groupByBrand(lines, t("garage.shopping.noBrand"), i18n.language);
  const shareText = shoppingShareText(
    lines,
    {
      heading: t("garage.shopping.shareHeading", {
        team: teamName,
        date: todayIso(),
      }),
      noBrand: t("garage.shopping.noBrand"),
      total: t("garage.shopping.total"),
      locale: i18n.language,
      formatMoney: money,
    },
    costs,
  );

  const remove = async (line: ShoppingLine) => {
    setError(null);
    try {
      await repository.removeShoppingItem(teamId, line.partId);
      await refresh();
    } catch (failure) {
      setError(garageErrorMessage(t, failure));
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(shareText);
      setCopied(true);
    } catch {
      // Clipboard can be blocked; the text stays selectable in the sheet.
      setCopied(false);
    }
  };

  const buyingPart = buying ? parts.data.find((part) => part.id === buying.partId) : undefined;
  const editingPart = editing ? parts.data.find((part) => part.id === editing.partId) : undefined;

  return (
    <>
      <GaragePageHeader
        title={t("garage.shopping.title", { count: lines.length })}
        subtitle={t("garage.shopping.subtitle")}
      />
      {error ? (
        <div className="mb-3">
          <Alert tone="crit">{error}</Alert>
        </div>
      ) : null}
      {lines.length === 0 ? (
        <EmptyState title={t("garage.shopping.empty")} body={t("garage.shopping.emptyBody")} />
      ) : (
        <div className="space-y-4">
          {costs && lines.some((line) => lineCostCents(line) !== null) ? (
            <Card>
              <p className="text-xs text-muted">{t("garage.shopping.total")}</p>
              <p className="text-3xl font-extrabold tabular-nums">{money(totalCostCents(lines))}</p>
              <p className="text-xs text-muted">{t("garage.shopping.totalHint")}</p>
            </Card>
          ) : null}
          <div className="flex gap-2">
            <Button
              variant="secondary"
              full
              onClick={() => {
                setCopied(false);
                setShareOpen(true);
              }}
            >
              {t("garage.shopping.share")}
            </Button>
            {canWrite ? (
              <Button variant="secondary" full onClick={() => setAddOpen(true)}>
                {t("garage.shopping.add")}
              </Button>
            ) : null}
          </div>
          {groups.map(([brand, group], groupIndex) => (
            <section key={brand} aria-labelledby={`shop-brand-${groupIndex}`}>
              <h2
                id={`shop-brand-${groupIndex}`}
                className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted"
              >
                {brand}
              </h2>
              <ul className="overflow-hidden rounded-2xl border border-line bg-surface">
                {group.map((line, index) => {
                  const cost = costs ? lineCostCents(line) : null;
                  return (
                    <li
                      key={line.partId}
                      className={`flex flex-col gap-2 px-4 py-3 ${index > 0 ? "border-t border-line" : ""}`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <Link
                          to="/teams/$teamId/garage/parts/$partId"
                          params={{ teamId, partId: line.partId }}
                          className="min-w-0 py-1"
                        >
                          <span className="block font-semibold text-fg">{line.description}</span>
                          <span className="block text-xs tabular-nums text-muted">
                            {t("garage.shopping.have", {
                              qty: formatQuantity(line.stockCenti, line.unit, i18n.language),
                            })}
                            {" · "}
                            {t("garage.shopping.buy", {
                              qty: formatQuantity(line.qtyCenti, line.unit, i18n.language),
                            })}
                          </span>
                          <span className="block text-xs text-muted">
                            {line.reference ?? "—"}
                            {cost === null ? "" : ` · ${money(cost)}`}
                          </span>
                        </Link>
                        <Badge tone={line.belowMin ? "info" : undefined}>
                          {line.belowMin ? t("garage.shopping.auto") : t("garage.shopping.manual")}
                        </Badge>
                      </div>
                      {canWrite ? (
                        <div className="grid grid-cols-3 gap-2">
                          <Button variant="secondary" onClick={() => setEditing(line)}>
                            {t("garage.shopping.editQtyShort")}
                          </Button>
                          <Button variant="secondary" onClick={() => void remove(line)}>
                            {t("garage.shopping.remove")}
                          </Button>
                          <Button onClick={() => setBuying(line)}>{t("garage.shopping.bought")}</Button>
                        </div>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}
        </div>
      )}
      {lines.length === 0 && canWrite ? (
        <div className="mt-4">
          <Button variant="secondary" onClick={() => setAddOpen(true)}>
            {t("garage.shopping.add")}
          </Button>
        </div>
      ) : null}
      {canWrite ? (
        <>
          {addOpen ? (
            <ShopQtySheet
              open
              onClose={() => setAddOpen(false)}
              onSave={async (partId, qty) => {
                await repository.setShoppingItem(teamId, partId, qty);
                await refresh();
              }}
              teamId={teamId}
              parts={parts.data}
              canCreate
            />
          ) : null}
          {editing && editingPart ? (
            <ShopQtySheet
              key={editing.partId}
              open
              onClose={() => setEditing(null)}
              onSave={async (partId, qty) => {
                await repository.setShoppingItem(teamId, partId, qty);
                await refresh();
              }}
              teamId={teamId}
              parts={parts.data}
              part={editingPart}
              initialQtyCenti={editing.qtyCenti}
              canCreate={false}
            />
          ) : null}
          {buying && buyingPart ? (
            <MovementSheet
              key={buying.partId}
              open
              onClose={() => setBuying(null)}
              teamId={teamId}
              part={buyingPart}
              repository={repository}
              userId={userId}
              showCosts={costs}
              initialReason="purchase"
              initialQty={centiToInput(buying.qtyCenti, buying.unit)}
            />
          ) : null}
        </>
      ) : null}
      {shareOpen ? (
      <Sheet
        open
        title={t("garage.shopping.shareTitle")}
        description={t("garage.shopping.shareBody")}
        onClose={() => setShareOpen(false)}
      >
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded-2xl border border-line bg-surface-2 p-4 font-sans text-sm leading-relaxed">
          {shareText}
        </pre>
        <div className="mt-4 flex gap-2">
          <Button variant="secondary" full onClick={() => setShareOpen(false)}>
            {t("garage.shopping.close")}
          </Button>
          <Button full onClick={() => void copy()}>
            {copied ? t("garage.shopping.copied") : t("garage.shopping.copy")}
          </Button>
        </div>
      </Sheet>
      ) : null}
    </>
  );
}
