import type { AppSliceCreator, MembersSlice } from "./types";

// Page cap for the roster drain loop: guards against a runaway server cursor
// (or a bug that never returns an empty nextPageToken) consuming the UI thread.
// 50 pages * 100 rows = 5000 rows per roster, well past any realistic workspace.
const MAX_DRAIN_PAGES = 50;

// MembersSlice owns the flat workspace directory: humans (from the user roster)
// and agents (from the agent roster) merged into a single list, sorted by title.
// It does not own the underlying rosters — those live in UserSlice / AgentSlice —
// it only derives the merged view. fetchMembers drains *every* page of both
// rosters (not just the first) so a workspace with >100 users or agents is not
// silently truncated, surfaces a load failure via `membersError`, and writes
// the full rosters back into the source slices so the Machines / Agents /
// Settings pages stay consistent with this view.
export const createMembersSlice: AppSliceCreator<MembersSlice> = (
  set,
  get
) => ({
  members: [],
  membersLoading: false,
  membersError: false,

  async fetchMembers(params) {
    const silent = params?.silent;
    // Silent (background) refreshes keep the cached roster visible instead of
    // swapping the rail to a "Loading…" state on every re-entry.
    if (!silent) set({ membersLoading: true, membersError: false });
    try {
      // Drain the user and agent rosters in parallel (they touch independent
      // slices) — the sequential loop previously doubled the wall-clock time.
      const [users, agents] = await Promise.all([
        drainRoster(
          (pageToken) => get().fetchUsers({ pageSize: 100, pageToken }),
          () => get().users,
          (rows) => set({ users: rows })
        ),
        drainRoster(
          (pageToken) => get().fetchAgents({ pageSize: 100, pageToken }),
          () => get().agents,
          (rows) => set({ agents: rows })
        ),
      ]);
      // fetchUsers/fetchAgents return undefined on a failed fetch and clear
      // their slice to []; either roster failing means the directory is not
      // trustworthy, so we surface a load error rather than a partial list.
      if (users === undefined || agents === undefined) {
        if (!silent) {
          set({ members: [], membersLoading: false, membersError: true });
        }
        return undefined;
      }

      const userTitleByName = new Map(
        users.map((u) => [u.name, u.title || u.email || u.name])
      );
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
          subtitle: a.owner
            ? userTitleByName.get(a.owner) || a.owner.replace(/^users\//, "")
            : "",
          connectionState: a.status?.state,
          enabled: a.enabled,
        })),
      ].sort((x, y) => x.title.localeCompare(y.title));

      set({ members, membersLoading: false });
      return { usersNextPageToken: "", agentsNextPageToken: "" };
    } catch {
      if (!silent) {
        set({ members: [], membersLoading: false, membersError: true });
      }
      return undefined;
    }
  },
});

// drainRoster walks every page of a source slice's paged fetch. After each
// successful page it reads the just-populated slice (the fetch overwrites its
// slice with the current page) and accumulates the rows. It returns the full
// accumulated list on success, or undefined if any page failed (the source
// fetch returns undefined and clears its slice on error). On success it also
// writes the full list back into the source slice, so the rest of the app —
// which reads `users`/`agents` directly — sees the complete roster rather than
// only the last page.
async function drainRoster<T>(
  fetchPage: (
    pageToken: string
  ) => Promise<{ nextPageToken: string } | undefined>,
  readSlice: () => T[],
  writeSlice: (rows: T[]) => void
): Promise<T[] | undefined> {
  const all: T[] = [];
  let pageToken = "";
  for (let page = 0; page < MAX_DRAIN_PAGES; page++) {
    const res = await fetchPage(pageToken);
    if (res === undefined) {
      return undefined;
    }
    all.push(...readSlice());
    pageToken = res.nextPageToken;
    if (!pageToken) {
      break;
    }
  }
  writeSlice(all);
  return all;
}
