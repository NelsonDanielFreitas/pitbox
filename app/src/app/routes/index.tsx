import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  beforeLoad: () => {
    // The diagnostics build of the Android app opens on that page: a phone has no address bar.
    throw redirect({ to: import.meta.env.VITE_DIAGNOSTICS === "1" ? "/diagnostics" : "/teams" });
  },
});
