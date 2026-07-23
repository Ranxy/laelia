import type { AppSliceCreator, MembersSlice } from "./types";

// MembersSlice owns the flat workspace directory: humans (from the user roster)
// and agents (from the agent roster) merged into a single list, sorted by title.
// It does not own the underlying rosters — those live in UserSlice / AgentSlice —
// it only derives the merged view. fetchMembers triggers both source fetches and
// recomputes the merge; the members page calls it on mount.
export const createMembersSlice: AppSliceCreator<MembersSlice> = (
  set,
  get
) => ({
  members: [],
  membersLoading: false,

  async fetchMembers() {
    set({ membersLoading: true });
    try {
      // Fetch both rosters in parallel; each populates its own slice so the
      // Machines / Agents / Settings pages stay consistent with this view.
      const [usersRes, agentsRes] = await Promise.all([
        get().fetchUsers({ pageSize: 100 }),
        get().fetchAgents({ pageSize: 100 }),
      ]);
      const users = get().users;
      const agents = get().agents;

      const members = [
        ...users.map((u) => ({
          kind: "user" as const,
          name: u.name,
          title: u.title || u.email || u.name,
          subtitle: u.email,
        })),
        ...agents.map((a) => ({
          kind: "agent" as const,
          name: a.name,
          title: a.title || a.name,
          subtitle: a.machine || "",
          connectionState: a.status?.state,
        })),
      ].sort((x, y) => x.title.localeCompare(y.title));

      set({ members, membersLoading: false });
      return {
        usersNextPageToken: usersRes?.nextPageToken ?? "",
        agentsNextPageToken: agentsRes?.nextPageToken ?? "",
      };
    } catch {
      set({ members: [], membersLoading: false });
      return undefined;
    }
  },
});
