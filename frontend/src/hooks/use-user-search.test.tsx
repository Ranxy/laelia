import { create } from "@bufbuild/protobuf";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useUserSearch } from "./use-user-search";

const mock = vi.hoisted(() => ({
  listUsers: vi.fn(),
}));

vi.mock("@/connect", () => ({
  userServiceClient: { listUsers: mock.listUsers },
}));

function Probe(props: { query: string; enabled: boolean }) {
  const { results, searching } = useUserSearch(props.query, {
    enabled: props.enabled,
  });
  return createElement(
    "div",
    null,
    createElement(
      "div",
      { "data-testid": "results" },
      JSON.stringify(results.map((u) => u.name))
    ),
    createElement("div", { "data-testid": "searching" }, String(searching))
  );
}

function mount(initial: { query: string; enabled: boolean }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    createElement(
      QueryClientProvider,
      { client },
      createElement(Probe, initial)
    )
  );
  return {
    rerender: (next: { query: string; enabled: boolean }) =>
      view.rerender(
        createElement(
          QueryClientProvider,
          { client },
          createElement(Probe, next)
        )
      ),
    unmount: view.unmount,
  };
}

const user = (name: string) => create(UserSchema, { name, title: name });

beforeEach(() => {
  vi.useFakeTimers();
  mock.listUsers.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe("useUserSearch", () => {
  it("debounces the fetch and exposes the results", async () => {
    mock.listUsers.mockResolvedValue({
      users: [user("users/1"), user("users/2")],
    });
    mount({ query: "ali", enabled: true });

    expect(mock.listUsers).not.toHaveBeenCalled();

    await advance(250);

    expect(mock.listUsers).toHaveBeenCalledTimes(1);
    expect(mock.listUsers).toHaveBeenCalledWith({
      pageSize: 50,
      filter: 'name.matches("ali") || email.matches("ali")',
    });
    expect(screen_results()).toBe(JSON.stringify(["users/1", "users/2"]));
  });

  it("drops a superseded in-flight response (generation guard)", async () => {
    let resolveFirst: (v: { users: unknown[] }) => void = () => {};
    mock.listUsers
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveFirst = resolve;
          })
      )
      .mockResolvedValueOnce({ users: [user("users/bob")] });

    const view = mount({ query: "a", enabled: true });
    await advance(250);
    expect(mock.listUsers).toHaveBeenCalledTimes(1);

    // Newer keystroke fires a second search that resolves first.
    view.rerender({ query: "ab", enabled: true });
    await advance(250);
    expect(mock.listUsers).toHaveBeenCalledTimes(2);
    expect(screen_results()).toBe(JSON.stringify(["users/bob"]));

    // The stale first response lands late — it must not clobber "bob".
    await act(async () => {
      resolveFirst({ users: [user("users/alice")] });
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen_results()).toBe(JSON.stringify(["users/bob"]));
  });

  it("clears results and stops searching while disabled", async () => {
    mock.listUsers.mockResolvedValue({ users: [user("users/1")] });
    const view = mount({ query: "ali", enabled: true });
    await advance(250);
    expect(screen_results()).toBe(JSON.stringify(["users/1"]));

    view.rerender({ query: "ali", enabled: false });
    expect(screen_results()).toBe("[]");
    expect(screen_searching()).toBe("false");
    expect(mock.listUsers).toHaveBeenCalledTimes(1);
  });

  it("never searches while disabled even with a non-empty query", async () => {
    // A disabled hook never searches — the caller gates typed-only search
    // through `enabled` (FromSenderPicker).
    mock.listUsers.mockResolvedValue({ users: [] });
    mount({ query: "ali", enabled: false });
    await advance(250);
    expect(mock.listUsers).not.toHaveBeenCalled();
    expect(screen_searching()).toBe("false");
  });
});

function screen_results(): string | null {
  return document.querySelector('[data-testid="results"]')?.textContent ?? null;
}

function screen_searching(): string | null {
  return (
    document.querySelector('[data-testid="searching"]')?.textContent ?? null
  );
}
