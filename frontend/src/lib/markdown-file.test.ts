import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  anchorForSelection,
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

describe("anchorForSelection", () => {
  function setupDoc() {
    const container = document.createElement("div");
    container.innerHTML = `
      <h2>Intro</h2>
      <p>first paragraph</p>
      <h2>Server</h2>
      <p>buffer size is hardcoded</p>
    `;
    document.body.appendChild(container);
    const outline = buildOutline(container);
    return { container, outline };
  }

  function selectText(node: Node, offset: number, length: number) {
    const sel = window.getSelection()!;
    sel.removeAllRanges();
    const range = document.createRange();
    range.setStart(node, offset);
    range.setEnd(node, offset + length);
    sel.addRange(range);
    return sel;
  }

  it("TestAnchorForSelection_PrecedingHeading: anchors to the section before the selection", () => {
    const { container, outline } = setupDoc();
    // Select text inside the second <p> (under "Server").
    const secondP = container.querySelectorAll("p")[1].firstChild!;
    const sel = selectText(secondP, 0, "buffer size".length);
    const anchor = anchorForSelection(container, sel, outline);
    expect(anchor).not.toBeNull();
    expect(anchor!.sectionAnchor).toBe("§ 2 Server");
    expect(anchor!.quotedText).toBe("buffer size");
    expect(anchor!.sectionId).not.toBe("");
    container.remove();
  });

  it("TestAnchorForSelection_BeforeFirstHeading: returns null when selection precedes any heading", () => {
    const { container, outline } = setupDoc();
    // Place a <p> before the first heading and select inside it, so the
    // selection precedes every heading in document order.
    const before = document.createElement("p");
    before.textContent = "preamble text";
    container.insertBefore(before, container.firstChild!);
    const sel = selectText(before.firstChild!, 0, "preamble".length);
    expect(anchorForSelection(container, sel, outline)).toBeNull();
    container.remove();
  });

  it("TestAnchorForSelection_OutsideContainer: returns null when selection is outside", () => {
    const { container, outline } = setupDoc();
    const other = document.createElement("div");
    other.textContent = "elsewhere";
    document.body.appendChild(other);
    const sel = selectText(other.firstChild!, 0, 4);
    expect(anchorForSelection(container, sel, outline)).toBeNull();
    other.remove();
    container.remove();
  });
});
