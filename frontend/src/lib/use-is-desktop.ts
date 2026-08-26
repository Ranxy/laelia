import { useSyncExternalStore } from "react";

// A "desktop" layout applies either when the viewport is wide enough, or when
// the app is running as an installed standalone PWA on a desktop-class device
// (fine pointer + hover). Installed desktop PWA windows can be narrower than
// the 1024px breakpoint; without this they would incorrectly fall back to the
// phone UI. Mobile standalone (add-to-home-screen on a phone) has no fine
// pointer/hover, so it still gets the mobile layout.
const DESKTOP_QUERY =
  "(min-width: 1024px), (display-mode: standalone) and (hover: hover) and (pointer: fine)";

function subscribe(callback: () => void) {
  const media = window.matchMedia(DESKTOP_QUERY);
  media.addEventListener("change", callback);
  return () => {
    media.removeEventListener("change", callback);
  };
}

function getSnapshot() {
  return window.matchMedia(DESKTOP_QUERY).matches;
}

function getServerSnapshot() {
  return false;
}

export function useIsDesktop(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
