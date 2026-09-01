import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SearchChatHistoryRequestSchema,
  SearchScope,
} from "@/types/proto-es/v1/command_pb";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mockRouter = vi.hoisted(() => ({
  navigate: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mockRouter.navigate,
}));

// Desktop/mobile chrome is driven by the hook; tests flip this flag.
const desktopFlag = vi.hoisted(() => ({ value: true }));

vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: () => desktopFlag.value,
}));

const mockClient = vi.hoisted(() => ({
  listChannels: vi.fn(),
  searchChatHistory: vi.fn(),
  listUsers: vi.fn(),
  downloadAvatar: vi.fn(),
  downloadAgentAvatar: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listChannels: mockClient.listChannels,
    searchChatHistory: mockClient.searchChatHistory,
  },
  userServiceClient: {
    listUsers: mockClient.listUsers,
    downloadAvatar: mockClient.downloadAvatar,
  },
  agentServiceClient: {
    downloadAgentAvatar: mockClient.downloadAgentAvatar,
  },
}));

import { useAppStore } from "@/stores";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { GlobalSearchPage } from "./global-search";

function conversation(overrides?: Partial<Conversation>): Conversation {
  return {
    name: "conversations/c1",
    type: 2,
    title: "General",
    address: "general",
    ownerId: "users/1",
    archived: false,
    ...overrides,
  } as unknown as Conversation;
}

function seedStore(conversations: Conversation[]) {
  useAppStore.getState().reset();
  useAppStore.setState({
    channels: conversations,
    agents: [
      {
        name: "agents/a1",
        handle: "a1",
        title: "Seer",
        description: "scout",
        state: 1,
      } as unknown as AgentSummary,
    ],
    agentsLoading: false,
  });
}

function expectRequestShape(call: unknown) {
  // Mirror of buildSearchRequest for the "any time" default: no since window,
  // the 50-entry page size, and empty unset filters.
  const expected = create(SearchChatHistoryRequestSchema, {
    query: "hello",
    from: "",
    scope: SearchScope.UNSPECIFIED,
    conversation: "",
    since: undefined,
    limit: 50,
    pageToken: "",
  });
  expect(call).toMatchObject(expected);
}

beforeEach(() => {
  desktopFlag.value = true;
  mockClient.listChannels.mockReset().mockResolvedValue({
    channels: [conversation()],
    nextPageToken: "",
  });
  mockClient.searchChatHistory.mockReset().mockResolvedValue({
    entries: [],
    nextPageToken: "",
  });
  mockClient.listUsers.mockReset().mockResolvedValue({ users: [] });
  mockClient.downloadAvatar.mockReset().mockRejectedValue(new Error("none"));
  mockClient.downloadAgentAvatar
    .mockReset()
    .mockRejectedValue(new Error("none"));
  seedStore([conversation()]);
});

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("GlobalSearchPage", () => {
  it("renders the desktop filter chrome and searches after the debounce", async () => {
    render(<GlobalSearchPage />);

    // Chrome: main search box + From/conversation pickers + scope/time selects.
    expect(
      screen.getByPlaceholderText("globalSearch.placeholder")
    ).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText("globalSearch.from")
    ).toBeInTheDocument();
    expect(screen.getByText("globalSearch.scope-all")).toBeInTheDocument();
    expect(screen.getByText("globalSearch.time-any")).toBeInTheDocument();

    // The conversation roster comes from the paginated ListChannels loop.
    await waitFor(() => {
      expect(mockClient.listChannels).toHaveBeenCalledWith(
        expect.objectContaining({ pageSize: 100, includeClosed: true })
      );
    });

    fireEvent.change(screen.getByPlaceholderText("globalSearch.placeholder"), {
      target: { value: "hello" },
    });

    await waitFor(
      () => {
        expect(mockClient.searchChatHistory).toHaveBeenCalled();
      },
      { timeout: 2000 }
    );
    // Debounce: exactly one request for the burst of typing.
    expect(mockClient.searchChatHistory).toHaveBeenCalledTimes(1);
    expectRequestShape(mockClient.searchChatHistory.mock.calls[0][0]);
  });

  it("selecting a sender in the From picker filters the search by handle", async () => {
    mockClient.listUsers.mockResolvedValue({
      users: [{ name: "users/u1", handle: "u1", title: "Alice", email: "a@x" }],
    });
    render(<GlobalSearchPage />);

    fireEvent.change(screen.getByPlaceholderText("globalSearch.placeholder"), {
      target: { value: "hello" },
    });
    await waitFor(() => {
      expect(mockClient.searchChatHistory).toHaveBeenCalled();
    });

    fireEvent.change(screen.getByPlaceholderText("globalSearch.from"), {
      target: { value: "Alice" },
    });

    const row = await screen.findByText("Alice");
    fireEvent.pointerDown(row);
    fireEvent.pointerUp(row);
    fireEvent.click(row);

    await waitFor(
      () => {
        const last =
          mockClient.searchChatHistory.mock.calls[
            mockClient.searchChatHistory.mock.calls.length - 1
          ];
        expect(last[0]).toMatchObject({ query: "hello", from: "u1" });
      },
      { timeout: 2000 }
    );
  });

  it("selecting a conversation commits the resource name into the search", async () => {
    render(<GlobalSearchPage />);

    fireEvent.change(screen.getByPlaceholderText("globalSearch.placeholder"), {
      target: { value: "hello" },
    });
    await waitFor(() => {
      expect(mockClient.searchChatHistory).toHaveBeenCalled();
    });

    fireEvent.mouseDown(
      screen.getByPlaceholderText("globalSearch.all-channels")
    );
    const row = await screen.findByText("General");
    fireEvent.pointerDown(row);
    fireEvent.pointerUp(row);
    fireEvent.click(row);

    await waitFor(
      () => {
        const last =
          mockClient.searchChatHistory.mock.calls[
            mockClient.searchChatHistory.mock.calls.length - 1
          ];
        expect(last[0]).toMatchObject({
          query: "hello",
          conversation: "conversations/c1",
        });
      },
      { timeout: 2000 }
    );
  });

  it("toggles the mobile filter panel", async () => {
    desktopFlag.value = false;
    render(<GlobalSearchPage />);

    // Let the conversations load settle while the panel is still collapsed.
    await waitFor(() => {
      expect(mockClient.listChannels).toHaveBeenCalled();
    });

    const toggle = screen.getByRole("button", { name: "globalSearch.filters" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    // The filter chrome moves inside the expanded panel.
    expect(
      screen.getByPlaceholderText("globalSearch.from")
    ).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText("globalSearch.all-channels")
    ).toBeInTheDocument();
  });
});
