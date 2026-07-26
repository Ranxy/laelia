import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  ConvertMessageToTaskRequestSchema,
  ListTaskCountsRequestSchema,
  ListTasksRequestSchema,
  TaskStatus,
} from "@/types/proto-es/v1/command_pb";
import { toUiMessage } from "./chat-helpers";
import type { AppSliceCreator, TaskSlice } from "./types";

// Page size for the task board. The panel loads the newest TASKS_PAGE_SIZE
// tasks first, then appends older pages on scroll-to-bottom (loadMoreTasks).
const TASKS_PAGE_SIZE = 30;

// createTaskSlice owns the channel task board panel. Tasks live in the same
// chatMessages flow as regular messages (a task IS a message with metadata);
// this slice is only the panel's separate view onto the task subset, plus the
// convert-message-to-task mutation. Task roots/reply ids are bare UUIDs; the
// ConvertMessageToTask RPC takes the full `conversations/{c}/messages/{m}`
// resource name, built here. The list is paginated newest-first; per-status
// totals come from a separate ListTaskCounts call so the summary is accurate
// regardless of how many tasks are loaded.
export const createTaskSlice: AppSliceCreator<TaskSlice> = (set, get) => ({
  tasksByConv: {},
  tasksNextPageToken: {},
  taskCountsByConv: {},
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
    // Opening the panel loads the task board (first page) + status counts;
    // closing leaves the cache in place so reopening is instant.
    if (get().tasksPanelOpen[convName]) {
      void get().loadTasks(conversationId);
      void get().loadTaskCounts(conversationId);
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
          pageSize: TASKS_PAGE_SIZE,
          pageToken: "",
        })
      );
      const tasks = (res.tasks ?? []).map(toUiMessage);
      set((s) => ({
        tasksByConv: { ...s.tasksByConv, [convName]: tasks },
        tasksNextPageToken: {
          ...s.tasksNextPageToken,
          [convName]: res.nextPageToken ?? "",
        },
        tasksLoading: { ...s.tasksLoading, [convName]: false },
      }));
    } catch {
      set((s) => ({
        tasksLoading: { ...s.tasksLoading, [convName]: false },
      }));
    }
  },

  async loadMoreTasks(conversationId) {
    const convName = `conversations/${conversationId}`;
    const pageToken = get().tasksNextPageToken[convName] ?? "";
    // No more pages, or a load already in flight — nothing to do.
    if (pageToken === "" || (get().tasksLoading[convName] ?? false)) return;
    set((s) => ({
      tasksLoading: { ...s.tasksLoading, [convName]: true },
    }));
    try {
      const res = await commandServiceClient.listTasks(
        create(ListTasksRequestSchema, {
          conversation: convName,
          pageSize: TASKS_PAGE_SIZE,
          pageToken,
        })
      );
      const more = (res.tasks ?? []).map(toUiMessage);
      set((s) => {
        const prev = s.tasksByConv[convName] ?? [];
        return {
          tasksByConv: { ...s.tasksByConv, [convName]: [...prev, ...more] },
          tasksNextPageToken: {
            ...s.tasksNextPageToken,
            [convName]: res.nextPageToken ?? "",
          },
          tasksLoading: { ...s.tasksLoading, [convName]: false },
        };
      });
    } catch {
      set((s) => ({
        tasksLoading: { ...s.tasksLoading, [convName]: false },
      }));
    }
  },

  async loadTaskCounts(conversationId) {
    const convName = `conversations/${conversationId}`;
    try {
      const res = await commandServiceClient.listTaskCounts(
        create(ListTaskCountsRequestSchema, { conversation: convName })
      );
      set((s) => ({
        taskCountsByConv: {
          ...s.taskCountsByConv,
          [convName]: {
            todo: res.todoCount,
            inProgress: res.inProgressCount,
            inReview: res.inReviewCount,
            done: res.doneCount,
          },
        },
      }));
    } catch {
      // network error — the panel keeps the stale counts; next loadTaskCounts retries
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
      // panel (first page, newest-first) and the status counts so the new TODO
      // task appears at the top and the summary increments.
      await get().loadTasks(conversationId);
      void get().loadTaskCounts(conversationId);
    } catch {
      // network error — the panel keeps the stale cache; next loadTasks retries
    }
  },
});
