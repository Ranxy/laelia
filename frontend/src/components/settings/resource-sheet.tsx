import { type ReactNode, useId, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";

// ---------------------------------------------------------------------------
// ResourceSheet — the shared create/edit drawer of the settings CRUD pages.
//
// Implements the AGENTS.md "outer shell + inner form + stable-entity-ref +
// key" pattern:
//   - the last-open entity is frozen into a ref so the header stays stable
//     through the ~200ms close animation instead of blanking the name
//     (01-B14);
//   - the inner form remounts once per open, so its useState seeds read the
//    fresh entity every time and edits of different entities (or a create
//    after an edit) never inherit stale state (01-B2/01-B4);
//   - the footer is owned here: one Save button wired to the inner form
//     element through the `form` attribute, so pages only render the form
//     fields inside a <form id={ctx.formId}>.
// ---------------------------------------------------------------------------

export type ResourceSheetWidth = "medium" | "standard" | "wide";

export interface ResourceSheetRenderContext<T> {
  // The frozen entity: non-null for edit (even mid-close-animation), null
  // for create.
  entity: T | null;
  // Id the inner <form> element must carry so the footer button submits it.
  formId: string;
}

interface ResourceSheetProps<T> {
  open: boolean;
  onClose: () => void;
  // The current edit target — null for a create sheet. useCrudDialog retains
  // the last target on close, so the frozen entity is stable mid-animation.
  entity: T | null;
  // Static string, or resolved from the (frozen) entity for edit titles.
  title: string | ((entity: T | null) => string);
  description?: string | ((entity: T | null) => string);
  width?: ResourceSheetWidth;
  submitting: boolean;
  // Extra disable condition beyond submitting (e.g. a page-level validity
  // check reported by the inner form).
  submitDisabled?: boolean;
  submitLabel?: string;
  renderForm: (ctx: ResourceSheetRenderContext<T>) => ReactNode;
}

export function ResourceSheet<T>(props: ResourceSheetProps<T>) {
  const {
    open,
    onClose,
    entity,
    title,
    description,
    width,
    submitting,
    submitDisabled,
    submitLabel,
    renderForm,
  } = props;
  const { t } = useTranslation();
  const formId = useId();

  // Freeze the entity while open=false so header text is stable during the
  // close animation (Base UI's Portal unmounts after the animation).
  const openEntityRef = useRef<T | null>(null);
  if (open) {
    openEntityRef.current = entity;
  }
  const stableEntity = openEntityRef.current;

  // One remount per open — inner form state seeds fresh every time.
  const wasOpenRef = useRef(false);
  const openSeqRef = useRef(0);
  if (open !== wasOpenRef.current) {
    wasOpenRef.current = open;
    if (open) openSeqRef.current += 1;
  }

  const resolvedTitle =
    typeof title === "function" ? title(stableEntity) : title;
  const resolvedDescription =
    typeof description === "function" ? description(stableEntity) : description;

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width={width}>
        <SheetHeader>
          <SheetTitle>{resolvedTitle}</SheetTitle>
          {resolvedDescription ? (
            <SheetDescription>{resolvedDescription}</SheetDescription>
          ) : undefined}
        </SheetHeader>
        <SheetBody>
          <div key={openSeqRef.current} className="flex h-full flex-col">
            {renderForm({ entity: stableEntity, formId })}
          </div>
        </SheetBody>
        <SheetFooter>
          <Button
            type="submit"
            form={formId}
            disabled={submitting || submitDisabled}
            aria-label={submitLabel ?? t("common.save")}
          >
            {submitting
              ? t("common.saving")
              : (submitLabel ?? t("common.save"))}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
