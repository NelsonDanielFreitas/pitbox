import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { Spinner } from "@/shared/ui/Spinner";
import { Badge } from "@/shared/ui/Badge";

import type { EngineWithMount } from "../domain/models";

export function PickEngineSheet({
  open,
  engines,
  loading,
  onSelect,
  onClose,
}: {
  open: boolean;
  engines: EngineWithMount[];
  loading: boolean;
  onSelect: (engineId: string) => void;
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
      aria-labelledby="pick-engine-title"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClose={onClose}
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-2xl border border-line bg-surface p-5 text-fg shadow-xl backdrop:bg-black/70"
    >
      <h2 id="pick-engine-title" className="text-lg font-semibold">
        {t("garage.mount.pickEngine")}
      </h2>
      {loading ? (
        <div aria-busy="true" role="status" className="mt-4 flex items-center gap-3 text-muted">
          <Spinner />
          <span>{t("garage.loading")}</span>
        </div>
      ) : engines.length === 0 ? (
        <p className="mt-4 text-sm text-muted">{t("garage.mount.noEngines")}</p>
      ) : (
        <ul className="mt-4 grid gap-2">
          {engines.map((engine) => {
            const occupied = engine.currentCar !== null;
            return (
              <li key={engine.id}>
                <button
                  type="button"
                  disabled={occupied || engine.status === "retired"}
                  onClick={() => onSelect(engine.id)}
                  className="flex min-h-14 w-full items-center gap-3 rounded-xl border border-line bg-surface-2 p-3 text-left disabled:opacity-50"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-semibold">
                      {engine.make} {engine.model}
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {engine.serialNumber ?? "—"}
                    </span>
                  </span>
                  {occupied ? <Badge tone="neutral">{t("garage.mount.occupied")}</Badge> : null}
                </button>
              </li>
            );
          })}
        </ul>
      )}
      <div className="mt-5 flex justify-end">
        <Button variant="secondary" onClick={onClose}>
          {t("garage.actions.cancel")}
        </Button>
      </div>
    </dialog>
  );
}
