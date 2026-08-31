import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import type { ChatMessage } from "@/types/proto-es/v1/command_pb";
import {
  AssignTaskRequestSchema,
  ConvertMessageToTaskRequestSchema,
  ListTaskCountsRequestSchema,
  ListTasksRequestSchema,
  TaskStatus,
  UpdateTaskStatusRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { toUiMessage } from "./chat-helpers";
import type { AppSliceCreator } from "./types";
import type { ChatMessageUI } from "./ui-models";

// TaskSlice owns the channel task board panel: per-conversation task listings
// (cached as ChatMessageUI so they reuse MessageRow's task badge), panel open
// state, and the convert-message-to-task mutation. Tasks live in the same
// chatMessages flow as regular messages (a task IS a message with metadata);
// this slice is only the panel's separate view onto the task subset.
export interface TaskCountsUI {
  todo: number;
  inProgress: number;
  inReview: number;
  done: number;
}

export interface TaskSlice {
  tasksByConv: Record<string, ChatMessageUI[]>;
  // nextPageToken per conversation: "" means no more (older) pages to load.
  tasksNextPageToken: Record<string, string>;
  // Per-status totals per conversation, from ListTaskCounts, so the board
  // summary stays accurate regardless of how many tasks the paginated list has
  // loaded into tasksByConv.
  taskCountsByConv: Record<string, TaskCountsUI>;
  tasksLoading: Record<string, boolean>;
  tasksPanelOpen: Record<string, boolean>;

  toggleTasksPanel: (conversationId: string) => void;
  closeTasksPanel: (conversationId: string) => void;
  loadTasks: (conversationId: string, statusFilter?: number[]) => Promise<void>;
  // loadMoreTasks appends the next (older) page to tasksByConv; a no-op when
  // there is no next page or a load is already in flight.
  loadMoreTasks: (conversationId: string) => Promise<void>;
  loadTaskCounts: (conversationId: string) => Promise<void>;
  convertMessageToTask: (
    conversationId: string,
    messageId: string
  ) => Promise<void>;
  // updateTaskStatus moves a task to any of the four statuses. The caller's
  // thread root is patched with the authoritative response and the board +
  // counts reload; throws on failure so the UI can surface the error.
  updateTaskStatus: (
    conversationId: string,
    rootMessageId: string,
    status: number
  ) => Promise<void>;
  // assignTask assigns a task to a channel member (user or agent). The caller's
  // thread root is patched with the authoritative response and the board +
  // counts reload; throws on failure so the UI can surface the error.
  assignTask: (
    conversationId: string,
    rootMessageId: string,
    memberType: number,
    memberId: string
  ) => Promise<void>;
  // patchTaskThreadAndRefresh patches the open thread's root with the
  // authoritative task message returned by a task mutation, then reloads the
  // board + counts. Shared by updateTaskStatus / assignTask.
  patchTaskThreadAndRefresh: (
    conversationId: string,
    rootMessageId: string,
    res: { message?: ChatMessage }
  ) => Promise<void>;
}

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

  // patchTaskThreadAndRefresh patches the open thread's root with the
  // authoritative task message returned by a task mutation, then reloads the
  // board + counts. Shared by updateTaskStatus / assignTask.
  async patchTaskThreadAndRefresh(conversationId, rootMessageId, res) {
    const ui = res?.message ? toUiMessage(res.message) : null;
    if (ui) {
      set((state) => {
        const thread = state.threadByRoot[rootMessageId];
        if (!thread) return {};
        return {
          threadByRoot: {
            ...state.threadByRoot,
            [rootMessageId]: {
              ...thread,
              messages: thread.messages.map((m) => (m.id === ui.id ? ui : m)),
            },
          },
        };
      });
    }
    await get().loadTasks(conversationId);
    void get().loadTaskCounts(conversationId);
  },

  async updateTaskStatus(conversationId, rootMessageId, status) {
    // threadByRoot is keyed by whatever openThread got: the full resource name
    // from the channel page (msg.id) or a bare id from the activity/reminder
    // detail embeds. The RPC wants the resource name, so strip a name down to
    // its message segment first.
    const rootId = rootMessageId.split("/").pop() ?? rootMessageId;
    const message = `conversations/${conversationId}/messages/${rootId}`;
    try {
      const res = await commandServiceClient.updateTaskStatus(
        create(UpdateTaskStatusRequestSchema, { message, status })
      );
      await get().patchTaskThreadAndRefresh(conversationId, rootMessageId, res);
    } catch (err) {
      // The UI toasts the failure; keep the stale thread/board cache intact.
      throw err;
    }
  },

  async assignTask(conversationId, rootMessageId, memberType, memberId) {
    const rootId = rootMessageId.split("/").pop() ?? rootMessageId;
    const message = `conversations/${conversationId}/messages/${rootId}`;
    try {
      const res = await commandServiceClient.assignTask(
        create(AssignTaskRequestSchema, { message, memberType, memberId })
      );
      await get().patchTaskThreadAndRefresh(conversationId, rootMessageId, res);
    } catch (err) {
      // The UI toasts the failure; keep the stale thread/board cache intact.
      throw err;
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
