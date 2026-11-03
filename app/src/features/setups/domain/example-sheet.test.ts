// @vitest-environment node

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { expect, it } from "vitest";

import { parseTemplate } from "./template";

// The sheet that the browser tests (tests/e2e) put in the mock catalogue must be a valid template.
it("the e2e example sheet is a valid template", () => {
  const raw: unknown = JSON.parse(readFileSync(fileURLToPath(new URL("../../../../tests/e2e/fixtures/example-sheet.json", import.meta.url)), "utf8"));
  expect(parseTemplate(raw)).not.toBeNull();
});
