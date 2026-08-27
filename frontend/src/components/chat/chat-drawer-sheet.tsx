import type { ComponentProps, ReactNode } from "react";
import { useState } from "react";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { useSwipeToCloseSheet } from "@/lib/use-swipe-to-close-sheet";

interface ChatDrawerSheetProps {
  open: boolean;
  onClose: () => void;
  width?: ComponentProps<typeof SheetContent>["width"];
  children: ReactNode;
}

// ChatDrawerSheet wraps the shared right-side Sheet with the thread panel's
// mobile swipe-back gesture (see use-swipe-to-close-sheet): on mobile, dragging
// from the left edge slides the drawer out following the finger while the scrim
// fades to reveal the page underneath (the back target), releasing past the
// threshold commits the close and otherwise springs back. Desktop is untouched.
export function ChatDrawerSheet({
  open,
  onClose,
  width = "medium",
  children,
}: ChatDrawerSheetProps) {
  const [popup, setPopup] = useState<HTMLDivElement | null>(null);
  const [overlay, setOverlay] = useState<HTMLDivElement | null>(null);
  useSwipeToCloseSheet({ open, onClose, popup, overlay });

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width={width} ref={setPopup} overlayRef={setOverlay}>
        {children}
      </SheetContent>
    </Sheet>
  );
}
