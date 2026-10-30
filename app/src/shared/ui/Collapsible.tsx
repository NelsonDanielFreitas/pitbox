import { useId, useState, type ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

/**
 * A titled section the user opens and closes. The panel is only rendered while open, so a
 * long form keeps a small DOM; whoever owns the data keeps the state.
 */
export function Collapsible({
  title,
  summary,
  defaultOpen = false,
  children,
}: {
  title: string;
  summary?: ReactNode;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  const buttonId = `${id}-button`;
  const panelId = `${id}-panel`;

  return (
    <section className="rounded-2xl border border-line bg-surface">
      <h2 className="text-base">
        <button
          id={buttonId}
          type="button"
          aria-expanded={open}
          aria-controls={open ? panelId : undefined}
          onClick={() => setOpen((value) => !value)}
          className="flex min-h-14 w-full items-center justify-between gap-3 rounded-2xl px-4 text-left font-semibold text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        >
          <span className="min-w-0 flex-1">{title}</span>
          {summary ? <span className="shrink-0 text-sm font-medium text-muted">{summary}</span> : null}
          <svg
            aria-hidden="true"
            className={cn("size-5 shrink-0 text-muted transition-transform", open && "rotate-90")}
            viewBox="0 0 24 24"
            fill="none"
          >
            <path d="m9 6 6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      </h2>
      {open ? (
        <div id={panelId} role="region" aria-labelledby={buttonId} className="border-t border-line p-4">
          {children}
        </div>
      ) : null}
    </section>
  );
}
