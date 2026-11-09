import { App } from "@capacitor/app";
import { useQueryClient } from "@tanstack/react-query";
import { PitboxSecure } from "pitbox-secure";
import { useCallback, useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { sessionQueryKey } from "@/auth/session-key";
import { closeDatabase } from "@/db/client";
import { AppLockController } from "@/native/app-lock";
import { isNativePlatform } from "@/shared/lib/platform";
import { Button } from "@/shared/ui/Button";

/**
 * Locks the Android app behind the phone's biometrics or PIN (spec §3.4, ADR 0033). On the web
 * there is no lock: the session cookie is the gate there.
 */
export function AppLockGate({ children }: { children: ReactNode }) {
  return isNativePlatform() ? <NativeAppLock>{children}</NativeAppLock> : children;
}

type Outcome = "ok" | "unavailable" | "failed";
type Attempt = "idle" | "asking" | "unavailable" | "failed";

/** Anything but a success keeps the app locked. The error code only picks the message. */
async function authenticate(title: string): Promise<Outcome> {
  try {
    await PitboxSecure.authenticate({ title });
    return "ok";
  } catch (error) {
    const code = typeof error === "object" && error !== null && "code" in error ? error.code : undefined;
    return code === "BIOMETRIC_UNAVAILABLE" ? "unavailable" : "failed";
  }
}

function NativeAppLock({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [controller] = useState(() => new AppLockController());
  const lockState = useSyncExternalStore(controller.subscribe, controller.getState);
  const [attempt, setAttempt] = useState<Attempt>("idle");

  useEffect(() => {
    const handle = App.addListener("appStateChange", ({ isActive }) => {
      if (isActive) controller.cameToForeground();
      else controller.goneToBackground();
    });
    return () => {
      void handle.then((h) => h.remove());
    };
  }, [controller]);

  // Locking drops everything readable: the views unmount (and stop the sync), the query cache is
  // emptied and the database connection is closed. Opening it again needs the unlock.
  useEffect(() => {
    if (lockState !== "locked") return;
    // Not clear(): that would also drop the session query the layout above this gate reads,
    // which unmounts the gate, which mounts locked again and clears again, forever. The session
    // holds only the user id; everything else readable goes.
    queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== sessionQueryKey[0] });
    void closeDatabase().catch(() => undefined);
  }, [lockState, queryClient]);

  const prompt = t("lock.prompt");
  const settle = useCallback(
    (outcome: Outcome) => {
      if (outcome === "ok") controller.unlock();
      setAttempt(outcome === "ok" ? "idle" : outcome);
    },
    [controller],
  );

  // Ask as soon as the lock screen is there; the button repeats it after a cancel.
  useEffect(() => {
    if (lockState !== "locked") return undefined;
    let current = true;
    void authenticate(prompt).then((outcome) => {
      if (current) settle(outcome);
    });
    return () => {
      current = false;
    };
  }, [lockState, prompt, settle]);

  if (lockState === "unlocked") return children;

  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 px-6 text-center">
      <h1 className="text-2xl font-bold">{t("lock.title")}</h1>
      <p className="max-w-sm text-muted">
        {attempt === "unavailable"
          ? t("lock.unavailable")
          : attempt === "failed"
            ? t("lock.failed")
            : t("lock.body")}
      </p>
      <Button size="lg" onClick={() => {
          setAttempt("asking");
          void authenticate(prompt).then(settle);
        }} disabled={attempt === "asking"}>
        {attempt === "failed" || attempt === "unavailable" ? t("lock.retry") : t("lock.unlock")}
      </Button>
    </main>
  );
}
