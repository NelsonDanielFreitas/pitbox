import { afterEach, describe, expect, it } from "vitest";

import { i18n, setLocale } from ".";
import { en } from "./resources.en";
import { ptPT } from "./resources.pt-PT";

function leafKeys(value: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof child === "object" && child !== null
      ? leafKeys(child as Record<string, unknown>, path)
      : [path];
  });
}

describe("i18n", () => {
  it("keeps the Portuguese and English resources in sync", () => {
    expect(leafKeys(en)).toEqual(leafKeys(ptPT));
  });

  afterEach(() => {
    setLocale("pt-PT");
    window.localStorage.clear();
  });

  it("switches to English and updates the document language", async () => {
    setLocale("en");
    await i18n.changeLanguage("en");
    expect(i18n.t("teams.title")).toBe("Choose your team");
    expect(document.documentElement.lang).toBe("en");
    expect(window.localStorage.getItem("pitbox.locale")).toBe("en");
  });
});
