import { useTranslation } from "react-i18next";
import { useRegisterSW } from "virtual:pwa-register/react";

import { useSyncStatus } from "@/features/sync/data/use-sync";
import { Button } from "@/shared/ui/Button";

/**
 * The new service worker waits until the user says so (ADR 0028): no skipWaiting, because a
 * new shell can carry a local migration and must not replace the code mid-write or mid-sync.
 */
export function UpdatePrompt() {
  const { t } = useTranslation();
  const {
    needRefresh: [needRefresh],
    updateServiceWorker,
  } = useRegisterSW();
  const sync = useSyncStatus();

  if (!needRefresh) return null;
  const busy = sync.phase === "syncing";
  return (
    <section
      aria-label={t("pwa.updateAvailable")}
      className="fixed inset-x-3 bottom-3 z-50 mx-auto flex max-w-md flex-col gap-2 rounded-xl border border-info/30 bg-surface p-4 text-sm text-fg shadow-lg"
    >
      <p>{t("pwa.updateAvailable")}</p>
      {busy ? <p className="text-muted">{t("pwa.updateBusy")}</p> : null}
      <Button disabled={busy} onClick={() => void updateServiceWorker(true)}>
        {t("pwa.update")}
      </Button>
    </section>
  );
}
