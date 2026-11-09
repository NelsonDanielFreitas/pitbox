import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const registerSW = vi.fn();
const isNative = vi.fn<() => boolean>();

vi.mock("virtual:pwa-register/react", () => ({
  useRegisterSW: () => {
    registerSW();
    return {
      needRefresh: [true, () => undefined] as const,
      updateServiceWorker: () => Promise.resolve(),
    };
  },
}));
vi.mock("@/shared/lib/platform", () => ({ isNativePlatform: () => isNative() }));
vi.mock("@/features/sync/data/use-sync", () => ({ useSyncStatus: () => ({ phase: "idle" }) }));

import { UpdatePrompt } from "./UpdatePrompt";

describe("UpdatePrompt", () => {
  beforeEach(() => {
    registerSW.mockClear();
  });

  it("never registers a service worker inside the native shell", () => {
    isNative.mockReturnValue(true);
    const { container } = render(<UpdatePrompt />);
    expect(registerSW).not.toHaveBeenCalled();
    expect(container).toBeEmptyDOMElement();
  });

  it("registers the service worker in the browser", () => {
    isNative.mockReturnValue(false);
    render(<UpdatePrompt />);
    expect(registerSW).toHaveBeenCalledTimes(1);
  });
});
