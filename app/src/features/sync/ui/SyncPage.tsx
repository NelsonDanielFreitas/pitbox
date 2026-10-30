import { useTranslation } from "react-i18next";

import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { PageHeader } from "@/shared/ui/PageHeader";
import { useTeamContext } from "@/features/teams/ui/team-context";

import {
  useDismissFailed,
  useReregisterDevice,
  useResolveConflict,
  useSyncIssues,
  useSyncNow,
  useSyncStatus,
  type ConflictItem,
} from "../data/use-sync";

function show(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "string") return value;
  return JSON.stringify(value);
}

export function SyncPage() {
  const { t } = useTranslation();
  const team = useTeamContext();
  const status = useSyncStatus();
  const { conflicts, failed } = useSyncIssues(team.id);
  const resolve = useResolveConflict(team.id);
  const dismiss = useDismissFailed();
  const reregister = useReregisterDevice(team.id);
  const syncNow = useSyncNow();

  const fieldLabel = (item: ConflictItem) =>
    t(`sync.fields.${item.entity}.${item.field}`, { defaultValue: item.field });

  return (
    <main className="space-y-6">
      <PageHeader title={t("sync.title")} subtitle={t("sync.subtitle")} />

      <section aria-labelledby="sync-state" className="space-y-3 rounded-2xl border border-line bg-surface p-4">
        <h2 id="sync-state" className="font-semibold text-fg">
          {t("sync.stateTitle")}
        </h2>
        <p className="text-sm text-muted">{t(`sync.phase.${status.phase}`)}</p>
        <p className="text-sm text-muted">{t("sync.pendingCount", { count: status.pending })}</p>
        {status.lastSyncAt ? (
          <p className="text-sm text-muted">
            {t("sync.lastSync", { time: new Date(status.lastSyncAt).toLocaleTimeString() })}
          </p>
        ) : null}
        {status.problem ? <Alert tone="warn">{t(`sync.problem.${status.problem}`)}</Alert> : null}
        {status.problem === "device_revoked" ? (
          <Button onClick={() => reregister.mutate()} disabled={reregister.isPending}>
            {t("sync.reregister")}
          </Button>
        ) : (
          <Button variant="secondary" onClick={syncNow}>
            {t("sync.syncNow")}
          </Button>
        )}
      </section>

      <section aria-labelledby="sync-conflicts" className="space-y-3">
        <h2 id="sync-conflicts" className="font-semibold text-fg">
          {t("sync.conflictsTitle")}
        </h2>
        {conflicts.data && conflicts.data.length === 0 ? (
          <p className="text-sm text-muted">{t("sync.noConflicts")}</p>
        ) : null}
        {resolve.isError ? <Alert tone="crit">{t("sync.resolveError")}</Alert> : null}
        <ul className="space-y-3">
          {conflicts.data?.map((item) => (
            <li key={item.id} className="space-y-3 rounded-2xl border border-line bg-surface p-4">
              <p className="font-medium text-fg">
                {t(`sync.entities.${item.entity}`, { defaultValue: item.entity })} · {fieldLabel(item)}
              </p>
              <dl className="grid gap-2 sm:grid-cols-2">
                <div className="rounded-xl bg-surface-2 p-3">
                  <dt className="text-xs text-muted">{t("sync.serverValue")}</dt>
                  <dd className="break-words text-fg">{show(item.serverValue)}</dd>
                </div>
                <div className="rounded-xl bg-surface-2 p-3">
                  <dt className="text-xs text-muted">{t("sync.yourValue")}</dt>
                  <dd className="break-words text-fg">{show(item.clientValue)}</dd>
                </div>
              </dl>
              <div className="flex flex-wrap gap-3">
                <Button
                  variant="secondary"
                  disabled={resolve.isPending}
                  onClick={() => resolve.mutate({ conflictId: item.id, resolution: "server" })}
                >
                  {t("sync.keepServer")}
                </Button>
                <Button
                  disabled={resolve.isPending}
                  onClick={() => resolve.mutate({ conflictId: item.id, resolution: "client" })}
                >
                  {t("sync.useMine")}
                </Button>
              </div>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted">{t("sync.resolveNeedsNetwork")}</p>
      </section>

      {failed.data && failed.data.length > 0 ? (
        <section aria-labelledby="sync-failed" className="space-y-3">
          <h2 id="sync-failed" className="font-semibold text-fg">
            {t("sync.failedTitle")}
          </h2>
          <p className="text-sm text-muted">{t("sync.failedBody")}</p>
          <ul className="space-y-3">
            {failed.data.map((item) => (
              <li key={item.opId} className="flex items-center justify-between gap-3 rounded-2xl border border-line bg-surface p-4">
                <span className="min-w-0 text-fg">
                  {t(`sync.entities.${item.entity}`, { defaultValue: item.entity })} ·{" "}
                  {t(`sync.reasons.${item.reason ?? "rejected"}`, { defaultValue: t("sync.reasons.rejected") })}
                </span>
                <Button variant="secondary" onClick={() => dismiss.mutate(item.opId)}>
                  {t("sync.dismiss")}
                </Button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </main>
  );
}
