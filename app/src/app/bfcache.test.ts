import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { installBfcacheRevalidation } from "./bfcache";

describe("installBfcacheRevalidation", () => {
  it("invalidates the session when a page is restored from bfcache", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue();
    const uninstall = installBfcacheRevalidation(queryClient);

    const event = new Event("pageshow");
    Object.defineProperty(event, "persisted", { value: true });
    window.dispatchEvent(event);

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["auth", "session"] });
    uninstall();
  });

  it("does nothing on a normal page show", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue();
    const uninstall = installBfcacheRevalidation(queryClient);

    window.dispatchEvent(new Event("pageshow"));

    expect(invalidate).not.toHaveBeenCalled();
    uninstall();
  });
});
