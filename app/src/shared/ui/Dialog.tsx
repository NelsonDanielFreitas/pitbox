import { useId, type ReactNode } from "react";

import { useModalDialog } from "@/shared/lib/use-modal-dialog";

export function Dialog({
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
      role="alertdialog"
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-2xl border border-line bg-surface p-5 text-fg shadow-xl backdrop:bg-black/70"
    >
      <h2 id={titleId} className="text-lg font-semibold">
        {title}
      </h2>
      {description ? (
        <p id={descriptionId} className="mt-2 text-sm text-muted">
          {description}
        </p>
      ) : null}
      <div className="mt-5">{children}</div>
    </dialog>
  );
}
