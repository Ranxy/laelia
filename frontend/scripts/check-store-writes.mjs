// frontend/scripts/check-store-writes.mjs
//
// Enforces the store write-surface policy (audit 05 §7.3-7): only store
// slices, the sanctioned unauthenticated-redirect interceptor in src/connect/,
// and tests may call useAppStore.setState directly. Components and pages must
// update state through slice actions instead.
//
// This is a conservative textual guardrail, not a complete TypeScript
// evaluator (same stance as check-react-layering.mjs). It matches
// useAppStore.setState( call text, namespaced forms such as
// mod.useAppStore.setState(, and local aliases bound from "@/stores" imports.
// Renamed re-exports, indirection through helper functions, or shadowed
// bindings may be unresolved; passing this check does not permit bypassing
// the policy.

import { readFileSync, readdirSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");
const SRC_DIR = resolve(ROOT, "src");
const REPORT_ONLY = process.argv.includes("--report-only");

// Where direct writes ARE allowed:
// - src/stores/**  — store slices (each slice owns its own state surgery)
// - src/connect/** — the unauthenticated-redirect interceptor, which clears
//   the store on a mid-session 401 via a dynamic import of "@/stores".
const ALLOWED_PREFIXES = [
  "src/stores/",
  "src/connect/",
];

const DIRECT_WRITE_PATTERN = /\buseAppStore\s*\.\s*setState\s*\(/g;

// Aliases that bind the same store object under another local name; writes
// through such an alias are still direct store surgery:
// - `import { useAppStore as store } from "@/stores"` (static, renamed)
// - `const { useAppStore: store } = await import("@/stores")` (dynamic)
// A namespaced binding (`const mod = await import("@/stores")`) needs no
// alias tracking because `mod.useAppStore.setState(` already matches
// DIRECT_WRITE_PATTERN textually.
const ALIAS_IMPORT_PATTERNS = [
  () =>
    /import\s*\{\s*([^}]*)\}\s*from\s*(["'])@\/stores\2/g,
  () =>
    /const\s*\{\s*([^}]*?)\s*\}\s*=\s*(?:await\s+)?import\s*\(\s*(["'])@\/stores\2\s*\)/g,
];

const findFiles = (dir) => {
  const files = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = resolve(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...findFiles(full));
    } else if (/\.(ts|tsx)$/.test(entry.name)) {
      files.push(full);
    }
  }
  return files;
};

const isWriteAllowedPath = (path) =>
  ALLOWED_PREFIXES.some((prefix) => path.startsWith(prefix));

const isTestFile = (path) =>
  /\.test\.tsx?$/.test(path) || /\.spec\.[jt]sx?$/.test(path);

const importsVitest = (source) =>
  /(?:\bimport\s*\(\s*|\bfrom\s+|\brequire\s*\(\s*)(["'])vitest(?:\/[\w./-]+)?\1/.test(
    source
  );

const buildLineStarts = (source) => {
  const lineStarts = [0];
  for (
    let index = source.indexOf("\n");
    index !== -1;
    index = source.indexOf("\n", index + 1)
  ) {
    lineStarts.push(index + 1);
  }
  return lineStarts;
};

const getLineNumber = (lineStarts, index) => {
  let low = 0;
  let high = lineStarts.length - 1;
  while (low <= high) {
    const mid = Math.floor((low + high) / 2);
    if (lineStarts[mid] <= index) {
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }
  return high + 1;
};

const collectStoreAliases = (source) => {
  const aliases = new Set();
  const addAlias = (name) => {
    // The unpreserved name is already covered by DIRECT_WRITE_PATTERN.
    if (name && name !== "useAppStore") {
      aliases.add(name);
    }
  };
  for (const makePattern of ALIAS_IMPORT_PATTERNS) {
    // Fresh regex per call: a shared /g regex would carry lastIndex across
    // files and silently skip matches.
    const pattern = makePattern();
    let match;
    while ((match = pattern.exec(source))) {
      for (const binding of match[1].split(",")) {
        // Static import: `useAppStore as store`.
        const asForm = binding.match(/\buseAppStore\s+as\s+([A-Za-z_$][\w$]*)/);
        if (asForm) {
          addAlias(asForm[1]);
          continue;
        }
        // Dynamic destructure: `const { useAppStore: store } = await ...`.
        const renamedForm = binding.match(/\buseAppStore\s*:\s*([A-Za-z_$][\w$]*)/);
        if (renamedForm) {
          addAlias(renamedForm[1]);
        }
      }
    }
  }
  return aliases;
};

const scanSource = (source, rel) => {
  if (isWriteAllowedPath(rel)) {
    return [];
  }
  // Tests seeding store state are legitimate; a file importing vitest is
  // treated as test code even when the name does not end in .test/.spec.
  if (isTestFile(rel) || importsVitest(source)) {
    return [];
  }

  const violations = [];
  const lines = source.split("\n");
  const lineStarts = buildLineStarts(source);
  const report = (index, reason) => {
    const lineNumber = getLineNumber(lineStarts, index);
    violations.push({
      rel,
      lineNumber,
      reason,
      line: lines[lineNumber - 1] ?? "",
    });
  };

  // Direct write pattern covers `useAppStore.setState(` and namespaced
  // `mod.useAppStore.setState(` forms.
  const direct = new RegExp(DIRECT_WRITE_PATTERN.source, "g");
  let match;
  while ((match = direct.exec(source))) {
    report(
      match.index,
      "direct useAppStore.setState outside the store write surface"
    );
  }

  // Writes through renamed bindings of the same store ("useAppStore as x").
  for (const alias of collectStoreAliases(source)) {
    const aliased = new RegExp(
      `\\b${alias.replace(/\$/g, "\\$")}\\s*\\.\\s*setState\\s*\\(`,
      "g"
    );
    while ((match = aliased.exec(source))) {
      report(
        match.index,
        `direct store write via useAppStore alias "${alias}" outside the store write surface`
      );
    }
  }

  return violations;
};

const scanFile = (file) => {
  const rel = relative(ROOT, file).replace(/\\/g, "/");
  const source = readFileSync(file, "utf-8");
  return scanSource(source, rel);
};

const scanStoreWrites = () => findFiles(SRC_DIR).flatMap(scanFile);

const main = () => {
  const violations = scanStoreWrites();

  if (violations.length > 0) {
    console.error(
      `Store write-surface policy violations (${violations.length}).\n` +
        "Only store slices own direct store writes; components and pages " +
        "must update state through slice actions.\n" +
        "Direct useAppStore.setState calls are allowed ONLY in:\n" +
        "  - src/stores/**  (store slices)\n" +
        "  - src/connect/** (the unauthenticated-redirect interceptor in src/connect/index.ts)\n" +
        "  - tests          (*.test.ts, *.test.tsx, *.spec.*, or files importing vitest)\n"
    );
    for (const violation of violations) {
      console.error(
        `${violation.rel}:${violation.lineNumber}: ${violation.reason}\n` +
          `  ${violation.line.trim()}\n`
      );
    }
    if (!REPORT_ONLY) {
      process.exit(1);
    }
  }

  console.log(
    violations.length === 0
      ? "Store write-surface policy: all checks passed."
      : "Store write-surface policy: report-only mode completed with violations."
  );
};

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}