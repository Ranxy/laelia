import { commandServiceClient } from "@/connect";
import { isHtmlAttachment, MAX_HTML_PREVIEW_BYTES } from "@/lib/html-file";
import { MAX_MARKDOWN_PREVIEW_BYTES } from "@/lib/markdown-file";
import type { AppSliceCreator, PreviewSlice } from "./types";

// createPreviewSlice owns the markdown/html file preview overlay state.
// Opening a preview decodes the file bytes as UTF-8 text and stores it for
// the overlay to render (markstream for markdown, a sandboxed iframe for
// html). Files above the per-kind size limit are refused before any download
// happens — the overlay still opens in a "too-large" status so the user sees
// an explicit "preview not supported" message and can fall back to download.
export const createPreviewSlice: AppSliceCreator<PreviewSlice> = (
  set,
  get
) => ({
  activePreview: null,

  async openFilePreview(
    conversation,
    rootMessageId,
    attachment,
    scrollToAnchorId,
    scrollToQuote
  ) {
    const kind = isHtmlAttachment(attachment) ? "html" : "markdown";
    const limit =
      kind === "html" ? MAX_HTML_PREVIEW_BYTES : MAX_MARKDOWN_PREVIEW_BYTES;
    const tooLarge = (attachment.sizeBytes ?? 0n) > limit;
    set({
      activePreview: {
        kind,
        conversation,
        conversationId: conversation.replace(/^conversations\//, ""),
        rootMessageId,
        attachment,
        content: "",
        status: tooLarge ? "too-large" : "loading",
        scrollToAnchorId,
        scrollToQuote,
      },
    });
    if (tooLarge) return;

    // requestId ties the in-flight download to the attachment it was opened
    // for. Opening a second file (or closing the overlay) while the first is
    // downloading must not let the stale response overwrite the newer preview.
    const requestId = attachment.id;
    try {
      const res = await commandServiceClient.downloadFile({
        id: attachment.id,
      });
      if (get().activePreview?.attachment.id !== requestId) return;
      // Belt-and-suspenders: if the backend returned more than we want to
      // render (size metadata was missing/wrong), surface too-large instead
      // of attempting to render a multi-megabyte document.
      const tooLargeNow = res.data.byteLength > Number(limit);
      const text = tooLargeNow
        ? ""
        : new TextDecoder("utf-8", { fatal: false }).decode(res.data);
      set((s) =>
        s.activePreview?.attachment.id === requestId
          ? {
              activePreview: {
                ...s.activePreview,
                content: text,
                status: tooLargeNow ? "too-large" : "ready",
              },
            }
          : {}
      );
    } catch (err) {
      console.error("file preview fetch failed", err);
      set((s) =>
        s.activePreview?.attachment.id === requestId
          ? { activePreview: { ...s.activePreview, status: "error" } }
          : {}
      );
    }
  },

  closeFilePreview() {
    set({ activePreview: null });
  },
});
