import { MapPin } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// AttachmentCommentCard renders the structured "comment on a section of a
// file" payload carried by an anchored attachment: a section-anchor chip and
// the quoted selection. `variant` selects the layout:
//   - "inline"  used inside a MessageRow in the thread panel — shows just the
//               anchor chip + quote (the comment body is the message content,
//               rendered separately by MessageRow).
//   "compact" used in the preview's comment aside list — also renders the
//               comment body and is paired with a sender/time header by the
//               caller.
export function AttachmentCommentCard({
  attachment,
  variant = "inline",
  body,
  onJumpToSection,
  className,
}: {
  attachment: Attachment;
  variant?: "inline" | "compact";
  body?: string;
  onJumpToSection?: (sectionId: string) => void;
  className?: string;
}) {
  const anchor = attachment.sectionAnchor;
  const quote = attachment.quotedText;
  const canJump = !!onJumpToSection && !!attachment.sectionId;
  return (
    <div
      className={cn(
        "rounded-lg border border-control-border bg-control-bg/40 text-sm",
        className
      )}
    >
      {anchor && (
        <button
          type="button"
          disabled={!canJump}
          onClick={() =>
            canJump && onJumpToSection?.(attachment.sectionId ?? "")
          }
          title={anchor}
          className={cn(
            "flex w-full items-center gap-1.5 border-b border-control-border px-2 py-1.5 text-left text-[11px] font-medium text-main",
            canJump ? "cursor-pointer hover:bg-control-bg" : "cursor-default"
          )}
        >
          <MapPin className="size-3 shrink-0 text-accent" />
          <span className="truncate">{anchor}</span>
        </button>
      )}
      {quote && (
        <blockquote className="whitespace-pre-wrap break-words border-l-2 border-control-border py-1.5 pl-2 pr-2 text-[12px] italic text-control-light">
          {quote}
        </blockquote>
      )}
      {variant === "compact" && body && (
        <div className="whitespace-pre-wrap break-words px-2 py-1.5 text-sm text-main">
          {body}
        </div>
      )}
    </div>
  );
}
