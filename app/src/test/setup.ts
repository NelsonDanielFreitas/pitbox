import "@testing-library/jest-dom/vitest";
import "@/i18n";

import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

if (typeof window !== "undefined") {
  Object.defineProperty(window, "scrollTo", { configurable: true, value: vi.fn() });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
