// frontend/scripts/check-react-i18n.mjs
//
// Enforces a strict mapping between source code and the canonical locale
// files (src/locales/en-US.json, src/locales/zh-CN.json):
//   1. Missing keys      — t("key") in code but key not in the locale files
//   2. Unused keys       — key in locale files but not referenced in code
//   3. Consistency       — every locale file must have the exact same key set
//   4. Placeholder syntax — react-i18next uses {{name}}; flag stray single
//                           {name} braces that render literally
//
// Indirect references the checker CAN trace statically:
//   - titleKey/descriptionKey/messageKey/labelKey string literals in data
//     objects and JSX props (e.g. router/route-info.ts, setup-checklist-dialog.tsx)
//   - t(cond ? "a" : "b") ternary literals (e.g. conversation-list.tsx)
// Anything else (template-literal keys, status maps translated via t(labelKey))
// must be listed in DYNAMIC_PREFIXES below with a pointer to the caller.
//
// Usage: node frontend/scripts/check-react-i18n.mjs

import { readFileSync, readdirSync } from "fs";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

// Keys whose usage the checker cannot trace statically — exempt from the
// unused-key check. Matched with String.startsWith, so an entry ending in
// "." is a prefix family (matches every key under it) and an entry without a
// trailing "." matches itself (and anything that starts with it).
const DYNAMIC_PREFIXES = [
  // Template-literal keys: t(`activity.filter-${f}`), t(`reminders.filter-${f}`),
  // t(`tasks.filter-${f}`) in activity-list.tsx / reminder-list.tsx / command-list.tsx.
  "activity.filter-",
  "reminders.filter-",
  "tasks.filter-",
  // t(`auth.sign-up.password-${check.label}`) in pages/auth/signup.tsx.
  "auth.sign-up.password-",
  // t(`agent.lifecycle.${state}`) in pages/dashboard/agents.tsx.
  "agent.lifecycle.",
  // Status maps in lib/{task,reminder,command}-status.ts, translated via
  // t(labelKey) in the badge components (task-status-badge.tsx etc.).
  "channelTask.status-",
  "reminders.status-",
  "command.status-",
  "command.event-",
  // command-event-toolbar.tsx filter map translated via t(FILTER_LABEL_KEY[f]).
  "command.filter-",
  // command-event-ledger.tsx phase map translated via t(PHASE_LABEL_KEY[phase]).
  "command.phase-",
  // command-event-kind.ts output stream kinds translated via t(kind.labelKey).
  "command.stream-",
  // global-search.tsx time-range filter translated via t(timeLabelKey(value)).
  "globalSearch.time-",
  // token-usage-card.tsx rows: { key: "command.token-*" } translated via
  // t(row.key); the literal keys live in a data array, not a t("…") call.
  "command.token-input",
  "command.token-output",
  "command.token-cache-read",
  "command.token-cache-write",
];

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");
const SOURCE_DIR = resolve(ROOT, "src");
const LOCALES_DIR = resolve(ROOT, "src/locales");
const LOCALES = ["en-US", "zh-CN"];

let errors = 0;

function error(msg) {
  console.error(msg);
  errors++;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function findFiles(dir, ext) {
  const results = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = resolve(dir, entry.name);
    if (entry.isDirectory() && !["locales", "proto-es"].includes(entry.name)) {
      results.push(...findFiles(full, ext));
    } else if (entry.isFile() && entry.name.endsWith(ext)) {
      results.push(full);
    }
  }
  return results;
}

function flatten(obj, prefix = "") {
  const result = {};
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "object" && v !== null && !Array.isArray(v)) {
      Object.assign(result, flatten(v, key));
    } else {
      result[key] = v;
    }
  }
  return result;
}

// i18next pluralization: _one/_other suffixes map to the base key in code.
function baseKey(key) {
  return key.replace(/_(one|other|zero|few|many)$/, "");
}

function isDynamic(key) {
  return DYNAMIC_PREFIXES.some((p) => key.startsWith(p));
}

// ---------------------------------------------------------------------------
// Collect translation keys from source files
// ---------------------------------------------------------------------------
function collectSourceKeys() {
  const files = [
    ...findFiles(SOURCE_DIR, ".tsx"),
    ...findFiles(SOURCE_DIR, ".ts"),
  ];
  const keys = new Set();
  // t("key") / t('key') — template literals are dynamic keys, not matched.
  const re = /\bt\(\s*["']([^"']+)["']/g;
  // t(cond ? "a" : "b") — conditional literal pairs.
  const ternaryRe = /\bt\(\s*[^)]*\?\s*["']([^"']+)["']\s*:\s*["']([^"']+)["']/g;
  // titleKey/descriptionKey/messageKey/labelKey literals in data objects and
  // JSX props, translated later via t(variable).
  const keyPropRe =
    /\b(titleKey|descriptionKey|messageKey|labelKey)\s*[:=]\s*["']([^"']+)["']/g;
  for (const file of files) {
    const src = readFileSync(file, "utf-8");
    re.lastIndex = 0;
    let m;
    while ((m = re.exec(src))) keys.add(m[1]);
    ternaryRe.lastIndex = 0;
    while ((m = ternaryRe.exec(src))) {
      keys.add(m[1]);
      keys.add(m[2]);
    }
    keyPropRe.lastIndex = 0;
    while ((m = keyPropRe.exec(src))) keys.add(m[2]);
  }
  return keys;
}

// ---------------------------------------------------------------------------
// Collect keys from a locale file
// ---------------------------------------------------------------------------
function loadLocaleKeys(locale) {
  const main = JSON.parse(
    readFileSync(resolve(LOCALES_DIR, `${locale}.json`), "utf-8")
  );
  return new Set(Object.keys(flatten(main)));
}

// ---------------------------------------------------------------------------
// Check 1: Missing keys (in code but not in locale)
// ---------------------------------------------------------------------------
const sourceKeys = collectSourceKeys();
const enUSKeys = loadLocaleKeys("en-US");

// Effective locale keys: base keys for pluralized entries included.
const effectiveLocaleKeys = new Set(enUSKeys);
for (const key of enUSKeys) {
  effectiveLocaleKeys.add(baseKey(key));
}

const missing = [];
for (const key of sourceKeys) {
  if (effectiveLocaleKeys.has(key)) continue;
  if (isDynamic(key)) continue;
  missing.push(key);
}

if (missing.length > 0) {
  console.error(`Missing keys (${missing.length}) — used in code but not in locale files:\n`);
  for (const key of missing.sort()) {
    error(`  - ${key}`);
  }
  console.error();
}

// ---------------------------------------------------------------------------
// Check 2: Unused keys (in locale but not in code)
// ---------------------------------------------------------------------------
const unused = [];
for (const key of enUSKeys) {
  const base = baseKey(key);
  if (sourceKeys.has(key) || sourceKeys.has(base)) continue;
  if (isDynamic(key)) continue;
  unused.push(key);
}

if (unused.length > 0) {
  console.error(`Unused keys (${unused.length}) — in locale files but not in code:\n`);
  for (const key of unused.sort()) {
    error(`  - ${key}`);
  }
  console.error(
    "\nRemove from frontend/src/locales/ or add to DYNAMIC_PREFIXES if referenced indirectly (helper return, template literal).\n"
  );
}

// ---------------------------------------------------------------------------
// Check 3: Cross-locale consistency (every locale must match en-US key set)
// ---------------------------------------------------------------------------
for (const locale of LOCALES) {
  if (locale === "en-US") continue;
  const localeKeys = loadLocaleKeys(locale);

  const missingInLocale = [...enUSKeys].filter((k) => !localeKeys.has(k));
  const extraInLocale = [...localeKeys].filter((k) => !enUSKeys.has(k));

  if (missingInLocale.length > 0) {
    console.error(`${locale}: missing ${missingInLocale.length} key(s) (present in en-US):\n`);
    for (const key of missingInLocale.sort()) {
      error(`  - ${key}`);
    }
    console.error();
  }

  if (extraInLocale.length > 0) {
    console.error(`${locale}: extra ${extraInLocale.length} key(s) (not in en-US):\n`);
    for (const key of extraInLocale.sort()) {
      error(`  + ${key}`);
    }
    console.error();
  }
}

// ---------------------------------------------------------------------------
// Check 4: Single {name} placeholders (must be {{name}} for react-i18next)
// ---------------------------------------------------------------------------
// react-i18next interpolates {{name}}. A bare {name} (not part of {{name}})
// renders literally and is almost always a copy/paste slip.
const SINGLE_BRACE_RE = /(?<!\{)\{([a-zA-Z_][a-zA-Z0-9_]*)\}(?!\})/g;

function findSingleBracePlaceholders(obj, path = "") {
  const issues = [];
  if (obj && typeof obj === "object" && !Array.isArray(obj)) {
    for (const [k, v] of Object.entries(obj)) {
      issues.push(...findSingleBracePlaceholders(v, path ? `${path}.${k}` : k));
    }
  } else if (typeof obj === "string") {
    SINGLE_BRACE_RE.lastIndex = 0;
    const names = [];
    let m;
    while ((m = SINGLE_BRACE_RE.exec(obj))) names.push(m[1]);
    if (names.length > 0) issues.push({ key: path, value: obj, names });
  }
  return issues;
}

for (const locale of LOCALES) {
  const data = JSON.parse(
    readFileSync(resolve(LOCALES_DIR, `${locale}.json`), "utf-8")
  );
  const issues = findSingleBracePlaceholders(data);
  if (issues.length > 0) {
    console.error(
      `${locale}: ${issues.length} string(s) with single {name} placeholders — react-i18next needs {{name}}:\n`
    );
    for (const { key, value, names } of issues) {
      error(`  - ${key} → ${JSON.stringify(value)} (placeholders: ${names.join(", ")})`);
    }
    console.error();
  }
}

// ---------------------------------------------------------------------------
// Result
// ---------------------------------------------------------------------------
if (errors > 0) {
  process.exit(1);
} else {
  console.log(
    "React i18n: all checks passed (missing keys, unused keys, cross-locale consistency, placeholder syntax)."
  );
}
