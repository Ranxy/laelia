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

// createReminderSlice owns the agent-page Reminders tab state: the list of
// reminders for the viewed agent (filtered by status), and the mutations the
// detail page uses (update schedule/content, cancel). Reminders mirror
// commands: 1:1 with their trigger message, fired by the manager scheduler.
export const createReminderSlice: AppSliceCreator<ReminderSlice> = (set) => ({
  reminders: [],
  remindersLoading: false,

  async listReminders(agent, params) {
    set({ remindersLoading: true });
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
      set({ reminders: res.reminders, remindersLoading: false });
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
