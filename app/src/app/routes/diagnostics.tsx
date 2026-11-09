import { createFileRoute, notFound } from "@tanstack/react-router";
import { lazy, Suspense } from "react";

// The page exists only in the Android builds made for it (PITBOX_DIAGNOSTICS=1, `make android-diagnostics`). The constant is
// replaced at build time, so in any other build the import below is removed from the bundle.
const Page =
  import.meta.env.VITE_DIAGNOSTICS === "1"
    ? lazy(() => import("@/features/diagnostics/DiagnosticsPage"))
    : null;

export const Route = createFileRoute("/diagnostics")({
  beforeLoad: () => {
    if (!Page) throw notFound();
  },
  component: () =>
    Page ? (
      <Suspense fallback={null}>
        <Page />
      </Suspense>
    ) : null,
});
