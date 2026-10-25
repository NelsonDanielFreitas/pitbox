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
            "rounded-md px-2 py-1 text-xs font-semibold uppercase tracking-wide",
            i18n.language === locale
              ? "bg-pitbox-ink text-white"
              : "text-pitbox-muted hover:bg-slate-100",
          )}
          onClick={() => setLocale(locale)}
        >
          {locale === "pt-PT" ? "PT" : "EN"}
        </button>
      ))}
    </div>
  );
}
