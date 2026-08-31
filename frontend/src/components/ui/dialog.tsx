import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import {
  getLayerRoot,
  LAYER_BACKDROP_SET,
  LAYER_SURFACE_CLASS,
  usePreserveHigherLayerAccess,
} from "./layer";

// Typography shared verbatim across the modal shells: Dialog and AlertDialog
// use both, Sheet reuses the description. Extend (never re-list) these when a
// shell needs an extra class so the shells cannot drift apart.
export const MODAL_TITLE_CLASS = "text-lg font-semibold";
export const MODAL_DESCRIPTION_CLASS = "text-sm text-control-light";

// ---- Root ----
const Dialog = BaseDialog.Root;

// ---- Trigger ----
const DialogTrigger = BaseDialog.Trigger;

// ---- Overlay / Backdrop ----
function DialogOverlay({
  className,
  ref,
  ...props
}: ComponentProps<typeof BaseDialog.Backdrop>) {
  return (
    <BaseDialog.Backdrop
      ref={ref}
      className={cn(LAYER_BACKDROP_SET, className)}
      {...props}
    />
  );
}

// ---- Content / Popup ----
function DialogContent({
  className,
  children,
  ref,
  ...props
}: ComponentProps<typeof BaseDialog.Popup>) {
  usePreserveHigherLayerAccess("overlay");

  return (
    <BaseDialog.Portal container={getLayerRoot("overlay")}>
      <DialogOverlay />
      <BaseDialog.Popup
        ref={ref}
        className={cn(
          `fixed left-1/2 top-1/2 ${LAYER_SURFACE_CLASS} -translate-x-1/2 -translate-y-1/2`,
          // Single max-w utility (no responsive variant) so a caller's
          // max-w-* fully replaces it via tailwind-merge; a 2xl: variant
          // here would survive the merge and win the cascade on wide
          // screens, silently overriding the caller's width.
          "w-[calc(100vw-8rem)] max-w-[max(48rem,55vw)]",
          "max-h-[calc(100vh-10rem)] overflow-y-auto",
          "rounded-sm bg-background p-6 shadow-lg",
          className
        )}
        {...props}
      >
        {children}
      </BaseDialog.Popup>
    </BaseDialog.Portal>
  );
}

// ---- Title ----
function DialogTitle({
  className,
  ref,
  ...props
}: ComponentProps<typeof BaseDialog.Title>) {
  return (
    <BaseDialog.Title
      ref={ref}
      className={cn(MODAL_TITLE_CLASS, className)}
      {...props}
    />
  );
}

// ---- Description ----
function DialogDescription({
  className,
  ref,
  ...props
}: ComponentProps<typeof BaseDialog.Description>) {
  return (
    <BaseDialog.Description
      ref={ref}
      className={cn(MODAL_DESCRIPTION_CLASS, className)}
      {...props}
    />
  );
}

// ---- Close ----
const DialogClose = BaseDialog.Close;

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogTitle,
  DialogTrigger,
};
