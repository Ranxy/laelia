import { describeError } from "./connect-errors";
import { toastManager } from "./toast";

// showErrorToast is the single error-toast outlet (audit 01-D6/09-A-3: error
// presentation was four styles coexisting). Call it from catch blocks:
//
//   try { await rpc() } catch (err) {
//     void showErrorToast(err, t("auth.sign-in.failed"));
//   }
//
// The page passes the already-translated title (it carries intent, e.g.
// "Sign-in failed"); the description always goes through the shared taxonomy
// (permission-denied details, ConnectError codes, raw messages) instead of an
// ad-hoc `err.message`. Same (title, description) within a short window is
// suppressed so a burst of failing polls does not stack identical toasts.
//
// Important: the title must come through the react-i18next `t()` at the call
// site — this keeps the i18n key statically visible to
// scripts/check-react-i18n.mjs, and keeps this module free of any i18n
// imports (page tests mock react-i18next without initReactI18next).

// Dedupe only applies when the CALLER opts in with an explicit dedupeKey
// (e.g. failing watchers that burst identical errors). Tests and one-shot
// UI actions must always show their toast, so there is no implicit default.

const DEDUPE_WINDOW_MS = 10_000;
const recentKeys = new Map<string, number>();

export function showErrorToast(
  err: unknown,
  translatedTitle?: string,
  opts?: { dedupeKey?: string }
): void {
  const description = describeError(err);
  const title = translatedTitle ?? description;

  if (opts?.dedupeKey) {
    const now = Date.now();
    const last = recentKeys.get(opts.dedupeKey);
    if (last != null && now - last < DEDUPE_WINDOW_MS) return;
    recentKeys.set(opts.dedupeKey, now);
    if (recentKeys.size > 64) {
      // Bound the dedupe map; window-expired entries are swept lazily.
      for (const [k, at] of recentKeys) {
        if (now - at >= DEDUPE_WINDOW_MS) recentKeys.delete(k);
      }
    }
  }

  toastManager.add({ type: "error", title, description });
}
