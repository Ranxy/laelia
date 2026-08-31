import { useState } from "react";
import { platformOwnsEdgeSwipe } from "./platform-edge-swipe";
import { useHistorySentinel } from "./use-history-sentinel";
import { useSwipeToCloseSheet } from "./use-swipe-to-close-sheet";

interface UseEdgeDragToCloseOptions {
  open: boolean;
  onClose: () => void;
}

// Single wiring point for the mobile edge drag-to-close treatment shared by
// the drawer-carrying sheets (ChatDrawerSheet, MentionDetailSheet). It owns
// the popup/overlay ref state the Sheet needs and composes the two halves of
// the gesture policy:
//
// - On browsers without a system edge-swipe (desktop devtools emulation,
//   Android in-page touches) the sheet follows the finger from the left edge
//   while the scrim fades to reveal the page underneath
//   (use-swipe-to-close-sheet) — the same feel as the thread panel.
// - On real iOS/iPadOS browsers the system edge-swipe recognizer owns those
//   touches (see platform-edge-swipe.ts), so the synthetic gesture yields
//   (stays inert) and dismissal goes through the history sentinel
//   (use-history-sentinel): the system swipe's transition reveals the
//   sheet-free page underneath and its commit closes the sheet instead of
//   leaving the page. The browser back button gains the same dismiss-on-back
//   behavior for free.
export function useEdgeDragToClose({
  open,
  onClose,
}: UseEdgeDragToCloseOptions) {
  const [popup, setPopup] = useState<HTMLDivElement | null>(null);
  const [overlay, setOverlay] = useState<HTMLDivElement | null>(null);
  useSwipeToCloseSheet({
    open: open && !platformOwnsEdgeSwipe(),
    onClose,
    popup,
    overlay,
  });
  useHistorySentinel(open, onClose);
  return { setPopup, setOverlay };
}
