import { RefObject, useEffect, useState } from "react";
import {
  anchorForSelection,
  type CommentAnchor,
  type OutlineItem,
} from "@/lib/markdown-file";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { CommentsPanel, usePreviewComments } from "./comments-panel";

// CommentsAside is the right-side panel inside the markdown preview overlay.
// It lists the section-anchored comments already posted on the previewed
// file's thread, and lets the user select text in the document, write a
// comment, and send it as a thread reply carrying an anchored attachment.
// Thin adapter over the shared CommentsPanel: the pending anchor comes from
// DOM text selection in the markdown body, and jumps locate by DOM
// scrollIntoView on the section id (bridge locate is the html twin).
export function CommentsAside({
  conversation,
  conversationId,
  rootMessageId,
  attachment,
  contentRef,
  outline,
  onJumpToSection,
}: {
  conversation: string; // "conversations/{id}"
  conversationId: string; // bare id
  rootMessageId: string;
  attachment: Attachment;
  contentRef: RefObject<HTMLDivElement | null>;
  outline: OutlineItem[];
  onJumpToSection: (sectionId: string) => void;
}) {
  // The markdown aside is only mounted while comment mode is on, so the
  // thread snapshot loads unconditionally (same as the pre-merge behavior).
  const comments = usePreviewComments(
    conversation,
    rootMessageId,
    attachment.id,
    true
  );
  const [pendingAnchor, setPendingAnchor] = useState<CommentAnchor | null>(
    null
  );

  // Capture text selections in the markdown body as a pending comment anchor.
  // Active only while this aside is mounted (i.e. comment mode is on).
  useEffect(() => {
    const onSelection = () => {
      const container = contentRef.current;
      if (!container) return;
      const sel = window.getSelection();
      if (!sel) return;
      const anchor = anchorForSelection(container, sel, outline);
      if (anchor) setPendingAnchor(anchor);
    };
    document.addEventListener("mouseup", onSelection);
    return () => document.removeEventListener("mouseup", onSelection);
  }, [contentRef, outline]);

  return (
    <CommentsPanel
      conversationId={conversationId}
      rootMessageId={rootMessageId}
      attachment={attachment}
      comments={comments}
      pendingAnchor={pendingAnchor}
      onSetPendingAnchor={setPendingAnchor}
      onJump={(sectionId) => onJumpToSection(sectionId)}
      jumpRequiresQuote={false}
    />
  );
}
