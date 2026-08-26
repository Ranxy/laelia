// PWA service-worker registration.
//
// Registers the shared /sw.js (app-shell precache + Web Push) as soon as the
// app boots so the PWA is installable and the shell is available offline —
// not only after the user opts into desktop notifications (web-push.ts reuses
// this same registration). Registration is best-effort and silent in
// production only; dev mode leaves the SW out so hot reload is never shadowed
// by a stale cache.

const SW_URL = "/sw.js";

export function registerServiceWorker(): void {
  if (!("serviceWorker" in navigator)) return;
  if (!import.meta.env.PROD) return;
  window.addEventListener("load", () => {
    navigator.serviceWorker.register(SW_URL).catch(() => {
      // Best-effort: a failed registration must never break the app.
    });
  });
}
