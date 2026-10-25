import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import { en } from "./resources.en";
import { ptPT } from "./resources.pt-PT";

export type Locale = "pt-PT" | "en";

const storageKey = "pitbox.locale";

function initialLocale(): Locale {
  const stored = window.localStorage.getItem(storageKey);
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
  window.localStorage.setItem(storageKey, locale);
  document.documentElement.lang = locale;
}

export { i18n };
