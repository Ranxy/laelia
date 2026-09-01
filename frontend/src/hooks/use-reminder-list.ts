import { create } from "@bufbuild/protobuf";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { commandServiceClient } from "@/connect";
import type { Reminder, ReminderStatus } from "@/types/proto-es/v1/command_pb";
import { ListRemindersRequestSchema } from "@/types/proto-es/v1/command_pb";

// ---------------------------------------------------------------------------
// useReminderPage — the reminder list's read primitive (ADR-1).
//
// Replaces the useResourceList + store-slice wiring on the agent reminders
// tab: TanStack Query now owns the per-(agent, filter, page) cache, the race
// ordering, and the 5s silent poll of the current page. Flattened view for
// the page:
//
//   - initial — isPending: no cache entry for this key yet (first view, or a
//     fresh key after a filter/agent change thanks to the page's remount
//     key). Renders the skeleton.
//   - refreshing — a fetch is in flight while usable data is on screen: a
//     page turn (keepPreviousData keeps the previous rows painted), a poll,
//     or a cache-hit refetch. Rows stay on screen.
//   - error renders as an empty table on the page, mirroring the old
//     silently-failed store action.
// ---------------------------------------------------------------------------

export interface ReminderListPage {
  reminders: Reminder[];
  nextPageToken: string;
}

// Page size for one reminders page (parity with the previous store action).
const PAGE_SIZE = 50;

// Poll cadence for the reminder table: 5s matches the chat layout's left-rail
// list poll. Reminder rows change at human/agent-action pace; the poll only
// refreshes the currently viewed page and stays silent while in flight.
const LIST_POLL_INTERVAL_MS = 5000;

const EMPTY_REMINDERS: Reminder[] = [];

function statusFilterKey(statusFilter: ReminderStatus[]): string {
  // QueryKey component for the selected view; "" for the no-filter tab.
  return statusFilter.join(",");
}

export interface ReminderPageParams {
  agent: string;
  statusFilter: ReminderStatus[];
  pageToken: string;
}

export interface ReminderPageResult {
  reminders: Reminder[];
  nextPageToken: string;
  // Skeleton case: this key has no cache yet (first view / new filter key).
  initial: boolean;
  // A fetch is in flight while usable data is already on screen.
  refreshing: boolean;
  error: boolean;
}

export function useReminderPage(opts: ReminderPageParams): ReminderPageResult {
  const { agent, statusFilter, pageToken } = opts;
  const query = useQuery({
    // Every parameter that selects a different server view lives in the key:
    // [root, agent, filterKey(statusFilter), pageToken].
    queryKey: ["reminders", agent, statusFilterKey(statusFilter), pageToken],
    queryFn: () =>
      commandServiceClient.listReminders(
        create(ListRemindersRequestSchema, {
          agent,
          pageSize: PAGE_SIZE,
          pageToken,
          statusFilter,
        })
      ),
    select: (res): ReminderListPage => ({
      reminders: res.reminders,
      nextPageToken: res.nextPageToken,
    }),
    // The 5s poll proves freshness itself; no staleness window to serve.
    staleTime: 0,
    // One attempt per beat — a failed poll waits for the next one (the old
    // silent-fetch semantics).
    retry: false,
    // Poll the current page, same cadence as the pre-query list.
    refetchInterval: LIST_POLL_INTERVAL_MS,
    refetchIntervalInBackground: false,
    // Page turns keep the previous rows on screen until the new page lands;
    // the page's remount key guarantees a filter/agent change starts from a
    // real skeleton instead of carrying the previous view's placeholder.
    placeholderData: keepPreviousData,
  });

  return {
    reminders: query.data?.reminders ?? EMPTY_REMINDERS,
    nextPageToken: query.data?.nextPageToken ?? "",
    initial: query.isPending,
    refreshing: query.isFetching && !query.isPending,
    error: query.isError,
  };
}
