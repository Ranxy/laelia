// Extracts real files from a paste/drop DataTransfer so the chat composers can
// upload a clipboard image (screenshot, copied image) exactly like a picked
// file. Zero-byte entries some platforms emit as placeholders are skipped, and
// a nameless file (older Firefox) gets a name derived from its mime type so
// the upload carries a usable originalName.

const EXT_BY_MIME: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
};

export function filesFromClipboard(data: DataTransfer | null): File[] {
  if (!data) return [];
  const files: File[] = [];
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (!file || file.size === 0) continue;
    if (file.name) {
      files.push(file);
    } else {
      const ext = EXT_BY_MIME[file.type] ?? "bin";
      files.push(new File([file], `image.${ext}`, { type: file.type }));
    }
  }
  return files;
}
