// PWA service-worker registration.
//
// Registers the shared /sw.js (app-shell precache + Web Push) as soon as the
// app boots so the PWA is installable and the shell is available offline —
// not only after the user opts into desktop notifications (web-push.ts reuses
// this same registration). Registration is best-effort and silent in
// production only; dev mode leaves the SW out so hot reload is never shadowed
// by a stale cache.

import { i18n } from "@/lib/i18n";
import { toastManager } from "@/lib/toast";

const SW_URL = "/sw.js";

export function registerServiceWorker(): void {
  if (!("serviceWorker" in navigator)) return;
  if (!import.meta.env.PROD) return;
  window.addEventListener("load", () => {
    navigator.serviceWorker
      .register(SW_URL)
      .then((reg) => {
        watchForUpdates(reg);
      })
      .catch(() => {
        // Best-effort: a failed registration must never break the app.
      });
  });
}

// watchForUpdates closes the auto-update loop on the page side. sw.ts calls
// skipWaiting() + clients.claim(), so a freshly installed SW activates at once
// and fires `controllerchange` on every page it takes over. When the page
// already had a controller that is a version swap: the running app is the OLD
// build and its precache entries are being purged (lazy chunks would 404), so
// reload once into the new version — the reload navigation is network-first,
// so it lands on the fresh deploy. The very first claim (a page that was never
// controlled) is not an update and must not reload, or first-time visitors
// would get a pointless second load.
function watchForUpdates(reg: ServiceWorkerRegistration): void {
  let hadController = navigator.serviceWorker.controller !== null;
  let reloading = false;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (!hadController) {
      hadController = true;
      return;
    }
    // A silent reload mid-work is a nasty surprise (lost draft text, closed
    // sheets). Announce the version swap first and give the toast a beat to
    // render; the reload still happens without any user action.
    if (reloading) return;
    reloading = true;
    toastManager.add({ title: i18n.t("common.pwa-updated-reload") });
    window.setTimeout(() => window.location.reload(), 1500);
  });

  // Boot-time register() already triggers an update check; long-lived tabs get
  // one extra nudge each time the tab becomes visible so a session spanning a
  // deploy still converges to the new version. The browser throttles repeated
  // checks, so this stays cheap.
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible") return;
    reg.update().catch(() => {
      // Best-effort; the next navigation retries anyway.
    });
  });
}
