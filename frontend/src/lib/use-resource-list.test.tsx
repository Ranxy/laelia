import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useResourceList } from "./use-resource-list";

describe("useResourceList", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("walks the token stack forward and back", async () => {
    const pages: Record<string, { rows: string[]; nextPageToken: string }> = {
      "": { rows: ["one"], nextPageToken: "tok2" },
      tok2: { rows: ["two"], nextPageToken: "" },
    };
    const fetcher = vi.fn(async (token: string) => pages[token]);

    const { result } = renderHook(() =>
      useResourceList<string>({ fetch: fetcher, resetKey: "k" })
    );

    await waitFor(() => expect(result.current.rows).toEqual(["one"]));
    expect(result.current.pageIndex).toBe(0);
    expect(result.current.canNext).toBe(true);

    act(() => result.current.nextPage());
    await waitFor(() => expect(result.current.rows).toEqual(["two"]));
    expect(result.current.pageIndex).toBe(1);
    expect(result.current.canPrev).toBe(true);
    expect(result.current.canNext).toBe(false);

    act(() => result.current.prevPage());
    await waitFor(() => expect(result.current.rows).toEqual(["one"]));
    expect(result.current.pageIndex).toBe(0);
    // Going back re-fetches the page (non-silent); no poll is configured.
    expect(fetcher).toHaveBeenCalledWith(
      "",
      expect.objectContaining({ silent: false })
    );
  });

  it("drops stale responses that race a reset", async () => {
    // Page 1 fetch is held open while the "filter" changes.
    let releaseStale: (v: { rows: string[]; nextPageToken: string }) => void =
      () => {};
    const fetcher = vi.fn(
      async (
        token: string
      ): Promise<{ rows: string[]; nextPageToken: string } | undefined> => {
        if (token === "" && fetcher.mock.calls.length === 1) {
          return new Promise<{ rows: string[]; nextPageToken: string }>(
            (resolve) => {
              releaseStale = resolve;
            }
          );
        }
        return { rows: ["fresh"], nextPageToken: "" };
      }
    );

    const { result, rerender } = renderHook(
      ({ resetKey }: { resetKey: string }) =>
        useResourceList<string>({ fetch: fetcher, resetKey }),
      { initialProps: { resetKey: "a" } }
    );

    await waitFor(() => expect(result.current.refreshing).toBe(true));
    rerender({ resetKey: "b" }); // filter changed; new generation starts

    await waitFor(() => expect(result.current.rows).toEqual(["fresh"]));
    // Late reply of the pre-reset request must not overwrite it.
    act(() => releaseStale({ rows: ["stale"], nextPageToken: "" }));

    expect(result.current.rows).toEqual(["fresh"]);
    expect(result.current.error).toBe(false);
  });

  it("marks the first load as the skeleton and polls silently", async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn(async () => ({ rows: ["r"], nextPageToken: "" }));
    const { result } = renderHook(() =>
      useResourceList<string>({
        fetch: fetcher,
        resetKey: "k",
        pollMs: 5,
      })
    );

    expect(result.current.loading).toBe(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.loading).toBe(false);
    expect(result.current.rows).toEqual(["r"]);

    act(() => {
      vi.advanceTimersByTime(5);
    });
    await act(async () => {
      await vi.runAllTicks();
    });
    expect(fetcher).toHaveBeenCalledTimes(2);
    // Poll is silent: no skeleton, rows refreshed in place.
    expect(result.current.loading).toBe(false);
    expect(result.current.refreshing).toBe(false);
    expect(result.current.rows).toEqual(["r"]);
    vi.useRealTimers();
  });
});
