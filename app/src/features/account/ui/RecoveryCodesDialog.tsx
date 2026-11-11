import { useState } from "react";
import { useTranslation } from "react-i18next";

import { stepUpHref } from "@/auth/session";
import { LoginCancelledError, LoginRefusedError, mobileAuth } from "@/auth/native";
import { generateRecoveryCodes, type RecoveryCodesResult } from "@/features/auth/data/recovery-codes";
import { isNativePlatform } from "@/shared/lib/platform";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { Dialog } from "@/shared/ui/Dialog";

type StepUpState = "idle" | "busy" | "weak" | "error";

/**
 * Generates the recovery codes and shows them once. A recent strong factor is needed: on the
 * web the user goes through the identity provider again, in the Android shell the phone
 * reauthenticates its own session (ADR 0032). Closing the dialog forgets the codes.
 */
export function RecoveryCodesDialog({
  open,
  onClose,
  onGenerated,
}: {
  open: boolean;
  onClose: () => void;
  /** Called once when a set was generated (the old set no longer works). */
  onGenerated?: () => void;
}) {
  const { t } = useTranslation();
  const native = isNativePlatform();
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<RecoveryCodesResult | null>(null);
  const [stepUp, setStepUp] = useState<StepUpState>("idle");

  const generate = async () => {
    setBusy(true);
    try {
      const next = await generateRecoveryCodes();
      setResult(next);
      if (next.status === "ok") onGenerated?.();
    } finally {
      setBusy(false);
    }
  };

  const confirmIdentity = async () => {
    setStepUp("busy");
    try {
      await mobileAuth().stepUp();
      setStepUp("idle");
      await generate();
    } catch (error) {
      if (error instanceof LoginCancelledError) setStepUp("idle");
      else if (error instanceof LoginRefusedError && error.reason === "weak_factor") setStepUp("weak");
      else setStepUp("error");
    }
  };

  const close = () => {
    setResult(null);
    setStepUp("idle");
    onClose();
  };

  return (
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
              {native ? (
                <Button
                  variant="secondary"
                  className="mt-2"
                  disabled={stepUp === "busy"}
                  onClick={() => void confirmIdentity()}
                >
                  {stepUp === "busy" ? t("auth.nativeBusy") : t("auth.recoveryStepUpLink")}
                </Button>
              ) : (
                <a className="underline" href={stepUpHref(window.location.pathname)}>
                  {t("auth.recoveryStepUpLink")}
                </a>
              )}
            </Alert>
          ) : null}
          {stepUp === "weak" ? (
            <Alert tone="crit" className="mb-3">
              {t("auth.recoveryStepUpWeak")}
            </Alert>
          ) : null}
          {stepUp === "error" ? (
            <Alert tone="crit" className="mb-3">
              {t("auth.recoveryStepUpError")}
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
  );
}
