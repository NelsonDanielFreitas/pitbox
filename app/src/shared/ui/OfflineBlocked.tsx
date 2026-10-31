import { useTranslation } from "react-i18next";

import { Alert } from "./Alert";
import { Button } from "./Button";

/** Cold start without network and without a valid offline pass: no data is shown (ADR 0028). */
export function OfflineBlocked() {
  const { t } = useTranslation();
  return (
    <main className="mx-auto max-w-5xl px-4 py-10">
      <Alert tone="warn" title={t("offline.blockedTitle")}>
        <p>{t("offline.blockedBody")}</p>
        <div className="mt-4">
          <Button variant="secondary" onClick={() => window.location.reload()}>
            {t("offline.retry")}
          </Button>
        </div>
      </Alert>
    </main>
  );
}
