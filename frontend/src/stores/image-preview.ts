import { commandServiceClient } from "@/connect";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator } from "./types";

// ImagePreviewSlice owns the image lightbox overlay: the active image (file +
// decoded blob URL + status) and the actions to open/close it. The overlay is
// mounted once at the dashboard layout and reads `activeImage`. The blob URL
// is created on open and revoked on close so we don't leak object URLs.
export interface ImagePreviewSlice {
  activeImage: {
    attachment: Attachment;
    blobUrl: string | null;
    status: "loading" | "ready" | "error";
  } | null;

  openImagePreview: (attachment: Attachment) => Promise<void>;
  closeImagePreview: () => void;
}

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

    // requestId ties the in-flight download to the attachment it was opened
    // for. If the user opens another image (or closes the lightbox) while this
    // download is in flight, the stale response must not overwrite the newer
    // active image — otherwise the lightbox shows A's bytes labeled as B.
    const requestId = attachment.id;
    try {
      const res = await commandServiceClient.downloadFile({
        id: attachment.id,
      });
      if (get().activeImage?.attachment.id !== requestId) return;
      const blob = new Blob([new Uint8Array(res.data)], {
        type: attachment.mimeType || undefined,
      });
      const blobUrl = URL.createObjectURL(blob);
      set({ activeImage: { attachment, blobUrl, status: "ready" } });
    } catch (err) {
      console.error("image preview fetch failed", err);
      if (get().activeImage?.attachment.id !== requestId) return;
      set({ activeImage: { attachment, blobUrl: null, status: "error" } });
    }
  },

  closeImagePreview() {
    const cur = get().activeImage;
    if (cur?.blobUrl) URL.revokeObjectURL(cur.blobUrl);
    set({ activeImage: null });
  },
});
