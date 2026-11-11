import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { requestRecovery, type RecoveryOutcome } from "@/features/auth/data/recovery";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";
import { TextInput } from "@/shared/ui/TextInput";

const messageKey = {
  accepted: "auth.recoverDone",
  invalid: "auth.recoverInvalid",
  rateLimited: "auth.recoverRateLimited",
  unavailable: "auth.recoverUnavailable",
} as const satisfies Record<RecoveryOutcome, string>;

/** "I lost my passkey": asks for an email with a link to register a new one (ADR 0034). */
export function RecoverPasskey() {
  const { t } = useTranslation();
  const inputId = useId();
  const codeId = useId();
  const codeHintId = useId();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<RecoveryOutcome | null>(null);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setOutcome(null);
    try {
      setOutcome(await requestRecovery(email, code));
    } finally {
      setCode(""); // a code is for one try: it is not kept on screen
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <Button variant="ghost" className="mt-4" onClick={() => setOpen(true)}>
        {t("auth.lostPasskey")}
      </Button>
    );
  }
  return (
    <form onSubmit={(event) => void submit(event)} className="mt-6 flex w-full flex-col gap-3 text-left" noValidate>
      <h2 className="text-lg font-semibold text-fg">{t("auth.recoverTitle")}</h2>
      <p className="text-sm text-muted">{t("auth.recoverBody")}</p>
      <label htmlFor={inputId} className="text-sm font-medium text-fg">
        {t("auth.recoverEmail")}
      </label>
      <TextInput
        id={inputId}
        type="email"
        inputMode="email"
        autoComplete="email"
        required
        maxLength={254}
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        invalid={outcome === "invalid"}
      />
      <label htmlFor={codeId} className="text-sm font-medium text-fg">
        {t("auth.recoverCode")}
      </label>
      <TextInput
        id={codeId}
        type="text"
        autoComplete="off"
        autoCapitalize="characters"
        spellCheck={false}
        maxLength={64}
        aria-describedby={codeHintId}
        value={code}
        onChange={(event) => setCode(event.target.value)}
        invalid={outcome === "invalid"}
      />
      <p id={codeHintId} className="text-sm text-muted">
        {t("auth.recoverCodeHint")}
      </p>
      {outcome ? (
        <Alert tone={outcome === "accepted" ? "ok" : "warn"}>{t(messageKey[outcome])}</Alert>
      ) : null}
      <Button type="submit" disabled={busy || email.trim() === ""}>
        {busy ? t("auth.recoverBusy") : t("auth.recoverSubmit")}
      </Button>
    </form>
  );
}
