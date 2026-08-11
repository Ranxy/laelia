import { useTranslation } from "react-i18next";
import { LOCALES, setLocale } from "@/lib/i18n";

export function LocaleSwitch() {
  const { i18n } = useTranslation();

  return (
    <span className="flex items-center gap-x-3 text-sm">
      {LOCALES.map((locale) => (
        <button
          key={locale.value}
          type="button"
          className={
            locale.value === i18n.language
              ? "font-medium text-main"
              : "text-control-light hover:text-control"
          }
          onClick={() => setLocale(locale.value)}
        >
          {locale.label}
        </button>
      ))}
    </span>
  );
}
