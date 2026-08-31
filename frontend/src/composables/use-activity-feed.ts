import { create } from "@bufbuild/protobuf";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useReducer } from "react";
import { commandServiceClient } from "@/connect";
import type {
  Activity,
  ActivityCategory,
} from "@/types/proto-es/v1/command_pb";
import {
  ActivityState,
  ListActivitiesRequestSchema,
  MarkActivityDoneRequestSchema,
} from "@/types/proto-es/v1/command_pb";

// ---------------------------------------------------------------------------
// use-activity-feed — the Activity feed's read/mutate primitives (ADR-1).
//
// Replaces the ActivitySlice server cache: the feed used to live in the store
// with hand-rolled silent/loading flags, a three-way equality check, a custom
// request sequence, and a first-page merge hack that preserved pages the user
// had already loaded through infinite scroll. All of that dissolves here:
//   - one query per (filter, pageToken) — request ordering is the Query key,
//     so a stale poll can never overwrite a newer view;
//   - refetchInterval only on the page the user is looking at (desktop: the
//     current page; mobile: page 0, mirroring the old silent poll), so the
//     5s cadence keeps its visibility gating (refetchIntervalInBackground);
//   - structuralSharing (Query default) replaces the hand-written equality;
//   - markDone optimistically removes the row from every cached page (every
//     non-Done view excludes Done server-side) instead of patching a shared
//     list and waiting for the next poll.
// Query roots: ["activities", filterKey, pageToken]. The detail page's
// fallback scan reads the same subtrees (no separate detail cache — there is
// no GetActivity RPC).
// ---------------------------------------------------------------------------

export interface ActivityPageParams {
  readStateFilter: ActivityState;
  categoryFilter: ActivityCategory[];
}

export interface ActivityPageView {
  activities: Activity[];
  nextPageToken: string;
  // No cache entry for this page key yet (first load / just-entered page).
  pending: boolean;
  // A refetch (polled or focus-driven) is in flight for this page.
  fetching: boolean;
}

// Left-rail poll cadence — the activity feed polls at the light cadence of the
// channel list (5s) and only the visible page, so background refreshes never
// flicker the list.
export const ACTIVITY_POLL_INTERVAL_MS = 5000;
export const ACTIVITY_PAGE_SIZE = 50;

const ACTIVITY_ROOT_KEY = ["activities"] as const;

// filterKey renders a filter into a stable query-key segment. Every value that
// selects a different server view must live in the key, or two views share
// one cache entry.
function filterKey(p: ActivityPageParams): string {
  return `${p.readStateFilter}:${p.categoryFilter.join(",")}`;
}

// The raw cache shape (queries cache the raw RPC page; hooks view it as-is).
export interface ActivityServerPage {
  activities: Activity[];
  nextPageToken: string;
}

// useActivityPages mounts one query per entry of pageTokens (the token to
// ENTER that page; page 0 uses ""). Appended tokens mount as new queries;
// already-mounted pages keep their cached rows and only the intervalIndex
// page polls. intervalIndex selects which visible page refreshes on the 5s
// cadence (desktop: the page on screen; mobile: the first page — the old
// silent poll polled exactly the first page and never re-fetched scrolled-in
// pages).
export function useActivityPages(opts: {
  params: ActivityPageParams;
  pageTokens: string[];
  intervalIndex: number;
}): ActivityPageView[] {
  const { params, pageTokens, intervalIndex } = opts;
  const queries = useQueries({
    queries: pageTokens.map((token, index) => ({
      queryKey: [ACTIVITY_ROOT_KEY[0], filterKey(params), token] as const,
      queryFn: async (): Promise<ActivityServerPage> => {
        const res = await commandServiceClient.listActivities(
          create(ListActivitiesRequestSchema, {
            filter: params.categoryFilter,
            readStateFilter: params.readStateFilter as ActivityState,
            pageSize: ACTIVITY_PAGE_SIZE,
            pageToken: token,
          })
        );
        return { activities: res.activities, nextPageToken: res.nextPageToken };
      },
      staleTime: 0,
      // Action semantics: one RPC attempt per (re)load — a failed poll keeps
      // the cached rows and the next tick retries (the old slice kept the
      // list on transient errors too).
      retry: false,
      refetchInterval:
        index === intervalIndex ? ACTIVITY_POLL_INTERVAL_MS : false,
      refetchIntervalInBackground: false,
    })),
  });
  return queries.map((q) => ({
    activities: q.data?.activities ?? [],
    nextPageToken: q.data?.nextPageToken ?? "",
    pending: q.isPending,
    fetching: q.isFetching,
  }));
}

// useMarkActivityDone marks an activity done. The mutation optimistically
// removes the row from every cached page (every view mounted here excludes
// Done rows server-side, so the row must leave the list rather than linger)
// and restores the caches on failure. On success the pages invalidate so the
// next tick confirms with the server.
export function useMarkActivityDone(): (name: string) => Promise<void> {
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: (name: string) =>
      commandServiceClient.markActivityDone(
        create(MarkActivityDoneRequestSchema, { name })
      ),
    onMutate: async (name) => {
      await queryClient.cancelQueries({ queryKey: ACTIVITY_ROOT_KEY });
      const snapshots = queryClient.getQueriesData<ActivityServerPage>({
        queryKey: ACTIVITY_ROOT_KEY,
      });
      for (const [key, page] of snapshots) {
        if (!page) continue;
        queryClient.setQueryData(key, {
          activities: page.activities.filter((a) => a.name !== name),
          nextPageToken: page.nextPageToken,
        });
      }
      return snapshots;
    },
    onError: (_error, _name, snapshots) => {
      if (!snapshots) return;
      for (const [key, page] of snapshots) queryClient.setQueryData(key, page);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ACTIVITY_ROOT_KEY });
    },
  });
  // The component only needs completion (old slice swallowed errors and
  // returned undefined; optimistic removal already restored on failure).
  return useCallback(
    async (name: string) => {
      await mutation.mutateAsync(name).catch(() => undefined);
    },
    [mutation]
  );
}

// useActivityFromCache scans the cached activity pages for one row, driven by
// any update under the ["activities"] root. It is the detail page's fallback
// when the row was not passed via router state (direct load / page refresh):
// the list keeps its cache per page filter — like the old store list, the
// fallback only finds rows the list has actually loaded (there is no
// GetActivity RPC to fetch one).
export function useActivityFromCache(messageId: string): Activity | undefined {
  const queryClient = useQueryClient();
  const cache = queryClient.getQueryCache();
  const [, rerender] = useReducer((n: number) => n + 1, 0);
  // Re-run the scan whenever any activity query (re)fetches or settles.
  useEffect(() => {
    const unsubscribe = cache.subscribe((event) => {
      if (event.query.queryKey[0] === ACTIVITY_ROOT_KEY[0]) rerender();
    });
    return unsubscribe;
  }, [cache]);

  const pages = queryClient.getQueriesData<ActivityServerPage>({
    queryKey: ACTIVITY_ROOT_KEY,
  });
  for (const [, page] of pages) {
    if (!page) continue;
    const hit = page.activities.find((a) =>
      a.name.endsWith(`/${messageId ?? ""}`)
    );
    if (hit && messageId) return hit;
  }
  return undefined;
}
