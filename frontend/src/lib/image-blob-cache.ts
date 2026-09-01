import { commandServiceClient } from "@/connect";
import { registerCleanup } from "@/stores/cleanup-registry";
import { createAsyncMemoCache } from "./async-memo-cache";

// Cached image-attachment bytes (Blob) keyed by attachment id, shared across
// RemoteImage mounts so switching channels doesn't re-download every image.
// Only the Blob is cached — each consumer creates its own object URL from it
// and revokes that URL on unmount — so evicting a cached blob never breaks a
// still-displayed <img> (the img's URL keeps the blob alive). Bounded by
// MAX_CACHED_IMAGES (FIFO), and cleared on logout so one principal's
// attachment bytes don't survive into the next session. The cache machinery
// (inflight dedupe, FIFO cap, generation guard) is the shared
// lib/async-memo-cache primitive — the generation guard is what keeps an
// in-flight download from writing the previous principal's bytes back after
// the logout invalidation (06 B-02).
const MAX_CACHED_IMAGES = 100;

// downloadFile takes only the attachment id; the MIME type comes from the
// caller's attachment payload and is only metadata on the Blob. It is stable
// per attachment, so the first caller's hint is kept per key (and dropped
// alongside its cache entry).
const mimeHints = new Map<string, string>();

const cache = createAsyncMemoCache<Blob>({
  fetch: async (id) => {
    try {
      const res = await commandServiceClient.downloadFile({ id });
      return new Blob([new Uint8Array(res.data)], {
        type: mimeHints.get(id) || undefined,
      });
    } catch (err) {
      console.error("image fetch failed", err);
      return null;
    }
  },
  maxEntries: MAX_CACHED_IMAGES,
  onDrop: (_blob, id) => {
    mimeHints.delete(id);
  },
});

export async function getImageBlob(
  id: string,
  mimeType: string
): Promise<Blob | null> {
  if (!mimeHints.has(id)) mimeHints.set(id, mimeType);
  return cache.load(id);
}

// Clears the cached bytes (and any in-flight fetches) so a logout doesn't leave
// the previous principal's image data readable in the tab.
export function invalidateImageBlobs() {
  mimeHints.clear();
  cache.invalidate();
}

// Self-registered with the cleanup registry: a store reset/logout clears the
// cached attachment bytes so one principal's image data never survives into
// the next session on the same tab.
registerCleanup(() => invalidateImageBlobs());
