import type { ComponentProps, ReactNode } from "react";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { useEdgeDragToClose } from "@/lib/use-edge-drag-to-close";

interface ChatDrawerSheetProps {
  open: boolean;
  onClose: () => void;
  width?: ComponentProps<typeof SheetContent>["width"];
  children: ReactNode;
}

// ChatDrawerSheet wraps the shared right-side Sheet with mobile
// swipe-to-close behavior (see use-edge-drag-to-close for the gesture and
// platform-yield details).
export function ChatDrawerSheet({
  open,
  onClose,
  width = "medium",
  children,
}: ChatDrawerSheetProps) {
  const { setPopup, setOverlay } = useEdgeDragToClose({ open, onClose });

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width={width} ref={setPopup} overlayRef={setOverlay}>
        {children}
      </SheetContent>
    </Sheet>
  );
}
