import i18next from "i18next";
import { initReactI18next } from "react-i18next";
import enUS from "@/locales/en-US.json";

const STORAGE_KEY = "laelia.language";

// The default locale is bundled statically so the initial render is
// synchronous; every other locale loads on demand via setLocale (or on boot
// when the stored language is non-default). Keeping the second locale out of
// the entry chunk avoids shipping both ~40K JSON bundles to every user.
const KNOWN_LOCALES = ["en-US", "zh-CN"] as const;
type Locale = (typeof KNOWN_LOCALES)[number];

const localeLoaders: Record<Locale, () => Promise<{ default: unknown }>> = {
  "en-US": async () => ({ default: enUS }),
  "zh-CN": () => import("@/locales/zh-CN.json"),
};

function getStoredLocale(): Locale {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      const parsed = JSON.parse(stored) as unknown;
      // Only trust strings that are actually registered — an unknown/removed
      // locale (old build, manual edit, browser extension) must fall back, not
      // crash the loader lookup on boot.
      if (
        typeof parsed === "string" &&
        (KNOWN_LOCALES as readonly string[]).includes(parsed)
      ) {
        return parsed as Locale;
      }
    }
  } catch {
    // ignore corrupted value
  }
  const nav = navigator.language;
  if (nav.startsWith("zh")) return "zh-CN";
  return "en-US";
}

const initialLng = getStoredLocale();

export const i18n = i18next.createInstance();

void i18n.use(initReactI18next).init({
  resources: {
    "en-US": { translation: enUS },
  },
  lng: initialLng,
  fallbackLng: "en-US",
  interpolation: {
    escapeValue: false,
  },
});

// When the boot language is not the bundled default, load its bundle right
// away so the app renders in the user's language as soon as it is ready
// (usually masked by the session loading state).
if (initialLng !== "en-US") {
  void localeLoaders[initialLng]().then((mod) => {
    i18n.addResourceBundle(
      initialLng,
      "translation",
      mod.default as Parameters<typeof i18n.addResourceBundle>[2]
    );
    void i18n.changeLanguage(initialLng);
  });
}

export function setLocale(locale: string) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(locale));
  const load = localeLoaders[locale as Locale];
  if (!load) {
    // Unknown/removed locale — fall back to the always-bundled default rather
    // than throwing on the loader lookup.
    void i18n.changeLanguage("en-US");
    return;
  }
  void load().then((mod) => {
    i18n.addResourceBundle(
      locale,
      "translation",
      mod.default as Parameters<typeof i18n.addResourceBundle>[2]
    );
    void i18n.changeLanguage(locale);
  });
}

export { i18n as default };
