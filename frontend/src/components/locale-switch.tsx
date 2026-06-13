import { useTranslation } from "react-i18next";
import { setLocale } from "@/react/lib/i18n";

type LocaleOption = {
  value: string;
  label: string;
};

const LOCALES: LocaleOption[] = [
  { value: "en-US", label: "English" },
  { value: "zh-CN", label: "中文" },
];

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
