import { useId, type ReactNode } from "react";

import { useModalDialog } from "@/shared/lib/use-modal-dialog";

export function Sheet({
  open,
  title,
  description,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  description?: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const dialogRef = useModalDialog(open);
  const titleId = useId();
  const descriptionId = useId();

  return (
    <dialog
      ref={dialogRef}
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      className="fixed inset-x-0 bottom-0 m-0 max-h-[90dvh] w-full max-w-none overflow-y-auto rounded-t-3xl border border-line bg-surface p-0 text-fg shadow-xl backdrop:bg-black/70 sm:inset-0 sm:m-auto sm:max-w-lg sm:rounded-2xl"
    >
      <div className="mx-auto w-full max-w-lg p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom))]">
        <div aria-hidden="true" className="mx-auto mb-4 h-1 w-10 rounded-full bg-muted/40 sm:hidden" />
        <h2 id={titleId} className="text-xl font-semibold">
          {title}
        </h2>
        {description ? (
          <p id={descriptionId} className="mt-2 text-sm text-muted">
            {description}
          </p>
        ) : null}
        <div className="mt-5">{children}</div>
      </div>
    </dialog>
  );
}
