import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ActivitySchema } from "@/types/proto-es/v1/command_pb";
import { useAppStore } from "./index";

// --- mock @/connect so the activity slice talks to controllable feeds ---
const mock = vi.hoisted(() => ({
  listActivities: vi.fn(),
  markActivityDone: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listActivities: mock.listActivities,
    markActivityDone: mock.markActivityDone,
  },
}));

const A = create(ActivitySchema, {
  name: "users/1/activities/a",
  conversation: "conversations/c1",
  message: "messages/m1",
  categories: 2,
  state: 1, // UNREAD
  roomVersion: 1n,
  summary: "task one",
  senderName: "Alice",
  senderType: 1,
});
const B = create(ActivitySchema, {
  ...A,
  name: "users/1/activities/b",
  summary: "task two",
});
const C = create(ActivitySchema, {
  ...A,
  name: "users/1/activities/c",
  summary: "task three",
});
const A_DONE = create(ActivitySchema, {
  ...A,
  state: 3, // DONE
});

beforeEach(() => {
  useAppStore.setState({
    activities: [],
    activitiesLoading: false,
    activitiesNextPageToken: "",
  });
  mock.listActivities.mockReset();
  mock.markActivityDone.mockReset();
});

describe("activity list", () => {
  it("drops a locally done row on the next silent poll", async () => {
    mock.listActivities
      .mockResolvedValueOnce({ activities: [A, B], nextPageToken: "" })
      .mockResolvedValueOnce({ activities: [B], nextPageToken: "" });
    mock.markActivityDone.mockResolvedValue({ activity: A_DONE });

    await useAppStore.getState().listActivities({});
    expect(useAppStore.getState().activities).toHaveLength(2);

    await useAppStore.getState().markActivityDone(A.name);
    // The row is DONE in the store but still present until the next fetch.
    expect(useAppStore.getState().activities[0].state).toBe(3);

    await useAppStore.getState().listActivities({ silent: true });
    expect(useAppStore.getState().activities.map((a) => a.name)).toEqual([
      B.name,
    ]);
  });

  it("keeps non-done rows from later pages during a silent poll", async () => {
    mock.listActivities
      .mockResolvedValueOnce({ activities: [A, B], nextPageToken: "" })
      .mockResolvedValueOnce({ activities: [A, B], nextPageToken: "" });

    await useAppStore.getState().listActivities({});
    // Simulate a later page appended via infinite scroll.
    useAppStore.setState((s) => ({
      activities: [...s.activities, C],
    }));

    await useAppStore.getState().listActivities({ silent: true });
    expect(useAppStore.getState().activities.map((a) => a.name)).toEqual([
      A.name,
      B.name,
      C.name,
    ]);
  });
});
