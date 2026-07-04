import { commandServiceClient } from "@/connect";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// downloadAttachment fetches the file bytes via the CommandService.DownloadFile
// RPC and hands them to the browser as a blob download. Auth rides on the
// Connect transport cookie, so there is no separate URL/credential handling.
// The bytes are copied into a fresh ArrayBuffer so the Blob constructor
// accepts them under the strict ArrayBufferView<ArrayBuffer> lib typings.
export async function downloadAttachment(att: Attachment): Promise<void> {
  const res = await commandServiceClient.downloadFile({ id: att.id });
  const mime = res.file?.mimeType || att.mimeType || undefined;
  const ab = new ArrayBuffer(res.data.byteLength);
  new Uint8Array(ab).set(res.data);
  const blob = new Blob([ab], mime ? { type: mime } : undefined);
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = res.file?.originalName || att.name || att.id;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
