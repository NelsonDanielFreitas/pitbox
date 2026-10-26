import type { ReactNode } from "react";

export function FormActions({ children }: { children: ReactNode }) {
  return (
    <div className="sticky bottom-0 z-20 -mx-4 mt-8 flex gap-3 border-t border-line bg-bg/95 px-4 pt-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] backdrop-blur sm:-mx-6 sm:px-6">
      {children}
    </div>
  );
}
