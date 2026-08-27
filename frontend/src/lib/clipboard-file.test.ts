import { describe, expect, it } from "vitest";
import { filesFromClipboard } from "@/lib/clipboard-file";

// Fake DataTransfer: tests only rely on the iterable items list, mirroring
// what a paste event exposes in browsers (jsdom has no DataTransfer).
function clipboard(
  items: { kind: string; type: string; file?: File }[]
): DataTransfer {
  return {
    items: items.map((item) => ({
      kind: item.kind,
      type: item.type,
      getAsFile: () => item.file ?? null,
    })),
  } as unknown as DataTransfer;
}

describe("filesFromClipboard", () => {
  it("TestFilesFromClipboard_Image: extracts a pasted image file", () => {
    const file = new File(["png"], "image.png", { type: "image/png" });
    const files = filesFromClipboard(
      clipboard([{ kind: "file", type: "image/png", file }])
    );
    expect(files).toEqual([file]);
  });

  it("TestFilesFromClipboard_Mixed: keeps files in order among text items", () => {
    const png = new File(["a"], "a.png", { type: "image/png" });
    const html = new File(["<b>x</b>"], "b.html", { type: "text/html" });
    const files = filesFromClipboard(
      clipboard([
        { kind: "string", type: "text/html" },
        { kind: "file", type: "image/png", file: png },
        { kind: "string", type: "text/plain" },
        { kind: "file", type: "text/html", file: html },
      ])
    );
    expect(files).toEqual([png, html]);
  });

  it("TestFilesFromClipboard_EmptyEntries: skips null and zero-byte files", () => {
    const files = filesFromClipboard(
      clipboard([
        { kind: "file", type: "image/png" },
        { kind: "file", type: "text/plain", file: new File([], "") },
        { kind: "string", type: "text/plain" },
      ])
    );
    expect(files).toEqual([]);
  });

  it("TestFilesFromClipboard_NamelessImage: derives a name from the mime type", () => {
    const file = new File(["raw"], "", { type: "image/jpeg" });
    const files = filesFromClipboard(
      clipboard([{ kind: "file", type: "image/jpeg", file }])
    );
    expect(files).toHaveLength(1);
    expect(files[0].name).toBe("image.jpg");
    expect(files[0].type).toBe("image/jpeg");
  });

  it("TestFilesFromClipboard_NullData: returns nothing without a clipboard", () => {
    expect(filesFromClipboard(null)).toEqual([]);
  });
});
