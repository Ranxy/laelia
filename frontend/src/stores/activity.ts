import { create } from "@bufbuild/protobuf";
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
import type { ActivitySlice, AppSliceCreator } from "./types";

// timestampEqual compares two protobuf Timestamps (seconds + nanos), treating
// both-missing as equal.
function timestampEqual(
  a?: { seconds: bigint; nanos: number },
  b?: { seconds: bigint; nanos: number }
): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return a.seconds === b.seconds && a.nanos === b.nanos;
}

// activitiesEqual returns true when two activity arrays have the same names in
// the same order and each activity is shallow-equal on every field the list and
// the detail pane render (including message/thread_root, which decide whether
// the detail opens a thread or the channel). Used to skip replacing state with
// identical data from polling refreshes, mirroring remindersEqual.
function activitiesEqual(a: Activity[], b: Activity[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (
      x.name !== y.name ||
      x.conversation !== y.conversation ||
      x.message !== y.message ||
      x.threadRoot !== y.threadRoot ||
      x.categories !== y.categories ||
      x.state !== y.state ||
      x.roomVersion !== y.roomVersion ||
      x.summary !== y.summary ||
      x.senderName !== y.senderName ||
      x.senderType !== y.senderType ||
      !timestampEqual(x.createdAt, y.createdAt) ||
      !timestampEqual(x.readAt, y.readAt) ||
      !timestampEqual(x.doneAt, y.doneAt)
    ) {
      return false;
    }
  }
  return true;
}

// createActivitySlice owns the per-user Activity feed: the list of new messages
// relevant to the caller (mention/task/reminder/thread) and the mark-done
// mutation. The feed is inherently per-user — the caller's own id is the
// implicit filter on the server — so the slice holds a single list for the
// authenticated user.
export const createActivitySlice: AppSliceCreator<ActivitySlice> = (
  set,
  get
) => ({
  activities: [],
  activitiesLoading: false,
  activitiesNextPageToken: "",

  async listActivities(params) {
    if (!params?.silent) {
      set({ activitiesLoading: true });
    }
    try {
      const res = await commandServiceClient.listActivities(
        create(ListActivitiesRequestSchema, {
          filter: (params?.filter ?? []).map((c) => c as ActivityCategory),
          readStateFilter: (params?.readStateFilter ??
            (0 as ActivityState)) as ActivityState,
          pageSize: params?.pageSize ?? 50,
          pageToken: params?.pageToken ?? "",
        })
      );
      const nextPageToken = res.nextPageToken ?? "";
      const pageToken = params?.pageToken ?? "";
      const silent = params?.silent ?? false;
      set((state) => {
        let nextActivities: Activity[];
        if (silent && pageToken === "") {
          // Background poll on the first page: merge any brand-new rows on top
          // without dropping pages the user has already loaded via infinite
          // scroll. Existing rows that also appear in the poll response are
          // refreshed in place with the latest server state. Locally DONE rows
          // are dropped too: every non-Done view excludes them server-side, so
          // a row marked done (or done elsewhere) must leave the list instead
          // of lingering as a stale "kept" page row.
          const incomingNames = new Set(res.activities.map((a) => a.name));
          const kept = state.activities.filter(
            (a) => !incomingNames.has(a.name) && a.state !== ActivityState.DONE
          );
          nextActivities = [...res.activities, ...kept];
        } else {
          nextActivities = activitiesEqual(state.activities, res.activities)
            ? state.activities
            : res.activities;
        }
        return {
          activities: activitiesEqual(state.activities, nextActivities)
            ? state.activities
            : nextActivities,
          activitiesNextPageToken: nextPageToken,
          activitiesLoading: false,
        };
      });
      return { activities: res.activities, nextPageToken };
    } catch {
      // A silent background poll (the activity feed polls every 5s) must keep
      // the existing list on a transient error — only an explicit load reports
      // failure and clears. Mirrors agent.ts's fetchAgents.
      if (!params?.silent) {
        set({
          activities: [],
          activitiesNextPageToken: "",
          activitiesLoading: false,
        });
      }
      return undefined;
    }
  },

  async loadMoreActivities(params) {
    const pageToken = get().activitiesNextPageToken;
    if (pageToken === "" || get().activitiesLoading) return;
    const res = await get().listActivities({
      ...params,
      pageToken,
      silent: true,
    });
    if (!res) return;
    set((state) => ({
      activities: [...state.activities, ...res.activities],
      activitiesNextPageToken: res.nextPageToken,
    }));
  },

  async markActivityDone(name) {
    try {
      const res = await commandServiceClient.markActivityDone(
        create(MarkActivityDoneRequestSchema, { name })
      );
      const updated = res.activity;
      if (updated) {
        set((state) => ({
          activities: state.activities.map((a) =>
            a.name === name ? updated : a
          ),
        }));
      }
      return updated;
    } catch {
      return undefined;
    }
  },
});

// Activity is the feed row type kept in the slice. Re-exported here for callers
// that import the slice and want the element type alongside.
export type { Activity };
