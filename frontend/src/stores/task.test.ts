import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  AssignTaskRequestSchema,
  ChatMessageSchema,
  CloseTaskRequestSchema,
  TaskInfoSchema,
  TaskStatus,
  UpdateTaskStatusRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { toUiMessage } from "./chat-helpers";
import { useAppStore } from "./index";

// --- mock @/connect so closeTask talks to a controllable client ---
const mock = vi.hoisted(() => ({
  closeTask: vi.fn(),
  updateTaskStatus: vi.fn(),
  assignTask: vi.fn(),
  listTasks: vi.fn(),
  listTaskCounts: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    closeTask: mock.closeTask,
    updateTaskStatus: mock.updateTaskStatus,
    assignTask: mock.assignTask,
    listTasks: mock.listTasks,
    listTaskCounts: mock.listTaskCounts,
  },
}));

const fixedTimestamp = create(TimestampSchema, {
  seconds: 1_700_000_000n,
  nanos: 0,
});

function taskMessage(status: number, content: string) {
  return create(ChatMessageSchema, {
    name: "conversations/c1/messages/m1",
    conversation: "conversations/c1",
    principalName: "users/1",
    role: 1,
    content,
    createdAt: fixedTimestamp,
    senderName: "users/1",
    senderType: 1,
    roomVersion: 1n,
    mentions: [],
    attachments: [],
    isOwn: false,
    task: create(TaskInfoSchema, { taskNumber: 7, status }),
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.getState().reset();
  useAppStore.setState({
    threadByRoot: {
      "conversations/c1/messages/m1": {
        messages: [toUiMessage(taskMessage(TaskStatus.IN_PROGRESS, "root"))],
        currentVersion: 1n,
        loading: false,
      },
    },
  });
  mock.listTasks.mockResolvedValue({ tasks: [], nextPageToken: "" });
  mock.listTaskCounts.mockResolvedValue({
    todoCount: 0,
    inProgressCount: 0,
    inReviewCount: 0,
    doneCount: 1,
  });
});

describe("task store closeTask", () => {
  it("marks the task DONE via the RPC and patches the open thread root", async () => {
    mock.closeTask.mockResolvedValue({
      message: taskMessage(TaskStatus.DONE, "root"),
    });

    await useAppStore
      .getState()
      .closeTask("c1", "conversations/c1/messages/m1");

    expect(mock.closeTask).toHaveBeenCalledTimes(1);
    const req = mock.closeTask.mock.calls[0][0];
    expect(req.message).toBe("conversations/c1/messages/m1");

    const root =
      useAppStore.getState().threadByRoot["conversations/c1/messages/m1"]
        .messages[0];
    expect(root.task?.status).toBe(TaskStatus.DONE);

    // Board + counts refresh after a close, like convertMessageToTask.
    expect(mock.listTasks).toHaveBeenCalled();
    expect(mock.listTaskCounts).toHaveBeenCalled();
  });

  it("throws on failure and leaves the thread untouched", async () => {
    mock.closeTask.mockRejectedValue(new Error("boom"));

    await expect(useAppStore.getState().closeTask("c1", "m1")).rejects.toThrow(
      "boom"
    );

    const root =
      useAppStore.getState().threadByRoot["conversations/c1/messages/m1"]
        .messages[0];
    expect(root.task?.status).toBe(TaskStatus.IN_PROGRESS);
    expect(mock.listTasks).not.toHaveBeenCalled();
  });

  it("normalizes a bare root id (detail-page shape) to a resource name", async () => {
    mock.closeTask.mockResolvedValue({});

    await useAppStore.getState().closeTask("c1", "m1");

    expect(mock.closeTask.mock.calls[0][0].message).toBe(
      "conversations/c1/messages/m1"
    );
  });

  it("sends the resource name shape the RPC expects", async () => {
    mock.closeTask.mockResolvedValue({});
    const expected = create(CloseTaskRequestSchema, {
      message: "conversations/c1/messages/m1",
    });

    await useAppStore
      .getState()
      .closeTask("c1", "conversations/c1/messages/m1");

    expect(mock.closeTask.mock.calls[0][0]).toEqual(expected);
  });
});

describe("task store updateTaskStatus", () => {
  it("moves the task to the target status and patches the open thread root", async () => {
    mock.updateTaskStatus.mockResolvedValue({
      message: taskMessage(TaskStatus.DONE, "root"),
    });

    await useAppStore
      .getState()
      .updateTaskStatus("c1", "conversations/c1/messages/m1", TaskStatus.DONE);

    expect(mock.updateTaskStatus).toHaveBeenCalledTimes(1);
    const req = mock.updateTaskStatus.mock.calls[0][0];
    expect(req.message).toBe("conversations/c1/messages/m1");
    expect(req.status).toBe(TaskStatus.DONE);

    const root =
      useAppStore.getState().threadByRoot["conversations/c1/messages/m1"]
        .messages[0];
    expect(root.task?.status).toBe(TaskStatus.DONE);

    expect(mock.listTasks).toHaveBeenCalled();
    expect(mock.listTaskCounts).toHaveBeenCalled();
  });

  it("sends the resource name shape the RPC expects", async () => {
    mock.updateTaskStatus.mockResolvedValue({});
    const expected = create(UpdateTaskStatusRequestSchema, {
      message: "conversations/c1/messages/m1",
      status: TaskStatus.IN_REVIEW,
    });

    await useAppStore
      .getState()
      .updateTaskStatus(
        "c1",
        "conversations/c1/messages/m1",
        TaskStatus.IN_REVIEW
      );

    expect(mock.updateTaskStatus.mock.calls[0][0]).toEqual(expected);
  });

  it("throws on failure and leaves the thread untouched", async () => {
    mock.updateTaskStatus.mockRejectedValue(new Error("boom"));

    await expect(
      useAppStore.getState().updateTaskStatus("c1", "m1", TaskStatus.DONE)
    ).rejects.toThrow("boom");

    const root =
      useAppStore.getState().threadByRoot["conversations/c1/messages/m1"]
        .messages[0];
    expect(root.task?.status).toBe(TaskStatus.IN_PROGRESS);
    expect(mock.listTasks).not.toHaveBeenCalled();
  });
});

describe("task store assignTask", () => {
  it("assigns a member and patches the open thread root", async () => {
    mock.assignTask.mockResolvedValue({
      message: taskMessage(TaskStatus.IN_PROGRESS, "root"),
    });

    await useAppStore
      .getState()
      .assignTask("c1", "conversations/c1/messages/m1", 2, "rei-agent-1");

    expect(mock.assignTask).toHaveBeenCalledTimes(1);
    const req = mock.assignTask.mock.calls[0][0];
    expect(req.message).toBe("conversations/c1/messages/m1");
    expect(req.memberType).toBe(2);
    expect(req.memberId).toBe("rei-agent-1");

    expect(mock.listTasks).toHaveBeenCalled();
    expect(mock.listTaskCounts).toHaveBeenCalled();
  });

  it("sends the resource name shape the RPC expects", async () => {
    mock.assignTask.mockResolvedValue({});
    const expected = create(AssignTaskRequestSchema, {
      message: "conversations/c1/messages/m1",
      memberType: 1,
      memberId: "ran-user-1",
    });

    await useAppStore
      .getState()
      .assignTask("c1", "conversations/c1/messages/m1", 1, "ran-user-1");

    expect(mock.assignTask.mock.calls[0][0]).toEqual(expected);
  });

  it("throws on failure and leaves the thread untouched", async () => {
    mock.assignTask.mockRejectedValue(new Error("boom"));

    await expect(
      useAppStore.getState().assignTask("c1", "m1", 2, "rei-agent-1")
    ).rejects.toThrow("boom");

    const root =
      useAppStore.getState().threadByRoot["conversations/c1/messages/m1"]
        .messages[0];
    expect(root.task?.status).toBe(TaskStatus.IN_PROGRESS);
    expect(mock.listTasks).not.toHaveBeenCalled();
  });
});
