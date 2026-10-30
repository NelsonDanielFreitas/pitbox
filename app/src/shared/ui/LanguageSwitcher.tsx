import { useTranslation } from "react-i18next";

import { setLocale, type Locale } from "@/i18n";
import { cn } from "@/shared/lib/cn";

const locales: Locale[] = ["pt-PT", "en"];

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  return (
    <div aria-label={t("language.label")} className="flex items-center gap-1" role="group">
      {locales.map((locale) => (
        <button
          key={locale}
          type="button"
          aria-pressed={i18n.language === locale}
          className={cn(
            "min-h-12 min-w-11 rounded-xl px-2 text-xs font-semibold uppercase tracking-wide transition-colors",
            i18n.language === locale
              ? "bg-primary text-on-primary"
              : "text-muted hover:bg-surface-2 hover:text-fg",
          )}
          onClick={() => setLocale(locale)}
        >
          {locale === "pt-PT" ? "PT" : "EN"}
        </button>
      ))}
    </div>
  );
}
