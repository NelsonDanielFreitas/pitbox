import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";

export function RetireDialog({
  open,
  type,
  onUnmount,
  onClose,
}: {
  open: boolean;
  type: "car" | "engine";
  onUnmount: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const dialogRef = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  return (
    <dialog
      ref={dialogRef}
      role="alertdialog"
      aria-labelledby="retire-dialog-title"
      aria-describedby="retire-dialog-body"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClose={onClose}
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-2xl border border-line bg-surface p-5 text-fg shadow-xl backdrop:bg-black/70"
    >
      <h2 id="retire-dialog-title" className="text-lg font-semibold">
        {t("garage.mount.retireTitle")}
      </h2>
      <p id="retire-dialog-body" className="mt-2 text-sm text-muted">
        {t(type === "car" ? "garage.mount.retireCarBody" : "garage.mount.retireEngineBody")}
      </p>
      <div className="mt-6 flex justify-end gap-3">
        <Button variant="secondary" onClick={onClose}>
          {t("garage.actions.cancel")}
        </Button>
        <Button variant="danger" onClick={onUnmount}>
          {t("garage.actions.unmountNow")}
        </Button>
      </div>
    </dialog>
  );
}
