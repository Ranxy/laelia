import { commandServiceClient } from "@/connect";
import { MAX_MARKDOWN_PREVIEW_BYTES } from "@/lib/markdown-file";
import type { AppSliceCreator, PreviewSlice } from "./types";

// createPreviewSlice owns the markdown file preview overlay state. Opening a
// preview decodes the file bytes as UTF-8 text and stores it for the overlay
// to render with markstream. Files above MAX_MARKDOWN_PREVIEW_BYTES are
// refused before any download happens — the overlay still opens in a
// "too-large" status so the user sees an explicit "preview not supported"
// message and can fall back to download.
export const createPreviewSlice: AppSliceCreator<PreviewSlice> = (set) => ({
  activePreview: null,

  async openFilePreview(
    conversation,
    rootMessageId,
    attachment,
    scrollToSectionId
  ) {
    const tooLarge = (attachment.sizeBytes ?? 0n) > MAX_MARKDOWN_PREVIEW_BYTES;
    set({
      activePreview: {
        conversation,
        conversationId: conversation.replace(/^conversations\//, ""),
        rootMessageId,
        attachment,
        content: "",
        status: tooLarge ? "too-large" : "loading",
        scrollToSectionId,
      },
    });
    if (tooLarge) return;

    try {
      const res = await commandServiceClient.downloadFile({
        id: attachment.id,
      });
      // Belt-and-suspenders: if the backend returned more than we want to
      // render (size metadata was missing/wrong), surface too-large instead
      // of attempting to render a multi-megabyte document.
      if (res.data.byteLength > Number(MAX_MARKDOWN_PREVIEW_BYTES)) {
        set((s) => ({
          activePreview:
            s.activePreview != null
              ? { ...s.activePreview, status: "too-large" }
              : s.activePreview,
        }));
        return;
      }
      const text = new TextDecoder("utf-8", { fatal: false }).decode(res.data);
      set((s) => ({
        activePreview:
          s.activePreview != null
            ? { ...s.activePreview, content: text, status: "ready" }
            : s.activePreview,
      }));
    } catch (err) {
      console.error("markdown preview fetch failed", err);
      set((s) => ({
        activePreview:
          s.activePreview != null
            ? { ...s.activePreview, status: "error" }
            : s.activePreview,
      }));
    }
  },

  closeFilePreview() {
    set({ activePreview: null });
  },
});
