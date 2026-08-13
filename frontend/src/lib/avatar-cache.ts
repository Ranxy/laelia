import { create } from "@bufbuild/protobuf";
import { useEffect, useState, useSyncExternalStore } from "react";
import { agentServiceClient, userServiceClient } from "@/connect";
import {
  DeleteAgentAvatarRequestSchema,
  DownloadAgentAvatarRequestSchema,
  UploadAgentAvatarRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import { DownloadAvatarRequestSchema } from "@/types/proto-es/v1/user_service_pb";

// avatar-cache memoizes avatar image blob URLs by their resource name
// (users/{id}/avatar or agents/{id}/avatar) for the lifetime of the page.
// Avatars are fetched on demand via the matching DownloadAvatar RPC and cached
// so a channel full of messages from the same members fetches each avatar at
// most once per session. Names that 404 (no uploaded avatar / stale roster
// entry) are recorded as missing so the pixel fallback renders without a
// refetch loop.
//
// invalidateAvatar bumps a global epoch; useAvatar subscribes to it so that
// after an upload/delete the affected rows refetch the new image without a
// page reload.

const blobUrls = new Map<string, string>();
const inflight = new Map<string, Promise<string | null>>();
const missing = new Set<string>();

// epoch increments on every invalidate; useAvatar re-runs its fetch effect when
// it changes so cleared entries refetch.
let epoch = 0;
const epochListeners = new Set<() => void>();

function subscribeEpoch(cb: () => void): () => void {
  epochListeners.add(cb);
  return () => epochListeners.delete(cb);
}

function bumpEpoch() {
  epoch++;
  epochListeners.forEach((l) => l());
}

export function getCachedAvatarUrl(name: string): string | null {
  return blobUrls.get(name) ?? null;
}

export function isAvatarKnownMissing(name: string): boolean {
  return missing.has(name);
}

// avatarNameForUserId builds the avatar resource name for a user from their
// mention handle (the {user} segment of "users/{user}").
export function avatarNameForUserId(handle: string): string {
  return `users/${handle}/avatar`;
}

// avatarNameForAgentId builds the avatar resource name for an agent from its
// resource id (the {agent} segment of "agents/{agent}").
export function avatarNameForAgentId(agentResourceId: string): string {
  return `agents/${agentResourceId}/avatar`;
}

// fetchAvatarUrl returns a blob URL for the avatar, or null when the member has
// no uploaded avatar (or the fetch fails). Concurrent callers for the same
// name share a single in-flight request. User and agent avatars use their
// respective RPCs based on the resource-name prefix.
export function fetchAvatarUrl(name: string): Promise<string | null> {
  const cached = blobUrls.get(name);
  if (cached) return Promise.resolve(cached);
  if (missing.has(name)) return Promise.resolve(null);
  const existing = inflight.get(name);
  if (existing) return existing;

  const p = (async () => {
    try {
      const res = await (name.startsWith("agents/")
        ? agentServiceClient.downloadAgentAvatar(
            create(DownloadAgentAvatarRequestSchema, { name })
          )
        : userServiceClient.downloadAvatar(
            create(DownloadAvatarRequestSchema, { name })
          ));
      const blob = new Blob([new Uint8Array(res.data)], {
        type: res.mimeType || "image/octet-stream",
      });
      const url = URL.createObjectURL(blob);
      blobUrls.set(name, url);
      return url;
    } catch {
      missing.add(name);
      return null;
    } finally {
      inflight.delete(name);
    }
  })();
  inflight.set(name, p);
  return p;
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
// upload/delete so the new avatar replaces the old one. Omit the name to
// clear the whole cache (e.g. on logout).
export function invalidateAvatar(name?: string) {
  if (name) {
    const url = blobUrls.get(name);
    if (url) URL.revokeObjectURL(url);
    blobUrls.delete(name);
    missing.delete(name);
    inflight.delete(name);
    bumpEpoch();
    return;
  }
  for (const url of blobUrls.values()) URL.revokeObjectURL(url);
  blobUrls.clear();
  missing.clear();
  inflight.clear();
  bumpEpoch();
}

// useAvatar returns the cached blob URL for an avatar resource name, fetching
// on demand. Returns null while pending or when the user has no avatar, in
// which case the caller renders the pixel fallback. It re-runs on epoch bumps
// so an invalidated entry refetches after an upload/delete.
export function useAvatar(name: string | undefined | null): string | null {
  const epochValue = useSyncExternalStore(
    subscribeEpoch,
    () => epoch,
    () => epoch
  );
  const [url, setUrl] = useState<string | null>(() =>
    name ? getCachedAvatarUrl(name) : null
  );

  useEffect(() => {
    if (!name) {
      setUrl(null);
      return;
    }
    const cached = getCachedAvatarUrl(name);
    if (cached) {
      setUrl(cached);
      return;
    }
    if (isAvatarKnownMissing(name)) {
      setUrl(null);
      return;
    }
    let active = true;
    void fetchAvatarUrl(name).then((u) => {
      if (active) setUrl(u);
    });
    return () => {
      active = false;
    };
    // epochValue forces a refetch after invalidateAvatar even when `name` is
    // unchanged (the resource name is stable across re-uploads).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [name, epochValue]);

  return url;
}
