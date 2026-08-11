import { Menu as BaseMenu } from "@base-ui/react/menu";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "./layer";

// ---- Root ----
function DropdownMenu({
  modal = false,
  ...props
}: ComponentProps<typeof BaseMenu.Root>) {
  return <BaseMenu.Root modal={modal} {...props} />;
}

// ---- Trigger ----
const DropdownMenuTrigger = BaseMenu.Trigger;

// ---- Submenu Root ----
const DropdownMenuSubmenu = BaseMenu.SubmenuRoot;

// ---- Portal + Positioner + Popup ----
function DropdownMenuContent({
  className,
  children,
  side = "bottom",
  sideOffset = 4,
  align = "end",
  ref,
  ...props
}: ComponentProps<typeof BaseMenu.Popup> & {
  side?: ComponentProps<typeof BaseMenu.Positioner>["side"];
  sideOffset?: ComponentProps<typeof BaseMenu.Positioner>["sideOffset"];
  align?: ComponentProps<typeof BaseMenu.Positioner>["align"];
}) {
  return (
    <BaseMenu.Portal container={getLayerRoot("overlay")}>
      <BaseMenu.Positioner
        side={side}
        sideOffset={sideOffset}
        align={align}
        className={LAYER_SURFACE_CLASS}
      >
        <BaseMenu.Popup
          ref={ref}
          className={cn(
            "min-w-[12rem] overflow-hidden rounded-sm border border-control-border bg-background py-1 shadow-md",
            "focus:outline-hidden",
            className
          )}
          {...props}
        >
          {children}
        </BaseMenu.Popup>
      </BaseMenu.Positioner>
    </BaseMenu.Portal>
  );
}

function DropdownMenuSubmenuContent({
  className,
  children,
  sideOffset = 4,
  align = "start",
  ref,
  ...props
}: ComponentProps<typeof BaseMenu.Popup> & {
  sideOffset?: ComponentProps<typeof BaseMenu.Positioner>["sideOffset"];
  align?: ComponentProps<typeof BaseMenu.Positioner>["align"];
}) {
  return (
    <BaseMenu.Portal container={getLayerRoot("overlay")}>
      <BaseMenu.Positioner
        side="right"
        sideOffset={sideOffset}
        align={align}
        className={LAYER_SURFACE_CLASS}
      >
        <BaseMenu.Popup
          ref={ref}
          className={cn(
            "min-w-[10rem] overflow-hidden rounded-sm border border-control-border bg-background py-1 shadow-md",
            "focus:outline-hidden",
            className
          )}
          {...props}
        >
          {children}
        </BaseMenu.Popup>
      </BaseMenu.Positioner>
    </BaseMenu.Portal>
  );
}

// ---- Item ----
function DropdownMenuItem({
  className,
  children,
  ref,
  ...props
}: ComponentProps<typeof BaseMenu.Item>) {
  return (
    <BaseMenu.Item
      ref={ref}
      className={cn(
        "relative flex items-center gap-x-2 px-3 py-2 text-sm cursor-pointer select-none outline-hidden",
        "hover:bg-control-bg data-highlighted:bg-control-bg",
        className
      )}
      {...props}
    >
      {children}
    </BaseMenu.Item>
  );
}

function DropdownMenuSubmenuTrigger({
  className,
  children,
  ref,
  ...props
}: ComponentProps<typeof BaseMenu.SubmenuTrigger>) {
  return (
    <BaseMenu.SubmenuTrigger
      ref={ref}
      className={cn(
        "relative flex w-full items-center gap-x-2 px-3 py-2 text-sm cursor-pointer select-none outline-hidden",
        "hover:bg-control-bg data-highlighted:bg-control-bg",
        className
      )}
      {...props}
    >
      {children}
    </BaseMenu.SubmenuTrigger>
  );
}

// ---- Separator ----
function DropdownMenuSeparator({
  className,
  ref,
  ...props
}: ComponentProps<typeof BaseMenu.Separator>) {
  return (
    <BaseMenu.Separator
      ref={ref}
      className={cn("-mx-1 my-1 h-px bg-control-border", className)}
      {...props}
    />
  );
}

export {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSubmenu,
  DropdownMenuSubmenuContent,
  DropdownMenuSubmenuTrigger,
  DropdownMenuTrigger,
};
