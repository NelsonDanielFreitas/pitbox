import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import {
  listSessions,
  recoveryCodesStatus,
  revokeOtherSessions,
  revokeSession,
  type SessionItem,
} from "@/features/account/data/account-api";
import { isNativePlatform } from "@/shared/lib/platform";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";

import { RecoveryCodesDialog } from "./RecoveryCodesDialog";

const sessionsKey = ["account", "sessions"] as const;
const codesKey = ["account", "recovery-codes"] as const;

/** Below this many codes the page asks for a new set. */
const LOW_CODES = 2;

function useFormatter() {
  const { i18n } = useTranslation();
  return new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" });
}

export function AccountPage() {
  const { t } = useTranslation();
  const session = useSession();
  const offline = session.data?.authenticated === true && session.data.offline === true;

  return (
    <section aria-labelledby="account-title" className="space-y-8">
      <div>
        <h1 id="account-title" className="text-2xl font-semibold text-fg">
          {t("account.title")}
        </h1>
        <p className="mt-1 text-sm text-muted">{t("account.subtitle")}</p>
      </div>
      {offline ? (
        <Alert tone="info">{t("account.offline")}</Alert>
      ) : (
        <>
          <RecoverySection />
          <SessionsSection />
          <CredentialsSection />
        </>
      )}
    </section>
  );
}

function RecoverySection() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const status = useQuery({
    queryKey: codesKey,
    queryFn: async ({ signal }) => {
      const result = await recoveryCodesStatus(signal);
      if (result.status !== "ok") throw new Error(result.status);
      return result.unused;
    },
    retry: false,
  });
  const unused = status.data;

  return (
    <div className="rounded-2xl border border-line bg-surface p-5">
      <h2 className="text-lg font-semibold text-fg">{t("account.recoveryTitle")}</h2>
      <p className="mt-1 text-sm text-muted">{t("account.recoveryBody")}</p>
      {status.isError ? <Alert tone="crit" className="mt-3">{t("account.loadError")}</Alert> : null}
      {unused !== undefined ? (
        <p className="mt-3 text-sm text-fg" data-testid="codes-left">
          {t("account.recoveryLeft", { count: unused })}
        </p>
      ) : null}
      {unused !== undefined && unused <= LOW_CODES ? (
        <Alert tone="warn" className="mt-3">
          {unused === 0 ? t("account.recoveryNone") : t("account.recoveryLow")}
        </Alert>
      ) : null}
      <Button className="mt-4" onClick={() => setOpen(true)}>
        {t("account.recoveryNew")}
      </Button>
      <RecoveryCodesDialog
        open={open}
        onClose={() => setOpen(false)}
        onGenerated={() => {
          void queryClient.invalidateQueries({ queryKey: codesKey });
          void queryClient.invalidateQueries({ queryKey: ["session"] });
        }}
      />
    </div>
  );
}

function SessionsSection() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [failed, setFailed] = useState(false);
  const sessions = useQuery({
    queryKey: sessionsKey,
    queryFn: async ({ signal }) => {
      const result = await listSessions(signal);
      if (result.status !== "ok") throw new Error(result.status);
      return result.sessions;
    },
    retry: false,
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: sessionsKey });
  const revoke = useMutation({
    mutationFn: (ref: string) => revokeSession(ref),
    onSuccess: async (result) => {
      setFailed(result.status !== "ok");
      await refresh();
    },
  });
  const revokeOthers = useMutation({
    mutationFn: () => revokeOtherSessions(),
    onSuccess: async (result) => {
      setFailed(result.status !== "ok");
      await refresh();
    },
  });
  const others = (sessions.data ?? []).filter((s) => !s.current);
  const busy = revoke.isPending || revokeOthers.isPending;

  return (
    <div className="rounded-2xl border border-line bg-surface p-5">
      <h2 className="text-lg font-semibold text-fg">{t("account.sessionsTitle")}</h2>
      <p className="mt-1 text-sm text-muted">{t("account.sessionsBody")}</p>
      {sessions.isError ? <Alert tone="crit" className="mt-3">{t("account.loadError")}</Alert> : null}
      {failed ? <Alert tone="crit" className="mt-3">{t("account.actionError")}</Alert> : null}
      {sessions.data ? (
        <ul className="mt-4 divide-y divide-line" aria-label={t("account.sessionsTitle")}>
          {sessions.data.map((s) => (
            <SessionRow key={s.ref} item={s} busy={busy} onRevoke={() => revoke.mutate(s.ref)} />
          ))}
        </ul>
      ) : null}
      {others.length > 0 ? (
        <Button className="mt-4" variant="secondary" disabled={busy} onClick={() => revokeOthers.mutate()}>
          {t("account.sessionsEndOthers")}
        </Button>
      ) : null}
    </div>
  );
}

function SessionRow({ item, busy, onRevoke }: { item: SessionItem; busy: boolean; onRevoke: () => void }) {
  const { t } = useTranslation();
  const format = useFormatter();
  const label = item.kind === "mobile" ? t("account.kindMobile") : t("account.kindWeb");
  return (
    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
      <div className="min-w-0">
        <p className="text-sm font-medium text-fg">
          {label}
          {item.current ? <span className="ml-2 rounded-full bg-surface-2 px-2 py-0.5 text-xs">{t("account.current")}</span> : null}
        </p>
        <p className="truncate text-xs text-muted" title={item.user_agent}>
          {item.user_agent || t("account.unknownDevice")}
        </p>
        <p className="text-xs text-muted">
          {t("account.since", { date: format.format(new Date(item.created_at)) })}
          {item.ip ? ` · ${item.ip}` : ""}
        </p>
      </div>
      {item.current ? null : (
        <Button variant="danger" disabled={busy} aria-label={t("account.endSessionOf", { device: label })} onClick={onRevoke}>
          {t("account.endSession")}
        </Button>
      )}
    </li>
  );
}

function CredentialsSection() {
  const { t } = useTranslation();
  return (
    <div className="rounded-2xl border border-line bg-surface p-5">
      <h2 className="text-lg font-semibold text-fg">{t("account.credentialsTitle")}</h2>
      <p className="mt-1 text-sm text-muted">{t("account.credentialsBody")}</p>
      {isNativePlatform() ? (
        <p className="mt-3 text-sm text-muted">{t("account.credentialsWebOnly")}</p>
      ) : (
        <a className="mt-3 inline-block text-sm underline" href="/auth/account-security" target="_blank" rel="noopener noreferrer">
          {t("account.credentialsLink")}
        </a>
      )}
    </div>
  );
}
