import type { Attachment } from "@/types/proto-es/v1/command_pb";

// isImageAttachment reports whether the attachment is a raster/vector image
// the UI can render inline. Judged by mime type first (uploads from browsers
// usually carry a correct image/* type), then by file-name extension for
// agent-uploaded files whose mime may be text/plain or missing. SVG is
// included — rendered via <img> it is sandboxed and cannot execute script.
const IMAGE_MIME_RE = /^image\//i;
const IMAGE_NAME_RE = /\.(png|jpe?g|gif|webp|bmp|svg|avif|ico)$/i;

export function isImageAttachment(att: Attachment): boolean {
  if (att.mimeType && IMAGE_MIME_RE.test(att.mimeType)) return true;
  return IMAGE_NAME_RE.test(att.name ?? "");
}
