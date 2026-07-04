import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  buildOutline,
  isMarkdownAttachment,
  isMarkdownPreviewable,
  MAX_MARKDOWN_PREVIEW_BYTES,
  slugify,
} from "@/lib/markdown-file";
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

describe("isMarkdownAttachment", () => {
  it("TestIsMarkdownAttachment_NameExtension: detects .md/.markdown/.mdx by name", () => {
    expect(isMarkdownAttachment(att({ name: "doc.md" }))).toBe(true);
    expect(isMarkdownAttachment(att({ name: "README.MARKDOWN" }))).toBe(true);
    expect(isMarkdownAttachment(att({ name: "notes.mdx" }))).toBe(true);
    expect(isMarkdownAttachment(att({ name: "report.txt" }))).toBe(false);
  });

  it("TestIsMarkdownAttachment_Mime: detects text/markdown mime", () => {
    expect(
      isMarkdownAttachment(att({ name: "file", mimeType: "text/markdown" }))
    ).toBe(true);
    expect(
      isMarkdownAttachment(att({ name: "file", mimeType: "text/x-markdown" }))
    ).toBe(true);
    expect(
      isMarkdownAttachment(att({ name: "file", mimeType: "text/plain" }))
    ).toBe(false);
  });
});

describe("isMarkdownPreviewable", () => {
  it("TestIsMarkdownPreviewable_SizeGuard: rejects files over the 10 MiB limit", () => {
    expect(isMarkdownPreviewable(att({ name: "a.md", sizeBytes: 0n }))).toBe(
      true
    );
    expect(isMarkdownPreviewable(att({ name: "a.md", sizeBytes: 1024n }))).toBe(
      true
    );
    const at = MAX_MARKDOWN_PREVIEW_BYTES;
    expect(isMarkdownPreviewable(att({ name: "a.md", sizeBytes: at }))).toBe(
      true
    );
    expect(
      isMarkdownPreviewable(att({ name: "a.md", sizeBytes: at + 1n }))
    ).toBe(false);
  });
});

describe("slugify", () => {
  it("TestSlugify_LowercasesAndDelimits: produces url-safe slug", () => {
    expect(slugify("Hello, World!")).toBe("hello-world");
    expect(slugify("  Server (server/)  ")).toBe("server-server");
    expect(slugify("___")).toBe("section");
  });
});

describe("buildOutline", () => {
  it("TestBuildOutline_NumbersAndIds: numbers by hierarchy and assigns ids", () => {
    const container = document.createElement("div");
    container.innerHTML = `
      <h2>Intro</h2>
      <h3>Detail</h3>
      <h2>Server</h2>
      <h3>Config</h3>
      <h4>Port</h4>
    `;
    const items = buildOutline(container);
    expect(items.map((i) => i.number)).toEqual([
      "1",
      "1.1",
      "2",
      "2.1",
      "2.1.1",
    ]);
    expect(items.map((i) => i.text)).toEqual([
      "Intro",
      "Detail",
      "Server",
      "Config",
      "Port",
    ]);
    // Each heading got a stable id assigned on the DOM.
    for (const it of items) {
      expect(container.querySelector(`#${it.id}`)).not.toBeNull();
    }
  });

  it("TestBuildOutline_StripsLeadingDocNumber: removes existing numbering from text", () => {
    const container = document.createElement("div");
    container.innerHTML = `<h2>2.1 Server (server/)</h2>`;
    const items = buildOutline(container);
    expect(items[0].number).toBe("1");
    expect(items[0].text).toBe("Server (server/)");
  });

  it("TestBuildOutline_Empty: returns nothing for no headings", () => {
    const container = document.createElement("div");
    container.innerHTML = `<p>no headings here</p>`;
    expect(buildOutline(container)).toEqual([]);
  });
});
