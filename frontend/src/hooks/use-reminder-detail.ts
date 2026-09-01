import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { commandServiceClient } from "@/connect";
import type { Reminder } from "@/types/proto-es/v1/command_pb";
import {
  CancelReminderRequestSchema,
  ReminderStatus,
  UpdateReminderRequestSchema,
} from "@/types/proto-es/v1/command_pb";

// Use-reminder-detail is the reminder page's read + mutation primitive
// (ADR-1): a single-reminder query plus the edit/cancel mutations that used
// to live in the reminder store slice.
//
// The list page's cache root is also "reminders", but this composable
// deliberately does not invalidate across pages: after edit/cancel the list
// page self-heals on its own polling (the old store patch never reached the
// list page's local rows either, so the behavior is equivalent).
const DETAIL_KEY = (name: string) => ["reminders", "detail", name];

// Detail-page re-fetch cadence. See isTerminal below for when it stops.
const DETAIL_POLL_INTERVAL_MS = 2000;

// isTerminal reports whether a reminder reached an immutable end state —
// COMPLETED/CANCELLED/FAILED cannot be edited, cancelled, or fired again, so
// polling an already-settled reminder would only burn requests.
export function isTerminal(reminder: Reminder): boolean {
  return (
    reminder.status === ReminderStatus.COMPLETED ||
    reminder.status === ReminderStatus.CANCELLED ||
    reminder.status === ReminderStatus.FAILED
  );
}

export interface UseReminderResult {
  reminder: Reminder | undefined;
  // isPending — a page renders its loading screen while true.
  initial: boolean;
  error: boolean;
}

// useReminder reads one reminder ("reminders/{id}") with a 2s re-fetch loop
// that stops once a terminal status is observed. A failed RPC rethrows in the
// queryFn, so the query enters the error state and the page falls to its
// not-found screen — the old store action caught and returned undefined, the
// page rendered the same screen either way.
export function useReminder(name: string): UseReminderResult {
  const result = useQuery({
    queryKey: DETAIL_KEY(name),
    staleTime: 0,
    retry: false,
    queryFn: async () => {
      const res = await commandServiceClient.getReminder({ name });
      return res.reminder;
    },
    refetchInterval: (query) => {
      const reminder = query.state.data;
      return reminder && isTerminal(reminder) ? false : DETAIL_POLL_INTERVAL_MS;
    },
    refetchIntervalInBackground: false,
  });
  return {
    reminder: result.data,
    initial: result.isPending,
    error: result.isError,
  };
}

export interface UpdateReminderInput {
  name: string;
  fields: {
    fireAt?: Date;
    cronExpr?: string;
    tz?: string;
    taskContent?: string;
  };
}

// useUpdateReminder edits the schedule and/or task content. On success the
// returned reminder is written straight into the detail cache, replacing the
// old store setReminder — the UI updates immediately and the list page
// self-heals on its next own poll.
export function useUpdateReminder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ name, fields }: UpdateReminderInput) => {
      const res = await commandServiceClient.updateReminder(
        create(UpdateReminderRequestSchema, {
          name,
          // For a one-shot reminder fire_at is required; for a recurring
          // reminder it may be omitted and the manager computes the next cron
          // fire.
          fireAt: fields.fireAt ? timestampFromDate(fields.fireAt) : undefined,
          cronExpr: fields.cronExpr ?? "",
          tz: fields.tz ?? "",
          taskContent: fields.taskContent ?? "",
        })
      );
      const updated = res.reminder;
      // The old page treated a missing reminder in the response as a failed
      // edit; throwing folds that into the single error path.
      if (!updated) {
        throw new Error(`updateReminder(${name}) returned no reminder`);
      }
      return updated;
    },
    onSuccess: (updated, { name }) => {
      queryClient.setQueryData(DETAIL_KEY(name), updated);
    },
  });
}

// useCancelReminder cancels the reminder and backfills the detail cache with
// the returned terminal reminder (old cancelReminder + setReminder).
export function useCancelReminder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => {
      const res = await commandServiceClient.cancelReminder(
        create(CancelReminderRequestSchema, { name })
      );
      const updated = res.reminder;
      if (!updated) {
        throw new Error(`cancelReminder(${name}) returned no reminder`);
      }
      return updated;
    },
    onSuccess: (updated, name) => {
      queryClient.setQueryData(DETAIL_KEY(name), updated);
    },
  });
}
