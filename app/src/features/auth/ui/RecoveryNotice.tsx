import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { RecoveryCodesDialog } from "@/features/account/ui/RecoveryCodesDialog";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";

/**
 * Standing warning for an account whose only credential is a passkey and that has no recovery
 * code (ADR 0034). The server says so with `recovery_ready: false`; when it does not know it
 * leaves the field out and nothing is shown. The codes can be generated here or on the
 * account page, on the web and on the phone.
 */
export function RecoveryNotice() {
  const { t } = useTranslation();
  const session = useSession();
  const [open, setOpen] = useState(false);
  const [done, setDone] = useState(false);

  const data = session.data;
  const warn = data?.authenticated === true && !data.offline && data.recovery_ready === false;
  if (!warn && !done) return null;

  return (
    <>
      {done ? null : (
        <Alert tone="warn" title={t("auth.recoveryNoticeTitle")} role="note" className="mb-4">
          <p>{t("auth.recoveryNoticeBody")}</p>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <Button onClick={() => setOpen(true)}>{t("auth.recoveryGenerate")}</Button>
            <Link to="/account" className="text-sm underline">
              {t("account.openPage")}
            </Link>
          </div>
        </Alert>
      )}
      <RecoveryCodesDialog open={open} onClose={() => setOpen(false)} onGenerated={() => setDone(true)} />
    </>
  );
}
