import { ContextMenu as BaseContextMenu } from "@base-ui/react/context-menu";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "./layer";

// ---- Root ----
function ContextMenu(props: ComponentProps<typeof BaseContextMenu.Root>) {
  return <BaseContextMenu.Root {...props} />;
}

// ---- Trigger ----
const ContextMenuTrigger = BaseContextMenu.Trigger;

// ---- Portal + Positioner + Popup ----
function ContextMenuContent({
  className,
  children,
  ref,
  ...props
}: ComponentProps<typeof BaseContextMenu.Popup>) {
  return (
    <BaseContextMenu.Portal container={getLayerRoot("overlay")}>
      <BaseContextMenu.Positioner className={LAYER_SURFACE_CLASS}>
        <BaseContextMenu.Popup
          ref={ref}
          className={cn(
            "min-w-[12rem] overflow-hidden rounded-sm border border-control-border bg-background py-1 shadow-md",
            "focus:outline-hidden",
            className
          )}
          {...props}
        >
          {children}
        </BaseContextMenu.Popup>
      </BaseContextMenu.Positioner>
    </BaseContextMenu.Portal>
  );
}

// ---- Item ----
function ContextMenuItem({
  className,
  children,
  ref,
  ...props
}: ComponentProps<typeof BaseContextMenu.Item>) {
  return (
    <BaseContextMenu.Item
      ref={ref}
      className={cn(
        "relative flex items-center gap-x-2 px-3 py-2 text-sm cursor-pointer select-none outline-hidden",
        "hover:bg-control-bg data-highlighted:bg-control-bg",
        className
      )}
      {...props}
    >
      {children}
    </BaseContextMenu.Item>
  );
}

// ---- Separator ----
function ContextMenuSeparator({
  className,
  ref,
  ...props
}: ComponentProps<typeof BaseContextMenu.Separator>) {
  return (
    <BaseContextMenu.Separator
      ref={ref}
      className={cn("-mx-1 my-1 h-px bg-control-border", className)}
      {...props}
    />
  );
}

export {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
};
