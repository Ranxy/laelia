import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toastManager } from "@/lib/toast";
import { useResourceQuery } from "./use-resource-query";

vi.mock("@/lib/toast", () => ({ toastManager: { add: vi.fn() } }));

const toastMock = vi.mocked(toastManager.add);

// Minimal harness: renders the hook and exposes its result through the DOM.
function TestProbe(props: {
  enabled?: boolean;
  fetch: (signal: AbortSignal) => Promise<string[]>;
}) {
  const list = useResourceQuery({
    enabled: props.enabled,
    queryKey: ["probe"],
    queryFn: props.fetch,
    failureTitle: "settings.probe.load-failed",
  });
  return createElement(
    "div",
    null,
    createElement(
      "div",
      { "data-testid": "items" },
      JSON.stringify(list.items)
    ),
    createElement(
      "div",
      { "data-testid": "state" },
      `${list.initialLoading ? "initialLoading " : ""}${
        list.refreshing ? "refreshing " : ""
      }${list.error ? "error" : ""}`
    ),
    createElement("button", { onClick: list.reload }, "reload")
  );
}

function mount(props: {
  enabled?: boolean;
  fetch: (signal: AbortSignal) => Promise<string[]>;
}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  render(
    createElement(
      QueryClientProvider,
      { client },
      createElement(TestProbe, props)
    )
  );
  return client;
}

beforeEach(() => {
  toastMock.mockReset();
});

describe("useResourceQuery", () => {
  it("maps the fetched items and clears initialLoading once resolved", async () => {
    let fetches = 0;
    const client = mount({
      fetch: async () => {
        fetches += 1;
        return ["a", "b"];
      },
    });
    void client;

    expect(screen.getByTestId("state").textContent).toContain("initialLoading");
    await waitFor(() => {
      expect(screen.getByTestId("items").textContent).toBe(
        JSON.stringify(["a", "b"])
      );
      expect(screen.getByTestId("state").textContent).toBe("");
    });
    expect(fetches).toBe(1);
  });

  it("keeps the query idle and reports no loading while disabled", () => {
    const fetch = vi.fn().mockResolvedValue(["x"]);
    mount({ enabled: false, fetch });
    expect(fetch).not.toHaveBeenCalled();
    // A disabled query (no list permission) must not look like "loading";
    // the page renders its PermissionNotice instead.
    expect(screen.getByTestId("state").textContent).toBe("");
  });

  it("toasts a failure exactly once per failure episode", async () => {
    let failing = true;
    const client = mount({
      fetch: async () => {
        if (failing) throw new Error("nope");
        return ["ok"];
      },
    });

    await waitFor(() => {
      expect(screen.getByTestId("state").textContent).toContain("error");
    });
    expect(toastMock).toHaveBeenCalledTimes(1);

    // A refetch while still failing does not repeat the toast...
    await act(async () => {
      await client.refetchQueries({ queryKey: ["probe"] });
    });
    expect(toastMock).toHaveBeenCalledTimes(1);

    // ...until a successful result clears the error; a later failure toasts again.
    failing = false;
    await act(async () => {
      await client.refetchQueries({ queryKey: ["probe"] });
      await waitFor(() => {
        expect(screen.getByTestId("items").textContent).toBe(
          JSON.stringify(["ok"])
        );
        expect(screen.getByTestId("state").textContent).toBe("");
      });
    });
    failing = true;
    await act(async () => {
      await client.refetchQueries({ queryKey: ["probe"] });
    });
    await waitFor(() => {
      expect(screen.getByTestId("state").textContent).toContain("error");
    });
    expect(toastMock).toHaveBeenCalledTimes(2);
  });
});
