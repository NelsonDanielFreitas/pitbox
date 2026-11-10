import { useState } from "react";
import { useTranslation } from "react-i18next";

import { stepUpHref } from "@/auth/session";
import { useSession } from "@/auth/session-query";
import { generateRecoveryCodes, type RecoveryCodesResult } from "@/features/auth/data/recovery-codes";
import { isNativePlatform } from "@/shared/lib/platform";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { Dialog } from "@/shared/ui/Dialog";

/**
 * Standing warning for an account whose only credential is a passkey and that has no recovery
 * code (ADR 0034). The server says so with `recovery_ready: false`; when it does not know it
 * leaves the field out and nothing is shown.
 */
export function RecoveryNotice() {
  const { t } = useTranslation();
  const session = useSession();
  const native = isNativePlatform();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<RecoveryCodesResult | null>(null);
  const [done, setDone] = useState(false);

  const data = session.data;
  const warn = data?.authenticated === true && !data.offline && data.recovery_ready === false;
  if (!warn && !done) return null;

  const generate = async () => {
    setBusy(true);
    try {
      setResult(await generateRecoveryCodes());
    } finally {
      setBusy(false);
    }
  };

  const close = () => {
    // The codes are not kept: closing the dialog forgets them.
    if (result?.status === "ok") setDone(true);
    setResult(null);
    setOpen(false);
  };

  return (
    <>
      {done ? null : (
        <Alert tone="warn" title={t("auth.recoveryNoticeTitle")} role="note" className="mb-4">
          <p>{t("auth.recoveryNoticeBody")}</p>
          {native ? (
            <p className="mt-2">{t("auth.recoveryNoticeWebOnly")}</p>
          ) : (
            <Button className="mt-3" onClick={() => setOpen(true)}>
              {t("auth.recoveryGenerate")}
            </Button>
          )}
        </Alert>
      )}
      <Dialog open={open} title={t("auth.recoveryDialogTitle")} description={t("auth.recoveryDialogBody")} onClose={close}>
        {result?.status === "ok" ? (
          <>
            <ul className="grid grid-cols-1 gap-1 font-mono text-base text-fg" aria-label={t("auth.recoveryCodesList")}>
              {result.codes.map((code) => (
                <li key={code}>{code}</li>
              ))}
            </ul>
            <p className="mt-3 text-sm text-muted">{t("auth.recoveryCodesOnce")}</p>
            <Button className="mt-4" full onClick={close}>
              {t("auth.recoverySaved")}
            </Button>
          </>
        ) : (
          <>
            {result?.status === "stepUp" ? (
              <Alert tone="warn" className="mb-3">
                {t("auth.recoveryStepUp")}{" "}
                <a className="underline" href={stepUpHref(window.location.pathname)}>
                  {t("auth.recoveryStepUpLink")}
                </a>
              </Alert>
            ) : null}
            {result && result.status !== "stepUp" ? (
              <Alert tone="crit" className="mb-3">
                {t("auth.recoveryError")}
              </Alert>
            ) : null}
            <div className="flex gap-2">
              <Button variant="ghost" onClick={close}>
                {t("auth.cancel")}
              </Button>
              <Button disabled={busy} onClick={() => void generate()}>
                {busy ? t("auth.recoveryGenerating") : t("auth.recoveryGenerate")}
              </Button>
            </div>
          </>
        )}
      </Dialog>
    </>
  );
}
