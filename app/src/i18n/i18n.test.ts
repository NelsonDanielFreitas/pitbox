import { afterEach, describe, expect, it } from "vitest";

import { i18n, setLocale } from ".";

describe("i18n", () => {
  afterEach(() => {
    setLocale("pt-PT");
    window.localStorage.clear();
  });

  it("switches to English and updates the document language", async () => {
    setLocale("en");
    await i18n.changeLanguage("en");
    expect(i18n.t("teams.title")).toBe("My teams");
    expect(document.documentElement.lang).toBe("en");
    expect(window.localStorage.getItem("pitbox.locale")).toBe("en");
  });
});
