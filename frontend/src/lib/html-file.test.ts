import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  buildHtmlPreviewDoc,
  htmlAnchorForSelection,
  isHtmlAttachment,
  isHtmlPreviewable,
  MAX_HTML_PREVIEW_BYTES,
  parseHtmlAnchor,
} from "@/lib/html-file";
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

describe("isHtmlAttachment", () => {
  it("TestIsHtmlAttachment_NameExtension: detects .html/.htm/.xhtml by name", () => {
    expect(isHtmlAttachment(att({ name: "page.html" }))).toBe(true);
    expect(isHtmlAttachment(att({ name: "PAGE.HTM" }))).toBe(true);
    expect(isHtmlAttachment(att({ name: "index.xhtml" }))).toBe(true);
    expect(isHtmlAttachment(att({ name: "report.txt" }))).toBe(false);
    expect(isHtmlAttachment(att({ name: "notes.md" }))).toBe(false);
  });

  it("TestIsHtmlAttachment_Mime: detects text/html mime", () => {
    expect(isHtmlAttachment(att({ name: "file", mimeType: "text/html" }))).toBe(
      true
    );
    expect(
      isHtmlAttachment(att({ name: "file", mimeType: "application/xhtml+xml" }))
    ).toBe(true);
    expect(
      isHtmlAttachment(att({ name: "file", mimeType: "text/plain" }))
    ).toBe(false);
  });
});

describe("isHtmlPreviewable", () => {
  it("TestIsHtmlPreviewable_SizeGuard: rejects files over the 10 MiB limit", () => {
    expect(isHtmlPreviewable(att({ name: "a.html", sizeBytes: 0n }))).toBe(
      true
    );
    expect(isHtmlPreviewable(att({ name: "a.html", sizeBytes: 1024n }))).toBe(
      true
    );
    const at = MAX_HTML_PREVIEW_BYTES;
    expect(isHtmlPreviewable(att({ name: "a.html", sizeBytes: at }))).toBe(
      true
    );
    expect(isHtmlPreviewable(att({ name: "a.html", sizeBytes: at + 1n }))).toBe(
      false
    );
  });
});

describe("htmlAnchorForSelection / parseHtmlAnchor", () => {
  it("TestHtmlAnchor_RoundTrip: encodes content-y and decodes it back", () => {
    const anchor = htmlAnchorForSelection("buffer size is hardcoded", 1234);
    expect(anchor).not.toBeNull();
    expect(anchor!.sectionId).toBe("html:y:1234");
    expect(anchor!.sectionAnchor).toBe("buffer size is hardcoded");
    expect(anchor!.quotedText).toBe("buffer size is hardcoded");
    expect(parseHtmlAnchor(anchor!.sectionId)).toEqual({ y: 1234 });
  });

  it("TestHtmlAnchor_NormalizesWhitespaceAndTruncates: collapses runs and caps lengths", () => {
    const long = "word ".repeat(300);
    const anchor = htmlAnchorForSelection(`  hello\n  world  ${long}  `, 5);
    expect(anchor!.quotedText).toBe(`hello world ${long.trim()}`.slice(0, 500));
    expect(anchor!.sectionAnchor.length).toBeLessThanOrEqual(60);
    expect(parseHtmlAnchor(anchor!.sectionId)).toEqual({ y: 5 });
  });

  it("TestHtmlAnchor_RejectsEmptyOrBroken: null for empty quote / bad y", () => {
    expect(htmlAnchorForSelection("   ", 10)).toBeNull();
    expect(htmlAnchorForSelection("text", Number.NaN)).toBeNull();
  });

  it("TestParseHtmlAnchor_RejectsForeignIds: ignores markdown ids and garbage", () => {
    expect(parseHtmlAnchor("md-0-hello-world")).toBeNull();
    expect(parseHtmlAnchor("html:y:abc")).toBeNull();
    expect(parseHtmlAnchor("")).toBeNull();
  });
});

describe("buildHtmlPreviewDoc", () => {
  it("TestBuildHtmlPreviewDoc_FullDocument: injects charset + bridge before </head>", () => {
    const doc =
      "<!doctype html><html><head><title>t</title></head><body><p>hi</p></body></html>";
    const out = buildHtmlPreviewDoc(doc, "nonce-1");
    expect(out).toContain('<meta charset="utf-8">');
    expect(out).toContain('data-ac-bridge="1"');
    expect(out).toContain('"nonce-1"');
    expect(out).not.toContain("__AC_BRIDGE_NONCE__");
    // Bridge is inside head, right before </head>, and exactly once.
    const headEnd = out.indexOf("</head>");
    expect(headEnd).toBeGreaterThan(out.indexOf("data-ac-bridge"));
    expect(out.split('data-ac-bridge="1"').length).toBe(2);
  });

  it("TestBuildHtmlPreviewDoc_UppercaseTags: handles </HEAD> and <HEAD>", () => {
    const doc = "<HTML><HEAD><TITLE>t</TITLE></HEAD><BODY>hi</BODY></HTML>";
    const out = buildHtmlPreviewDoc(doc, "nonce-2");
    expect(out).toContain('data-ac-bridge="1"');
    expect(out).toContain("</HEAD>");
    expect(out.split('data-ac-bridge="1"').length).toBe(2);
  });

  it("TestBuildHtmlPreviewDoc_NoHeadButBody: injects bridge before body content", () => {
    const doc = "<html><body><p>hi</p></body></html>";
    const out = buildHtmlPreviewDoc(doc, "nonce-3");
    expect(out).toContain('data-ac-bridge="1"');
    // Script must sit before the body content, not inside the paragraph.
    expect(out.indexOf("data-ac-bridge")).toBeLessThan(out.indexOf("<p>"));
  });

  it("TestBuildHtmlPreviewDoc_Fragment: wraps bare fragment in a document", () => {
    const out = buildHtmlPreviewDoc("<p>hello</p>", "nonce-4");
    expect(out.startsWith("<!doctype html>")).toBe(true);
    expect(out).toContain("<body><p>hello</p></body>");
    expect(out).toContain('data-ac-bridge="1"');
  });

  it("TestBuildHtmlPreviewDoc_ExistingCharset: does not duplicate charset meta", () => {
    const doc =
      '<html><head><meta charset="UTF-8"><title>t</title></head><body>x</body></html>';
    const out = buildHtmlPreviewDoc(doc, "nonce-5");
    expect(out.match(/<meta\s+charset/gi)?.length).toBe(1);
    expect(out.split('data-ac-bridge="1"').length).toBe(2);
  });
});
