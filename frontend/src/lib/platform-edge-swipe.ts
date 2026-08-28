// Detects browsers whose UI process owns left-edge swipes with a system
// edge-swipe recognizer (the browser's own back/forward navigation gesture):
// iOS/iPadOS Safari and every App-Store browser (all WKWebView-based, so they
// report WebKit's navigator.vendor). That recognizer engages on the same touch
// as any synthetic in-page gesture, cannot be prevented by web content
// (WebKit bug 240892) and does not reliably deliver touchcancel when it claims
// the touch (bug 136531). A synthetic gesture racing it makes the browser
// composite its own back-transition snapshot — the previous history entry —
// underneath the live page, which shows up on real devices as an extra copy of
// the back target between the dragged page and the rendered preview (the
// "three layer" artifact).
//
// Devtools device emulation spoofs the UA and touch points but cannot change
// the engine, so navigator.vendor stays "Google Inc." under Chrome emulation —
// and no system recognizer exists there anyway. Emulation therefore keeps the
// synthetic gesture for development and testing. Home-screen standalone web
// apps have no browser chrome either; iOS provides no system swipe there, so
// the in-page gesture remains the only back affordance in that mode.
export function platformOwnsEdgeSwipe(): boolean {
  if (navigator.vendor !== "Apple Computer, Inc.") return false;
  // Desktop Mac Safari matches the vendor but has no touch digitizer; the
  // system edge swipe requires touch hardware. (jsdom reports the Apple
  // vendor too, with no touch points — tests must take the inert path.)
  if ((navigator.maxTouchPoints ?? 0) < 2) return false;
  const iosNavigator = navigator as Navigator & { standalone?: boolean };
  if (
    iosNavigator.standalone ||
    window.matchMedia("(display-mode: standalone)").matches
  ) {
    return false;
  }
  return true;
}
