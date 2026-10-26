import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useSession } from "@/auth/session-query";
import { clearLocalUserData } from "@/db/lifecycle";

import { Alert } from "./Alert";
import { Button } from "./Button";

export function AccountMenu() {
  const { t } = useTranslation();
  const session = useSession();
  const logoutForm = useRef<HTMLFormElement>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState(false);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (confirming && !dialog.open) dialog.showModal();
    if (!confirming && dialog.open) dialog.close();
  }, [confirming]);

  const confirmLogout = async () => {
    setPending(true);
    setError(false);
    try {
      if (session.data?.authenticated) {
        await clearLocalUserData(session.data.user_id);
      }
      setConfirming(false);
      logoutForm.current?.requestSubmit();
    } catch {
      setError(true);
      setPending(false);
    }
  };

  return (
    <>
      <form ref={logoutForm} action="/auth/logout" method="post" className="hidden" />
      <DropdownMenu.Root modal={false}>
        <DropdownMenu.Trigger asChild>
          <Button variant="ghost" aria-label={t("auth.account")}>
            {t("auth.account")}
            <span aria-hidden="true">▾</span>
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            sideOffset={8}
            className="z-50 min-w-48 rounded-xl border border-line bg-surface p-1 shadow-lg"
          >
            <DropdownMenu.Item
              className="min-h-12 w-full rounded-lg px-3 text-left text-sm text-fg outline-none hover:bg-surface-2 focus:bg-surface-2"
              onSelect={(event) => {
                event.preventDefault();
                setError(false);
                setConfirming(true);
              }}
            >
              {t("auth.logout")}
            </DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>

      <dialog
        ref={dialogRef}
        role="alertdialog"
        aria-labelledby="logout-dialog-title"
        aria-describedby="logout-dialog-description"
        onCancel={(event) => {
          event.preventDefault();
          setConfirming(false);
        }}
        onClose={() => setConfirming(false)}
        className="m-auto w-[calc(100%-2rem)] max-w-md rounded-2xl border border-line bg-surface p-5 text-fg shadow-xl backdrop:bg-black/70"
      >
        <h2 id="logout-dialog-title" className="text-lg font-semibold text-fg">
          {t("auth.logoutConfirmTitle")}
        </h2>
        <p id="logout-dialog-description" className="mt-2 text-sm text-muted">
          {t("auth.logoutConfirmBody")}
        </p>
        {error ? (
          <Alert tone="crit" className="mt-4">
            {t("auth.logoutError")}
          </Alert>
        ) : null}
        <div className="mt-6 flex justify-end gap-3">
          <Button variant="secondary" disabled={pending} onClick={() => setConfirming(false)}>
            {t("auth.cancel")}
          </Button>
          <Button variant="danger" disabled={pending} onClick={() => void confirmLogout()}>
            {t("auth.logoutConfirmAction")}
          </Button>
        </div>
      </dialog>
    </>
  );
}
