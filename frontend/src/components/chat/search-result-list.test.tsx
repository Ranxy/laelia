import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  ChatMessageSchema,
  SearchChatHistoryEntrySchema,
} from "@/types/proto-es/v1/command_pb";
import { SearchResultList } from "./search-result-list";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }),
}));

vi.mock("@/components/chat/avatar", () => ({
  formatTime: () => "12:00",
}));

let seq = 0;

function entry(opts?: {
  content?: string;
  senderName?: string;
  principalId?: string;
  rootName?: string;
  rootContent?: string;
}) {
  seq += 1;
  const message = create(ChatMessageSchema, {
    name: `conversations/c1/messages/m${seq}`,
    content: opts?.content ?? "Deploy the service",
    senderName: opts?.senderName,
    principalId: opts?.principalId,
  });
  const e = create(SearchChatHistoryEntrySchema, { message });
  if (opts?.rootName) {
    const root = create(ChatMessageSchema, {
      name: opts.rootName,
      content: opts.rootContent ?? "The root question",
      senderName: "Rooter",
    });
    e.threadContext = { root } as never;
  }
  return e;
}

describe("SearchResultList", () => {
  it("renders single hits as clickable cards and reports the click", () => {
    const onOpen = vi.fn();
    render(<SearchResultList entries={[entry()]} query="" onOpen={onOpen} />);

    fireEvent.click(screen.getByText("Deploy the service"));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("shows the sender label with the name→principalId fallback", () => {
    render(
      <SearchResultList
        entries={[entry({ senderName: "  Alice  " })]}
        query=""
        onOpen={vi.fn()}
      />
    );
    expect(screen.getByText("@Alice")).toBeInTheDocument();

    render(
      <SearchResultList
        entries={[entry({ principalId: "u42" })]}
        query=""
        onOpen={vi.fn()}
      />
    );
    expect(screen.getByText("@u42")).toBeInTheDocument();
  });

  it("groups thread replies under their root and keeps the root unclickable", () => {
    const onOpen = vi.fn();
    const rootName = "conversations/c1/messages/root1";
    render(
      <SearchResultList
        entries={[
          entry({ rootName, content: "First matching reply" }),
          entry({ rootName, content: "Second matching reply" }),
        ]}
        query=""
        onOpen={onOpen}
        threadLabel="Thread"
      />
    );

    // The root renders once as context; both replies are indented below it.
    expect(screen.getByText("The root question")).toBeInTheDocument();
    expect(screen.getByText("First matching reply")).toBeInTheDocument();
    expect(screen.getByText("Second matching reply")).toBeInTheDocument();
    // Exactly one THREAD tag for the grouped card.
    expect(screen.getAllByText("Thread")).toHaveLength(1);

    fireEvent.click(screen.getByText("First matching reply"));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("marks query terms case-insensitively and prefers longer terms", () => {
    render(
      <SearchResultList
        entries={[entry({ content: "Deploy the DEPLOYER tool" })]}
        query="deploy deployer"
        onOpen={vi.fn()}
      />
    );

    const marks = screen.getAllByText(/deploy/i);
    // "DEPLOYER" must be one highlighted token, not "DEPLOY" + "ER" fragments.
    expect(marks.some((m) => m.textContent === "DEPLOYER")).toBe(true);
  });
});
