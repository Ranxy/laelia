import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { commandServiceClient } from "@/connect";
import type { Reminder, ReminderStatus } from "@/types/proto-es/v1/command_pb";
import {
  CancelReminderRequestSchema,
  ListRemindersRequestSchema,
  UpdateReminderRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, ReminderSlice } from "./types";

// remindersEqual returns true when two reminder arrays have the same names in
// the same order and each reminder is shallow-equal on its fields. Used to
// avoid replacing state with identical data from polling refreshes.
function remindersEqual(a: Reminder[], b: Reminder[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (
      x.name !== y.name ||
      x.status !== y.status ||
      x.cronExpr !== y.cronExpr ||
      x.tz !== y.tz ||
      x.taskContent !== y.taskContent ||
      x.assigneeName !== y.assigneeName ||
      x.fireAt !== y.fireAt
    ) {
      return false;
    }
  }
  return true;
}

// createReminderSlice owns the agent-page Reminders tab state: the list of
// reminders for the viewed agent (filtered by status), and the mutations the
// detail page uses (update schedule/content, cancel). Reminders mirror
// commands: 1:1 with their trigger message, fired by the manager scheduler.
export const createReminderSlice: AppSliceCreator<ReminderSlice> = (set) => ({
  reminders: [],
  remindersLoading: false,

  async listReminders(agent, params) {
    if (!params?.silent) {
      set({ remindersLoading: true });
    }
    try {
      const res = await commandServiceClient.listReminders(
        create(ListRemindersRequestSchema, {
          agent,
          pageSize: params?.pageSize ?? 50,
          pageToken: params?.pageToken ?? "",
          statusFilter: (params?.statusFilter ?? []).map(
            (s) => s as ReminderStatus
          ),
        })
      );
      set((state) => ({
        reminders: remindersEqual(state.reminders, res.reminders)
          ? state.reminders
          : res.reminders,
        remindersLoading: false,
      }));
      return { reminders: res.reminders, nextPageToken: res.nextPageToken };
    } catch {
      set({ reminders: [], remindersLoading: false });
      return undefined;
    }
  },

  async getReminder(name) {
    try {
      const res = await commandServiceClient.getReminder({ name });
      return res.reminder;
    } catch {
      return undefined;
    }
  },

  async updateReminder(name, fields) {
    const res = await commandServiceClient.updateReminder(
      create(UpdateReminderRequestSchema, {
        name,
        fireAt: fields.fireAt ? timestampFromDate(fields.fireAt) : undefined,
        cronExpr: fields.cronExpr ?? "",
        tz: fields.tz ?? "",
        taskContent: fields.taskContent ?? "",
      })
    );
    const updated = res.reminder;
    if (updated) {
      set((state) => ({
        reminders: state.reminders.map((r) => (r.name === name ? updated : r)),
      }));
    }
    return updated;
  },

  async cancelReminder(name) {
    const res = await commandServiceClient.cancelReminder(
      create(CancelReminderRequestSchema, { name })
    );
    const updated = res.reminder;
    if (updated) {
      set((state) => ({
        reminders: state.reminders.map((r) => (r.name === name ? updated : r)),
      }));
    }
    return updated;
  },
});

// Reminders is the list type kept in the slice. Re-exported here for callers
// that import the slice and want the element type alongside.
export type { Reminder };
