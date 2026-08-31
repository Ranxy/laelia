import type { ClassValue } from "clsx";
import { cn } from "@/lib/utils";
import { LAYER_SURFACE_CLASS } from "./layer";

// Shared styles for the positioned-popup family (Select / Popover /
// DropdownMenu main + submenu / ContextMenu). Every Positioner here portals
// into the overlay layer root and must carry the single intra-family stacking
// slot; every Popup shares the same rounded bordered surface. Per-component
// extras (min-width, overflow, text styles) and caller classNames are merged
// after these values so tailwind-merge keeps later overrides winning.
export const POPUP_SURFACE_CLASS =
  "rounded-sm border border-control-border bg-background py-1 shadow-md";

// Class for a family Positioner: attach the shared stacking slot, with
// optional caller-provided positioner classes merged after it.
export function positionerSurfaceClass(...classes: ClassValue[]) {
  return cn(LAYER_SURFACE_CLASS, ...classes);
}
