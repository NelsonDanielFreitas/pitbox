import { createRouter } from "@tanstack/react-router";

import { routeTree } from "./routeTree.gen";
import type { RouterContext } from "./router-context";

export function createAppRouter(context: RouterContext) {
  return createRouter({
    routeTree,
    context,
    defaultPreload: "intent",
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
