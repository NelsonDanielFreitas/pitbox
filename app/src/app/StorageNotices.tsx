import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { requestPersistentStorage } from "@/db/persistence";
import { Alert } from "@/shared/ui/Alert";
import { Button } from "@/shared/ui/Button";


/** Offline-session banner and the (discreet) warning when the browser refuses persistence. */
export function StorageNotices() {
  const { t } = useTranslation();
  const session = useSession();
  const offline = session.data?.authenticated === true && session.data.offline === true;
  const [denied, setDenied] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    let active = true;
    // After the first login the app is in use and the database is open: ask once.
    void requestPersistentStorage().then((persistent) => {
      if (active) setDenied(!persistent);
    });
    return () => {
      active = false;
    };
  }, []);

  return (
    <>
      {offline ? (
        <Alert tone="warn" role="note" className="mb-4">
          {t("offline.banner")}
        </Alert>
      ) : null}
      {denied && !dismissed ? (
        <Alert tone="info" role="note" className="mb-4">
          <div className="flex items-center justify-between gap-3">
            <p>{t("pwa.persistDenied")}</p>
            <Button
              variant="ghost"
              onClick={() => setDismissed(true)}
            >
              {t("pwa.dismiss")}
            </Button>
          </div>
        </Alert>
      ) : null}
    </>
  );
}
