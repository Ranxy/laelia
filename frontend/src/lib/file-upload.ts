import { create } from "@bufbuild/protobuf";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

export interface UploadProgress {
  loaded: number;
  total: number;
  percent: number;
}

export interface UploadFileOptions {
  conversation: string;
  originalName: string;
  mimeType: string;
  file: File;
  onProgress?: (progress: UploadProgress) => void;
}

/**
 * uploadFileToConversation uploads a file to the browser-facing multipart
 * endpoint using XHR. XHR exposes `upload.onprogress` so the UI can render a
 * real progress bar, and the browser streams the multipart body natively
 * without blocking the main thread with a large in-memory serialization.
 */
export function uploadFileToConversation(
  options: UploadFileOptions
): Promise<Attachment> {
  const { conversation, originalName, mimeType, file, onProgress } = options;
  const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "";
  const form = new FormData();
  form.append("conversation", conversation);
  form.append("originalName", originalName);
  form.append("mimeType", mimeType);
  form.append("file", file);

  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${baseUrl}/v1/files/upload`);
    xhr.withCredentials = true;

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) {
        onProgress({
          loaded: e.loaded,
          total: e.total,
          percent: Math.min(100, Math.round((e.loaded / e.total) * 100)),
        });
      }
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const data = JSON.parse(xhr.responseText) as Record<string, unknown>;
          // The Go backend marshals the protobuf File with snake_case JSON
          // tags (original_name / mime_type / size_bytes). Accept both
          // snake_case and camelCase so the client is robust to either.
          const id = String(data.id ?? "");
          const name = String(data.original_name ?? data.originalName ?? "");
          const mimeType = String(data.mime_type ?? data.mimeType ?? "");
          const sizeBytes = BigInt(
            String(data.size_bytes ?? data.sizeBytes ?? 0)
          );
          resolve(
            create(AttachmentSchema, {
              id,
              name,
              mimeType,
              sizeBytes,
            })
          );
        } catch (err) {
          reject(err);
        }
        return;
      }
      let message = `upload failed (${xhr.status})`;
      try {
        const data = JSON.parse(xhr.responseText) as { error?: string };
        if (data?.error) message = data.error;
      } catch {
        // keep the default message
      }
      reject(new Error(message));
    };

    xhr.onerror = () => reject(new Error("upload failed"));
    xhr.onabort = () => reject(new Error("upload aborted"));
    xhr.send(form);
  });
}
