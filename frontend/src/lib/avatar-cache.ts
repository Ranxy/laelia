import { create } from "@bufbuild/protobuf";
import { useEffect, useState, useSyncExternalStore } from "react";
import { agentServiceClient, userServiceClient } from "@/connect";
import { registerCleanup } from "@/stores/cleanup-registry";
import {
  DeleteAgentAvatarRequestSchema,
  DownloadAgentAvatarRequestSchema,
  UploadAgentAvatarRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import { DownloadAvatarRequestSchema } from "@/types/proto-es/v1/user_service_pb";
import { createAsyncMemoCache } from "./async-memo-cache";

// avatar-cache memoizes avatar image blob URLs by their resource name
// (users/{id}/avatar or agents/{id}/avatar) for the lifetime of the page.
// Resource-name construction lives in lib/resource.ts; this module owns the
// blob-URL cache, the on-demand fetch, and the invalidation broadcast. The
// cache machinery itself (inflight dedupe, generation guard, invalidation
// broadcast, capacity cap) is the shared lib/async-memo-cache primitive.
// Avatars are fetched on demand via the matching DownloadAvatar RPC and cached
// so a channel full of messages from the same members fetches each avatar at
// most once per session. Names that 404 (no uploaded avatar / stale roster
// entry) are recorded as missing so the pixel fallback renders without a
// refetch loop.
//
// invalidateAvatar broadcasts a new generation; useAvatar subscribes to it so
// that after an upload/delete the affected rows refetch the new image without
// a page reload.

// Generous session cap (06 B-09: the map used to grow without bound). At most
// this many blob URLs pin their bytes for the page lifetime.
const MAX_CACHED_AVATARS = 500;

const cache = createAsyncMemoCache<string>({
  fetch: async (name) => {
    try {
      const res = await (name.startsWith("agents/")
        ? agentServiceClient.downloadAgentAvatar(
            create(DownloadAgentAvatarRequestSchema, { name })
          )
        : userServiceClient.downloadAvatar(
            create(DownloadAvatarRequestSchema, { name })
          ));
      return URL.createObjectURL(
        new Blob([new Uint8Array(res.data)], {
          type: res.mimeType || "image/octet-stream",
        })
      );
    } catch {
      // No uploaded avatar (or a transient failure) — callers render the
      // pixel fallback; useAvatar records the miss so it does not loop.
      return null;
    }
  },
  maxEntries: MAX_CACHED_AVATARS,
  // Invalidation drops are explicit (upload/delete/logout) and subscribers
  // refetch, so their object URLs can be revoked. Capacity evictions may
  // still be displayed — those URLs leak on purpose (bounded by the cap)
  // rather than break a live <img>.
  onDrop: (url, _key, reason) => {
    if (reason === "invalidate") URL.revokeObjectURL(url);
  },
});

// Names whose fetch resolved null this generation: useAvatar renders the
// fallback without attempting another fetch until an invalidate clears them.
const missing = new Set<string>();

export function getCachedAvatarUrl(name: string): string | null {
  return cache.get(name) ?? null;
}

export function isAvatarKnownMissing(name: string): boolean {
  return missing.has(name);
}

// fetchAvatarUrl returns a blob URL for the avatar, or null when the member has
// no uploaded avatar (or the fetch fails). Concurrent callers for the same
// name share a single in-flight request. User and agent avatars use their
// respective RPCs based on the resource-name prefix.
export async function fetchAvatarUrl(name: string): Promise<string | null> {
  const cached = cache.get(name);
  if (cached) return cached;
  if (missing.has(name)) return null;
  const url = await cache.load(name);
  if (url === null) missing.add(name);
  return url;
}

// uploadAgentAvatar resizes and uploads an image file as the agent's avatar.
// Returns the updated Agent protobuf so callers can refresh the displayed
// avatar resource name.
export async function uploadAgentAvatar(
  name: string,
  file: File
): Promise<void> {
  const avatarName = name.startsWith("agents/") ? `${name}/avatar` : name;
  const arrayBuffer = await file.arrayBuffer();
  await agentServiceClient.uploadAgentAvatar(
    create(UploadAgentAvatarRequestSchema, {
      name: avatarName,
      data: new Uint8Array(arrayBuffer),
      mimeType: file.type,
    })
  );
}

// deleteAgentAvatar removes an agent's uploaded avatar, reverting to the pixel
// default.
export async function deleteAgentAvatar(name: string): Promise<void> {
  const avatarName =
    name.startsWith("agents/") && !name.endsWith("/avatar")
      ? `${name}/avatar`
      : name;
  await agentServiceClient.deleteAgentAvatar(
    create(DeleteAgentAvatarRequestSchema, { name: avatarName })
  );
}

// invalidateAvatar drops the cached blob URL (revoking the object URL) and
// clears the missing flag so the next render refetches. Call after an
// upload/delete so the new avatar replaces the old one (06 B-02: the
// generation guard in the primitive keeps an in-flight fetch for the OLD
// image from writing itself back after this). Omit the name to clear the
// whole cache (e.g. on logout).
export function invalidateAvatar(name?: string) {
  if (name) {
    missing.delete(name);
    cache.invalidate(name);
    return;
  }
  missing.clear();
  cache.invalidate();
}

// useAvatar returns the cached blob URL for an avatar resource name, fetching
// on demand. Returns null while pending or when the user has no avatar, in
// which case the caller renders the pixel fallback. It re-renders on
// invalidate generations so an invalidated entry refetches after an
// upload/delete.
export function useAvatar(name: string | undefined | null): string | null {
  const generation = useSyncExternalStore(
    cache.subscribe,
    cache.getVersion,
    cache.getVersion
  );
  const [url, setUrl] = useState<string | null>(() =>
    name ? getCachedAvatarUrl(name) : null
  );

  // biome-ignore lint/correctness/useExhaustiveDependencies: generation forces a refetch after invalidateAvatar even when `name` is unchanged (the resource name is stable across re-uploads).
  useEffect(() => {
    if (!name) {
      setUrl(null);
      return;
    }
    // Reset to the cache (or null) BEFORE fetching: switching members must
    // not keep flashing the previous member's avatar while this one loads
    // (06 B-03).
    const cached = getCachedAvatarUrl(name);
    setUrl(cached);
    if (cached || isAvatarKnownMissing(name)) return;
    let active = true;
    void fetchAvatarUrl(name).then((u) => {
      if (active) setUrl(u);
    });
    return () => {
      active = false;
    };
  }, [name, generation]);

  return url;
}

// Self-registered with the cleanup registry: a store reset/logout must drop
// every cached blob URL so a re-login refetches avatars for the new
// principal. Batch 4 归位 — this previously lived in stores/auth.ts as a
// lazy-namespace shim for partial test mocks.
registerCleanup(() => invalidateAvatar());
