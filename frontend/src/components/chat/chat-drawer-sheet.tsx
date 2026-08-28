import type { ComponentProps, ReactNode } from "react";
import { useState } from "react";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { platformOwnsEdgeSwipe } from "@/lib/platform-edge-swipe";
import { useHistorySentinel } from "@/lib/use-history-sentinel";
import { useSwipeToCloseSheet } from "@/lib/use-swipe-to-close-sheet";

interface ChatDrawerSheetProps {
  open: boolean;
  onClose: () => void;
  width?: ComponentProps<typeof SheetContent>["width"];
  children: ReactNode;
}

// ChatDrawerSheet wraps the shared right-side Sheet with mobile
// swipe-to-close behavior:
//
// - On browsers without a system edge-swipe (desktop devtools emulation,
//   Android in-page touches) the drawer follows the finger
//   from the left edge while the scrim fades to reveal the page underneath
//   (see use-swipe-to-close-sheet) — the same feel as the thread panel.
// - On real iOS/iPadOS browsers the system edge-swipe recognizer owns those
//   touches (see platform-edge-swipe.ts), so the synthetic gesture yields and
//   dismissal goes through the history sentinel (use-history-sentinel): the
//   system swipe's transition reveals the drawer-free page underneath and its
//   commit closes the drawer instead of leaving the page. The browser back
//   button gains the same dismiss-on-back behavior for free.
export function ChatDrawerSheet({
  open,
  onClose,
  width = "medium",
  children,
}: ChatDrawerSheetProps) {
  const [popup, setPopup] = useState<HTMLDivElement | null>(null);
  const [overlay, setOverlay] = useState<HTMLDivElement | null>(null);
  const yieldsToSystem = platformOwnsEdgeSwipe();
  useSwipeToCloseSheet({
    open: open && !yieldsToSystem,
    onClose,
    popup,
    overlay,
  });
  useHistorySentinel(open, onClose);

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width={width} ref={setPopup} overlayRef={setOverlay}>
        {children}
      </SheetContent>
    </Sheet>
  );
}
