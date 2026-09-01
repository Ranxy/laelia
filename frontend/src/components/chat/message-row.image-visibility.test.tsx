import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Stub react-i18next and markstream-react so MessageRow renders in isolation.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: "en-US" } }),
}));

vi.mock("markstream-react", () => ({
  MarkdownRender: ({ content }: { content: string }) => <>{content}</>,
  setCustomComponents: () => {},
  default: ({ content }: { content: string }) => <>{content}</>,
}));

const mockUseIsDesktop = vi.hoisted(() => vi.fn(() => true));
vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: mockUseIsDesktop,
}));

// RemoteImage fetches bytes via the blob cache; stub it so no network happens
// and the ready state renders an <img> immediately.
vi.mock("@/lib/image-blob-cache", () => ({
  getImageBlob: vi.fn(async () => new Blob(["x"], { type: "image/png" })),
}));

import { create } from "@bufbuild/protobuf";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { MessageRow } from "@/components/chat/message-row";
import type { ChatMessageUI } from "@/stores/ui-models";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

function imageAttachment() {
  return create(AttachmentSchema, {
    id: "file-1",
    name: "image.png",
    mimeType: "image/png",
    sizeBytes: 100n,
  });
}

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  mockUseIsDesktop.mockReturnValue(true);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  if (root) root.unmount();
  container?.remove();
});

function baseMsg(overrides: Partial<ChatMessageUI> = {}): ChatMessageUI {
  return {
    id: "m1",
    role: "user",
    content: "hello",
    timestamp: new Date(0),
    ...overrides,
  };
}

// The bubble element wraps content + attachments; when it carries "hidden" the
// recipient cannot see the image even though it is in the DOM.
function bubbleFor(att: Element | null): HTMLElement | null {
  return att?.closest("div.rounded-2xl") ?? null;
}

describe("MessageRow inline image visibility (recipient vs sender)", () => {
  it("OTHER user, text + image: bubble visible", async () => {
    await act(async () => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            content: "ada",
            principalId: "ran-user-1",
            attachments: [imageAttachment()],
          })}
          showAvatar
          currentPrincipalId="ran-user-2"
          agentTitle="Agent"
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    const img = container!.querySelector("img");
    expect(img).not.toBeNull();
    const bubble = bubbleFor(img);
    expect(bubble).not.toBeNull();
    expect(bubble!.className).not.toContain("hidden");
  });

  it("OTHER user, image-only (empty content): bubble visible — regression", async () => {
    await act(async () => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            content: "",
            principalId: "ran-user-1",
            attachments: [imageAttachment()],
          })}
          showAvatar
          currentPrincipalId="ran-user-2"
          agentTitle="Agent"
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    const img = container!.querySelector("img");
    expect(img).not.toBeNull();
    const bubble = bubbleFor(img);
    expect(bubble).not.toBeNull();
    // The bubble must NOT be hidden: a file-only message from another user
    // would otherwise be invisible to the recipient (the sender's own bubble
    // is always visible, which is why only recipients lost the image).
    expect(bubble!.className).not.toContain("hidden");
  });

  it("OTHER user, no content and no attachments: bubble stays hidden", async () => {
    await act(async () => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            content: "",
            principalId: "ran-user-1",
          })}
          showAvatar
          currentPrincipalId="ran-user-2"
          agentTitle="Agent"
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    const bubble = container!.querySelector("div.rounded-2xl");
    expect(bubble).not.toBeNull();
    expect(bubble!.className).toContain("hidden");
  });

  it("OTHER user, file-only (non-image, empty content): FileCard visible", async () => {
    await act(async () => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            content: "",
            principalId: "ran-user-1",
            attachments: [
              create(AttachmentSchema, {
                id: "file-2",
                name: "report.pdf",
                mimeType: "application/pdf",
                sizeBytes: 100n,
              }),
            ],
          })}
          showAvatar
          currentPrincipalId="ran-user-2"
          agentTitle="Agent"
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    // FileCard renders the attachment name as text.
    expect(container!.textContent).toContain("report.pdf");
    const card = container!.querySelector("div.rounded-2xl");
    expect(card).not.toBeNull();
    expect(card!.className).not.toContain("hidden");
  });

  it("OWN message, image-only (empty content): bubble visible", async () => {
    await act(async () => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            content: "",
            principalId: "ran-user-2",
            attachments: [imageAttachment()],
          })}
          showAvatar
          currentPrincipalId="ran-user-2"
          agentTitle="Agent"
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    const img = container!.querySelector("img");
    expect(img).not.toBeNull();
    const bubble = bubbleFor(img);
    expect(bubble).not.toBeNull();
    expect(bubble!.className).not.toContain("hidden");
  });
});
