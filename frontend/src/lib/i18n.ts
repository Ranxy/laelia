import i18next from "i18next";
import { initReactI18next } from "react-i18next";
import enUS from "@/react/locales/en-US.json";
import zhCN from "@/react/locales/zh-CN.json";

const STORAGE_KEY = "laelia.language";

function getStoredLocale(): string {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      const parsed = JSON.parse(stored) as unknown;
      if (typeof parsed === "string") return parsed;
    }
  } catch {
    // ignore corrupted value
  }
  const nav = navigator.language;
  if (nav.startsWith("zh")) return "zh-CN";
  return "en-US";
}

export const i18n = i18next.createInstance();

void i18n.use(initReactI18next).init({
  resources: {
    "en-US": { translation: enUS },
    "zh-CN": { translation: zhCN },
  },
  lng: getStoredLocale(),
  fallbackLng: "en-US",
  interpolation: {
    escapeValue: false,
  },
});

export function setLocale(locale: string) {
  void i18n.changeLanguage(locale);
  localStorage.setItem(STORAGE_KEY, JSON.stringify(locale));
}

export { i18n as default };
