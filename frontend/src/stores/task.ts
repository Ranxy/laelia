import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  ConvertMessageToTaskRequestSchema,
  ListTasksRequestSchema,
  TaskStatus,
} from "@/types/proto-es/v1/command_pb";
import { toUiMessage } from "./chat-helpers";
import type { AppSliceCreator, TaskSlice } from "./types";

// createTaskSlice owns the channel task board panel. Tasks live in the same
// chatMessages flow as regular messages (a task IS a message with metadata);
// this slice is only the panel's separate view onto the task subset, plus the
// convert-message-to-task mutation. Task roots/reply ids are bare UUIDs; the
// ConvertMessageToTask RPC takes the full `conversations/{c}/messages/{m}`
// resource name, built here.
export const createTaskSlice: AppSliceCreator<TaskSlice> = (set, get) => ({
  tasksByConv: {},
  tasksLoading: {},
  tasksPanelOpen: {},

  toggleTasksPanel(conversationId) {
    const convName = `conversations/${conversationId}`;
    set((s) => ({
      tasksPanelOpen: {
        ...s.tasksPanelOpen,
        [convName]: !s.tasksPanelOpen[convName],
      },
    }));
    // Opening the panel loads the task board; closing leaves the cache in place
    // so reopening is instant.
    if (get().tasksPanelOpen[convName]) {
      void get().loadTasks(conversationId);
    }
  },

  closeTasksPanel(conversationId) {
    const convName = `conversations/${conversationId}`;
    set((s) => ({
      tasksPanelOpen: { ...s.tasksPanelOpen, [convName]: false },
    }));
  },

  async loadTasks(conversationId, statusFilter) {
    const convName = `conversations/${conversationId}`;
    set((s) => ({
      tasksLoading: { ...s.tasksLoading, [convName]: true },
    }));
    try {
      const res = await commandServiceClient.listTasks(
        create(ListTasksRequestSchema, {
          conversation: convName,
          statusFilter: (statusFilter ?? []).map((s) => s as TaskStatus),
        })
      );
      const tasks = (res.tasks ?? []).map(toUiMessage);
      set((s) => ({
        tasksByConv: { ...s.tasksByConv, [convName]: tasks },
        tasksLoading: { ...s.tasksLoading, [convName]: false },
      }));
    } catch {
      set((s) => ({
        tasksLoading: { ...s.tasksLoading, [convName]: false },
      }));
    }
  },

  async convertMessageToTask(conversationId, messageId) {
    const message = `conversations/${conversationId}/messages/${messageId}`;
    try {
      await commandServiceClient.convertMessageToTask(
        create(ConvertMessageToTaskRequestSchema, { message })
      );
      // The conversion inserts a system notification row (bumping the
      // conversation version) which the channel watcher will surface; the task
      // itself is the same message id, now with task metadata. Refresh the
      // panel so the new task appears.
      await get().loadTasks(conversationId);
    } catch {
      // network error — the panel keeps the stale cache; next loadTasks retries
    }
  },
});
