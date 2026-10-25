import type { QueryClient } from "@tanstack/react-query";

import { sessionQueryKey } from "@/auth/session-query";

export function installBfcacheRevalidation(queryClient: QueryClient): () => void {
  const onPageShow = (event: PageTransitionEvent) => {
    if (event.persisted) {
      void queryClient.invalidateQueries({ queryKey: sessionQueryKey });
    }
  };
  window.addEventListener("pageshow", onPageShow);
  return () => window.removeEventListener("pageshow", onPageShow);
}
