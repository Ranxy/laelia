import type { CommentAnchor } from "@/lib/markdown-file";
import type { ChatMessageUI } from "@/stores/types";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { CommentsPanel } from "./comments-panel";

// HtmlCommentsAside is the right-side panel inside the html preview overlay.
// It is the html twin adapter of the shared CommentsPanel: the pending anchor
// comes from the bridge-reported iframe selection (the sandboxed document
// cannot be inspected from the parent) and jumps go through the bridge
// (`locate` + `scroll-to`) instead of scrollIntoView.
export function HtmlCommentsAside({
  conversationId,
  rootMessageId,
  attachment,
  comments,
  pendingAnchor,
  focusKey,
  onClearPendingAnchor,
  onJumpToComment,
}: {
  conversationId: string; // bare id
  rootMessageId: string;
  attachment: Attachment;
  comments: ChatMessageUI[];
  // pendingAnchor is owned by the overlay: it is set from the iframe bridge's
  // `selection` message and cleared on `selection-cleared` / after send.
  pendingAnchor: CommentAnchor | null;
  onClearPendingAnchor: () => void;
  onJumpToComment: (sectionId: string, quote: string) => void;
  // Bumping focusKey (e.g. from the floating "add comment" button over the
  // iframe) moves focus into the composer.
  focusKey: number;
}) {
  return (
    <CommentsPanel
      conversationId={conversationId}
      rootMessageId={rootMessageId}
      attachment={attachment}
      comments={comments}
      pendingAnchor={pendingAnchor}
      onSetPendingAnchor={onClearPendingAnchor}
      focusKey={focusKey}
      onJump={onJumpToComment}
    />
  );
}
