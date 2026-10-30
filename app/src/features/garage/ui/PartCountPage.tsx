import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import type { TeamRole } from "@/features/teams/domain/team";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { Dialog } from "@/shared/ui/Dialog";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";
import { Stepper } from "@/shared/ui/Stepper";

import { countDraftQueryOptions, partsQueryOptions } from "../data/garage-queries";
import type { GarageRepository } from "../data/garage-repository";
import { formatDate, formatQuantity } from "../domain/format";
import { partKinds, type PartWithStock } from "../domain/models";
import { canWriteGarage } from "../domain/roles";
import { todayIso } from "../domain/today";
import { GarageForbidden, GaragePageHeader } from "./common";
import { garageErrorMessage } from "./errors";

type Step = "count" | "review" | { applied: number };

export function PartCountPage({
  teamId,
  role,
  repository,
}: {
  teamId: string;
  role: TeamRole;
  repository: GarageRepository;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const draft = useQuery(countDraftQueryOptions(repository, teamId));
  const parts = useQuery(partsQueryOptions(repository, teamId));
  const session = useSession();
  const userId = session.data?.authenticated ? session.data.user_id : null;
  const [values, setValues] = useState<Record<string, number>>({});
  const [step, setStep] = useState<Step>("count");
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const loaded = useRef<string | null>(null);

  // The lines are kept in state so quick taps on − and + never read a stale query result;
  // each change is also written to the local database.
  useEffect(() => {
    if (draft.data && loaded.current !== draft.data.startedAt) {
      loaded.current = draft.data.startedAt;
      setValues(draft.data.lines);
    }
  }, [draft.data]);

  const scopeKind = draft.data?.scopeKind ?? null;
  const scope = (parts.data ?? []).filter((part) => scopeKind === null || part.kind === scopeKind);
  const diffs = scope.filter((part) => part.id in values && values[part.id] !== part.qtyCenti);
  const counted = scope.filter((part) => part.id in values).length;

  if (!canWriteGarage(role)) return <GarageForbidden />;
  if (draft.isPending || parts.isPending) {
    return (
      <div aria-busy="true" role="status" className="flex items-center gap-3 text-muted">
        <Spinner />
        <span>{t("garage.loading")}</span>
      </div>
    );
  }
  if (draft.isError || parts.isError) {
    return (
      <ErrorState
        title={t("garage.errorTitle")}
        body={t("garage.errorBody")}
        onRetry={() => {
          void draft.refetch();
          void parts.refetch();
        }}
      />
    );
  }
  if (typeof step === "object") {
    return (
      <>
        <GaragePageHeader title={t("garage.count.title")} subtitle={t("garage.subtitle")} />
        <div role="status" className="rounded-2xl border border-line bg-surface p-6 text-center">
          <p className="text-lg font-semibold">{t("garage.count.applied", { count: step.applied })}</p>
          <Link
            to="/teams/$teamId/garage/parts"
            params={{ teamId }}
            className="mt-4 inline-flex min-h-12 items-center rounded-xl bg-primary px-4 text-sm font-semibold text-on-primary"
          >
            {t("garage.count.backToParts")}
          </Link>
        </div>
      </>
    );
  }
  if (draft.data === null) {
    return <EmptyState title={t("garage.count.noDraft")} body={t("garage.count.noDraftBody")} />;
  }

  const setLine = (part: PartWithStock, next: number | null) => {
    setValues((current) => {
      const copy = { ...current };
      if (next === null) delete copy[part.id];
      else copy[part.id] = next;
      return copy;
    });
    repository.setCountLine(teamId, part.id, next).catch((failure: unknown) => {
      setError(garageErrorMessage(t, failure));
    });
  };

  const discard = async () => {
    await repository.discardCount(teamId);
    await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
    loaded.current = null;
    setValues({});
  };

  const apply = async () => {
    setBusy(true);
    setError(null);
    try {
      const today = todayIso();
      const applied = await repository.applyCount(teamId, {
        occurredOn: today,
        note: t("garage.count.note", { date: formatDate(today, i18n.language) }),
        createdBy: userId,
      });
      await queryClient.invalidateQueries({ queryKey: ["garage", teamId] });
      setConfirm(false);
      setStep({ applied });
    } catch (failure) {
      setConfirm(false);
      setError(garageErrorMessage(t, failure));
    } finally {
      setBusy(false);
    }
  };

  const title = draft.data.scopeKind
    ? t(`garage.kinds.${draft.data.scopeKind}`)
    : t("garage.count.allParts");

  if (step === "review") {
    return (
      <>
        <GaragePageHeader title={t("garage.count.review")} subtitle={title} />
        <p className="mb-3 text-sm text-muted">
          {t("garage.count.progress", { done: counted, total: scope.length })}
        </p>
        {error ? <Alert tone="crit">{error}</Alert> : null}
        {draft.data.movementsSince > 0 ? (
          <div className="mb-3">
            <Alert tone="warn">
              {t("garage.count.intruders", { count: draft.data.movementsSince })}
            </Alert>
          </div>
        ) : null}
        {diffs.length === 0 ? (
          <p className="rounded-2xl border border-dashed border-line p-5 text-center text-sm text-muted">
            {t("garage.count.noDiffs")}
          </p>
        ) : (
          <ul className="overflow-hidden rounded-2xl border border-line bg-surface">
            {diffs.map((part, index) => {
              const delta = (values[part.id] ?? 0) - part.qtyCenti;
              return (
                <li
                  key={part.id}
                  className={`flex min-h-16 items-center gap-3 px-4 py-3 ${index > 0 ? "border-t border-line" : ""}`}
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-semibold">{part.description}</span>
                    <span className="block truncate text-xs text-muted">
                      {[part.brand, part.reference].filter(Boolean).join(" · ")}
                    </span>
                  </span>
                  <span className="text-right tabular-nums">
                    <span className="block font-bold">
                      {formatQuantity(part.qtyCenti, part.unit, i18n.language)} →{" "}
                      {formatQuantity(values[part.id] ?? 0, part.unit, i18n.language)}
                    </span>
                    <span className={`block text-sm font-semibold ${delta < 0 ? "text-crit" : "text-ok"}`}>
                      {delta > 0 ? "+" : ""}
                      {formatQuantity(delta, part.unit, i18n.language)}
                    </span>
                  </span>
                </li>
              );
            })}
          </ul>
        )}
        <p className="mt-3 text-xs text-muted">{t("garage.count.reviewHint")}</p>
        <div className="mt-5 flex gap-2">
          <Button variant="secondary" full onClick={() => setStep("count")}>
            {t("garage.count.backToCount")}
          </Button>
          {diffs.length === 0 ? (
            <Button
              full
              onClick={() => {
                void discard().then(() => setStep({ applied: 0 }));
              }}
            >
              {t("garage.count.finishNoAdjust")}
            </Button>
          ) : (
            <Button full onClick={() => setConfirm(true)}>
              {t("garage.count.applyN", { count: diffs.length })}
            </Button>
          )}
        </div>
        <Dialog
          open={confirm}
          title={t("garage.count.confirmTitle", { count: diffs.length })}
          description={t("garage.count.confirmBody")}
          onClose={() => setConfirm(false)}
        >
          <div className="flex gap-2">
            <Button variant="secondary" full onClick={() => setConfirm(false)}>
              {t("garage.actions.cancel")}
            </Button>
            <Button full disabled={busy} onClick={() => void apply()}>
              {t("garage.count.applyN", { count: diffs.length })}
            </Button>
          </div>
        </Dialog>
      </>
    );
  }

  return (
    <>
      <GaragePageHeader title={title} subtitle={t("garage.count.savedHint")} />
      {error ? (
        <div className="mb-3">
          <Alert tone="crit">{error}</Alert>
        </div>
      ) : null}
      <div className="space-y-6 pb-20">
        {partKinds.map((kind) => {
          const rows = scope.filter((part) => part.kind === kind);
          if (rows.length === 0) return null;
          return (
            <section key={kind} aria-labelledby={`count-kind-${kind}`}>
              <h2
                id={`count-kind-${kind}`}
                className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted"
              >
                {t(`garage.kinds.${kind}`)}
              </h2>
              <ul className="overflow-hidden rounded-2xl border border-line bg-surface">
                {rows.map((part, index) => {
                  const value = values[part.id] ?? null;
                  const delta = value === null ? 0 : value - part.qtyCenti;
                  return (
                    <li
                      key={part.id}
                      className={`flex flex-col gap-3 px-4 py-3.5 ${index > 0 ? "border-t border-line" : ""}`}
                    >
                      <span className="flex items-start justify-between gap-3">
                        <span className="min-w-0">
                          <span className="block font-semibold">{part.description}</span>
                          <span className="block text-xs text-muted">
                            {[part.brand, part.reference].filter(Boolean).join(" · ")}
                          </span>
                        </span>
                        <span className="shrink-0 text-right text-sm tabular-nums text-muted">
                          {t("garage.count.expected", {
                            qty: formatQuantity(part.qtyCenti, part.unit, i18n.language),
                          })}
                        </span>
                      </span>
                      <span className="flex items-center justify-between gap-3">
                        <Stepper
                          value={value}
                          onChange={(next) => setLine(part, next)}
                          wholeOnly={part.unit === "un"}
                          inputLabel={t("garage.count.countedOf", { name: part.description })}
                          decreaseLabel={t("garage.count.lessOf", { name: part.description })}
                          increaseLabel={t("garage.count.moreOf", { name: part.description })}
                        />
                        <span
                          className={`text-right text-base font-bold tabular-nums ${
                            delta < 0 ? "text-crit" : delta > 0 ? "text-ok" : "text-muted"
                          }`}
                        >
                          {value === null
                            ? ""
                            : delta === 0
                              ? t("garage.count.right")
                              : `${delta > 0 ? "+" : ""}${formatQuantity(delta, part.unit, i18n.language)}`}
                        </span>
                      </span>
                    </li>
                  );
                })}
              </ul>
            </section>
          );
        })}
        {scope.length === 0 ? (
          <p className="text-sm text-muted">{t("garage.count.emptyScope")}</p>
        ) : null}
      </div>
      {/* Sits above the team tab bar (4 rem plus the safe area), which is fixed to the bottom. */}
      <div className="fixed inset-x-0 bottom-[calc(4rem+env(safe-area-inset-bottom))] z-20 border-t border-line bg-bg/95 p-4 backdrop-blur">
        <div className="mx-auto flex max-w-lg items-center gap-3">
          <p className="flex-1 text-sm tabular-nums">
            <b>{t("garage.count.progress", { done: counted, total: scope.length })}</b>
            {" · "}
            <span className={diffs.length > 0 ? "font-semibold text-warn" : undefined}>
              {t("garage.count.diffs", { count: diffs.length })}
            </span>
          </p>
          <Button variant="secondary" onClick={() => void discard()}>
            {t("garage.count.discard")}
          </Button>
          <Button disabled={counted === 0} onClick={() => setStep("review")}>
            {t("garage.count.reviewAction")}
          </Button>
        </div>
      </div>
    </>
  );
}
