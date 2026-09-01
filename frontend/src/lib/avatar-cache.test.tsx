import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fetchAvatarUrl, invalidateAvatar, useAvatar } from "./avatar-cache";

// Pins the useAvatar behaviors around the shared async-memo-cache primitive:
// the stale-URL clear on member switch (06 B-03) and the refetch after an
// invalidation. The generation guard keeping a superseded in-flight fetch
// from writing back (06 B-02) is covered by async-memo-cache.test.ts.

const mock = vi.hoisted(() => ({
  downloadAvatar: vi.fn(),
  downloadAgentAvatar: vi.fn(),
}));

vi.mock("@/connect", () => ({
  userServiceClient: { downloadAvatar: mock.downloadAvatar },
  agentServiceClient: { downloadAgentAvatar: mock.downloadAgentAvatar },
}));

// jsdom has no object URL factory; the cache stores what it returns. The
// stub encodes the blob's MIME tag so distinct fetches yield distinct URLs.
URL.createObjectURL = vi.fn(
  (blob: Blob) => `blob:${(blob as Blob).type || "unknown"}`
);
URL.revokeObjectURL = vi.fn();

function avatarBytes(tag: string) {
  return { data: new Uint8Array([1, 2, 3]), mimeType: `image/${tag}` };
}

beforeEach(() => {
  vi.clearAllMocks();
  invalidateAvatar();
});

describe("useAvatar", () => {
  it("clears the previous member's URL while the next member loads (B-03)", async () => {
    // Member A resolves immediately; member B's fetch is held open.
    mock.downloadAvatar.mockImplementation((req: { name: string }) =>
      req.name === "users/a/avatar"
        ? Promise.resolve(avatarBytes("a"))
        : new Promise(() => {})
    );

    const { result, rerender } = renderHook(
      ({ name }: { name: string }) => useAvatar(name),
      { initialProps: { name: "users/a/avatar" } }
    );
    await waitFor(
      () => expect(result.current).toBe("blob:image/a") // stub encodes the MIME tag
    );

    // Switching to B (uncached, still fetching) must NOT keep A's URL up.
    rerender({ name: "users/b/avatar" });
    expect(result.current).toBeNull();
  });

  it("refetches after an invalidation without a name change", async () => {
    // The mock reads the CURRENT payload at call time: act() flushes the
    // refetch synchronously, so a payload reprogrammed after the invalidate
    // would never be seen by the second fetch.
    let payload = avatarBytes("v1");
    mock.downloadAvatar.mockImplementation(async () => payload);

    const { result } = renderHook(() => useAvatar("users/a/avatar"));
    await waitFor(() => expect(result.current).not.toBeNull());
    const firstUrl = result.current;
    expect(mock.downloadAvatar).toHaveBeenCalledTimes(1);
    expect(firstUrl).toBe("blob:image/v1");

    // An upload/delete invalidates; the hook must fetch anew even though the
    // resource name is unchanged.
    payload = avatarBytes("b");
    act(() => {
      invalidateAvatar("users/a/avatar");
    });
    await waitFor(() => expect(mock.downloadAvatar).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current).toBe("blob:image/b"));
  });

  it("renders the pixel fallback for known-missing avatars without refetching", async () => {
    mock.downloadAvatar.mockRejectedValue(new Error("404"));

    const { result } = renderHook(() => useAvatar("users/none/avatar"));
    await waitFor(() => expect(result.current).toBeNull());

    await act(async () => {
      await fetchAvatarUrl("users/none/avatar");
    });
    expect(mock.downloadAvatar).toHaveBeenCalledTimes(1);
  });
});
