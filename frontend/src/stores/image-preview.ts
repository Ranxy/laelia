import { commandServiceClient } from "@/connect";
import type { AppSliceCreator, ImagePreviewSlice } from "./types";

// createImagePreviewSlice owns the image lightbox overlay state. Opening the
// lightbox fetches the file bytes via downloadFile, wraps them in a Blob, and
// stores an object URL for the overlay <img> to render. The previous blob URL
// is revoked on replace and on close so we never leak object URLs.
export const createImagePreviewSlice: AppSliceCreator<ImagePreviewSlice> = (
  set,
  get
) => ({
  activeImage: null,

  async openImagePreview(attachment) {
    // Revoke a prior lightbox's blob before replacing it.
    const prev = get().activeImage;
    if (prev?.blobUrl) URL.revokeObjectURL(prev.blobUrl);
    set({ activeImage: { attachment, blobUrl: null, status: "loading" } });

    try {
      const res = await commandServiceClient.downloadFile({
        id: attachment.id,
      });
      const blob = new Blob([new Uint8Array(res.data)], {
        type: attachment.mimeType || undefined,
      });
      const blobUrl = URL.createObjectURL(blob);
      set((s) =>
        s.activeImage
          ? { activeImage: { ...s.activeImage, blobUrl, status: "ready" } }
          : {}
      );
    } catch (err) {
      console.error("image preview fetch failed", err);
      set((s) =>
        s.activeImage
          ? { activeImage: { ...s.activeImage, status: "error" } }
          : {}
      );
    }
  },

  closeImagePreview() {
    const cur = get().activeImage;
    if (cur?.blobUrl) URL.revokeObjectURL(cur.blobUrl);
    set({ activeImage: null });
  },
});
