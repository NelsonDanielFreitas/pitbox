import { expect, it } from "vitest";

import { canonicalDevOrigin } from "./canonical-dev-origin";

it("moves a development origin from 127.0.0.1 to localhost", () => {
  expect(canonicalDevOrigin(new URL("http://127.0.0.1:5173/teams?x=1"), true)).toBe(
    "http://localhost:5173/teams?x=1",
  );
});

it("leaves localhost, production and non-local hosts alone", () => {
  expect(canonicalDevOrigin(new URL("http://localhost:5173/"), true)).toBeNull();
  expect(canonicalDevOrigin(new URL("http://127.0.0.1:5173/"), false)).toBeNull();
  expect(canonicalDevOrigin(new URL("https://app.pitbox.pt/"), true)).toBeNull();
});
