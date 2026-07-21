// resizeImageFile loads an image file, center-crops it to a square, scales it
// down to `size`x`size`, and re-encodes it as a compact bytes payload. This is
// the primary bandwidth control for avatar uploads: a multi-MB phone photo
// becomes a few-KB upload regardless of source dimensions.
//
// It prefers WebP (best compression); Safari lacks a WebP encoder, so it falls
// back to JPEG and reports the mime type actually used so the caller can pass it
// to the upload RPC.
export async function resizeImageFile(
  file: File,
  size = 256,
  quality = 0.9
): Promise<{ data: Uint8Array; mimeType: string }> {
  if (!file.type.startsWith("image/")) {
    throw new Error("file is not an image");
  }

  const objectUrl = URL.createObjectURL(file);
  try {
    const img = await loadImage(objectUrl);
    const side = Math.min(img.width, img.height);
    const sx = (img.width - side) / 2;
    const sy = (img.height - side) / 2;

    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("canvas 2d context unavailable");
    ctx.drawImage(img, sx, sy, side, side, 0, 0, size, size);

    const blob = await toBlobWithFallback(canvas, quality);
    if (!blob) throw new Error("failed to encode image");
    const data = new Uint8Array(await blob.arrayBuffer());
    return { data, mimeType: blob.type };
  } finally {
    URL.revokeObjectURL(objectUrl);
  }
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("failed to decode image"));
    img.src = src;
  });
}

// toBlobWithFallback tries WebP first and falls back to JPEG when the browser
// cannot encode WebP (notably Safari). Returns the produced blob (with its
// actual type) or null if both encodings fail.
function toBlobWithFallback(
  canvas: HTMLCanvasElement,
  quality: number
): Promise<Blob | null> {
  return new Promise((resolve) => {
    canvas.toBlob(
      (webp) => {
        if (webp && webp.type === "image/webp") {
          resolve(webp);
          return;
        }
        canvas.toBlob((jpeg) => resolve(jpeg), "image/jpeg", quality);
      },
      "image/webp",
      quality
    );
  });
}
