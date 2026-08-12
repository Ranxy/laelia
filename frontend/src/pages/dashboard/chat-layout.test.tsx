import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ChatLayout } from "./chat-layout";

const mock = vi.hoisted(() => ({
  fetchChannels: vi.fn(),
}));

vi.mock("@/stores", () => {
  const state = {
    fetchChannels: mock.fetchChannels,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

vi.mock("@/components/chat/conversation-list", () => ({
  ConversationList: () => <div data-testid="conversation-list" />,
}));

function renderPage(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/" element={<ChatLayout />}>
          <Route
            path=":conversationId"
            element={<div data-testid="detail" />}
          />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.fetchChannels.mockReset();
});

describe("chat-layout", () => {
  it("fetches the channel roster on mount", async () => {
    renderPage("/");

    await waitFor(() => expect(mock.fetchChannels).toHaveBeenCalledTimes(1));
  });

  it("shows the conversation list when no conversation is open", () => {
    renderPage("/");

    expect(screen.getByTestId("conversation-list")).toBeInTheDocument();
  });

  it("renders the open conversation in the right pane", () => {
    renderPage("/ch1");

    expect(screen.getByTestId("conversation-list")).toBeInTheDocument();
    expect(screen.getByTestId("detail")).toBeInTheDocument();
  });
});
