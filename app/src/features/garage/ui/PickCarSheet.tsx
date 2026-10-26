import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Spinner } from "@/shared/ui/Spinner";

import type { Car } from "../domain/models";

export function PickCarSheet({
  open,
  cars,
  currentCarId,
  occupiedCarIds,
  loading,
  onSelect,
  onClose,
}: {
  open: boolean;
  cars: Car[];
  currentCarId?: string;
  occupiedCarIds?: ReadonlySet<string>;
  loading: boolean;
  onSelect: (carId: string) => void;
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
      aria-labelledby="pick-car-title"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClose={onClose}
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-2xl border border-line bg-surface p-5 text-fg shadow-xl backdrop:bg-black/70"
    >
      <h2 id="pick-car-title" className="text-lg font-semibold">
        {t("garage.mount.pickCar")}
      </h2>
      {loading ? (
        <div aria-busy="true" role="status" className="mt-4 flex items-center gap-3 text-muted">
          <Spinner />
          <span>{t("garage.loading")}</span>
        </div>
      ) : cars.length === 0 ? (
        <p className="mt-4 text-sm text-muted">{t("garage.mount.noCars")}</p>
      ) : (
        <ul className="mt-4 grid gap-2">
          {cars.map((car) => {
            const isCurrent = car.id === currentCarId;
            const occupied =
              !isCurrent && (car.status === "retired" || occupiedCarIds?.has(car.id) === true);
            return (
              <li key={car.id}>
                <button
                  type="button"
                  disabled={occupied}
                  onClick={() => onSelect(car.id)}
                  className="flex min-h-14 w-full items-center gap-3 rounded-xl border border-line bg-surface-2 p-3 text-left disabled:opacity-50"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-semibold">
                      {car.nickname ?? `${car.make} ${car.model}`}
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {car.make} {car.model}
                    </span>
                  </span>
                  {occupied ? <Badge tone="neutral">{t("garage.mount.occupied")}</Badge> : null}
                  {isCurrent ? <Badge tone="ok">{t("garage.statuses.mounted")}</Badge> : null}
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
