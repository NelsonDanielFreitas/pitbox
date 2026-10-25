import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import { en } from "./resources.en";
import { ptPT } from "./resources.pt-PT";

export type Locale = "pt-PT" | "en";

const storageKey = "pitbox.locale";

function readStoredLocale(): string | null {
  try {
    return window.localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

function storeLocale(locale: Locale): void {
  try {
    window.localStorage.setItem(storageKey, locale);
  } catch {
    // Storage can be blocked (private mode, policy). The app still works in memory.
  }
}

function initialLocale(): Locale {
  const stored = readStoredLocale();
  if (stored === "en" || stored === "pt-PT") {
    return stored;
  }
  return "pt-PT";
}

void i18n.use(initReactI18next).init({
  resources: {
    "pt-PT": { translation: ptPT },
    en: { translation: en },
  },
  lng: initialLocale(),
  fallbackLng: "pt-PT",
  interpolation: { escapeValue: false },
  initAsync: false,
});

document.documentElement.lang = i18n.language;

export function setLocale(locale: Locale): void {
  void i18n.changeLanguage(locale);
  storeLocale(locale);
  document.documentElement.lang = locale;
}

export { i18n };
