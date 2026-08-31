import { Popover as BasePopover } from "@base-ui/react/popover";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { getLayerRoot } from "./layer";
import {
  POPUP_SURFACE_CLASS,
  positionerSurfaceClass,
} from "./positioned-popup";

// ---- Root ----
const Popover = BasePopover.Root;

// ---- Trigger ----
const PopoverTrigger = BasePopover.Trigger;

// ---- Portal + Positioner + Popup ----
function PopoverContent({
  className,
  children,
  side = "bottom",
  align = "end",
  sideOffset = 4,
  anchor,
  ref,
  ...props
}: ComponentProps<typeof BasePopover.Popup> & {
  side?: ComponentProps<typeof BasePopover.Positioner>["side"];
  align?: ComponentProps<typeof BasePopover.Positioner>["align"];
  sideOffset?: ComponentProps<typeof BasePopover.Positioner>["sideOffset"];
  anchor?: ComponentProps<typeof BasePopover.Positioner>["anchor"];
}) {
  return (
    <BasePopover.Portal container={getLayerRoot("overlay")}>
      <BasePopover.Positioner
        side={side}
        align={align}
        sideOffset={sideOffset}
        anchor={anchor}
        className={positionerSurfaceClass()}
      >
        <BasePopover.Popup
          ref={ref}
          className={cn(
            POPUP_SURFACE_CLASS,
            // p-3 (not the shared py-1): a popover pads all around its body.
            "p-3 text-sm text-control",
            "focus:outline-hidden",
            className
          )}
          {...props}
        >
          {children}
        </BasePopover.Popup>
      </BasePopover.Positioner>
    </BasePopover.Portal>
  );
}

export { Popover, PopoverContent, PopoverTrigger };
