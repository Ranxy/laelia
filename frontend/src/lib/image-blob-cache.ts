import { commandServiceClient } from "@/connect";

// Cached image-attachment bytes (Blob) keyed by attachment id, shared across
// RemoteImage mounts so switching channels doesn't re-download every image.
// Only the Blob is cached — each consumer creates its own object URL from it
// and revokes that URL on unmount — so evicting a cached blob never breaks a
// still-displayed <img> (the img's URL keeps the blob alive). Bounded by
// MAX_CACHED_IMAGES (FIFO), and cleared on logout so one principal's
// attachment bytes don't survive into the next session.
const MAX_CACHED_IMAGES = 100;
const imageBlobs = new Map<string, Blob>();
const inflight = new Map<string, Promise<Blob | null>>();

function cacheImageBlob(id: string, blob: Blob) {
  if (imageBlobs.size >= MAX_CACHED_IMAGES) {
    const oldest = imageBlobs.keys().next().value;
    if (oldest !== undefined) imageBlobs.delete(oldest);
  }
  imageBlobs.set(id, blob);
}

export async function getImageBlob(
  id: string,
  mimeType: string
): Promise<Blob | null> {
  const cached = imageBlobs.get(id);
  if (cached) return cached;
  const pending = inflight.get(id);
  if (pending) return pending;
  const promise = (async () => {
    try {
      const res = await commandServiceClient.downloadFile({ id });
      const blob = new Blob([new Uint8Array(res.data)], {
        type: mimeType || undefined,
      });
      cacheImageBlob(id, blob);
      return blob;
    } catch (err) {
      console.error("image fetch failed", err);
      return null;
    } finally {
      inflight.delete(id);
    }
  })();
  inflight.set(id, promise);
  return promise;
}

// Clears the cached bytes (and any in-flight fetches) so a logout doesn't leave
// the previous principal's image data readable in the tab.
export function invalidateImageBlobs() {
  imageBlobs.clear();
  inflight.clear();
}
