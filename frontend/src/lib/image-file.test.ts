import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { isImageAttachment } from "@/lib/image-file";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

function att(
  overrides: Partial<Pick<Attachment, "name" | "mimeType" | "sizeBytes">>
): Attachment {
  return create(AttachmentSchema, {
    id: "f",
    name: overrides.name ?? "",
    mimeType: overrides.mimeType ?? "",
    sizeBytes: overrides.sizeBytes ?? 0n,
  });
}

describe("isImageAttachment", () => {
  it("TestIsImageAttachment_ByExtension: matches image extensions", () => {
    expect(isImageAttachment(att({ name: "photo.png" }))).toBe(true);
    expect(isImageAttachment(att({ name: "photo.JPG" }))).toBe(true);
    expect(isImageAttachment(att({ name: "anim.gif" }))).toBe(true);
    expect(isImageAttachment(att({ name: "logo.svg" }))).toBe(true);
    expect(isImageAttachment(att({ name: "shot.webp" }))).toBe(true);
  });

  it("TestIsImageAttachment_ByMimeType: matches image/* mime types", () => {
    expect(isImageAttachment(att({ name: "file", mimeType: "image/png" }))).toBe(
      true
    );
    expect(
      isImageAttachment(att({ name: "file", mimeType: "image/jpeg" }))
    ).toBe(true);
    expect(
      isImageAttachment(att({ name: "file", mimeType: "image/webp" }))
    ).toBe(true);
  });

  it("TestIsImageAttachment_NonImage: rejects non-image attachments", () => {
    expect(isImageAttachment(att({ name: "report.md" }))).toBe(false);
    expect(isImageAttachment(att({ name: "doc.pdf" }))).toBe(false);
    expect(
      isImageAttachment(att({ name: "file", mimeType: "text/plain" }))
    ).toBe(false);
    expect(
      isImageAttachment(att({ name: "file", mimeType: "application/pdf" }))
    ).toBe(false);
  });

  it("TestIsImageAttachment_Empty: rejects empty name and mime", () => {
    expect(isImageAttachment(att({}))).toBe(false);
  });
});