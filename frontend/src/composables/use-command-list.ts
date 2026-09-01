import { create } from "@bufbuild/protobuf";
import { useQuery } from "@tanstack/react-query";
import { commandServiceClient } from "@/connect";
import type { Command } from "@/types/proto-es/v1/command_pb";
import {
  CommandStatus,
  ListCommandsRequestSchema,
} from "@/types/proto-es/v1/command_pb";

// ---------------------------------------------------------------------------
// use-command-list — the agent command list page's read primitive (ADR-1).
//
// The page used to read through the store's listCommands action, which kept a
// module-level `commands` cache with zero other readers and hand-rolled the
// page-token/race mechanics in lib/use-resource-list.ts. Everything dissolves
// into Query here:
//   - one query per (agent, status, pageToken) — request ordering is the key,
//     so racing tabs/pages can never overwrite each other;
//   - pagination keeps the previous page visible via a scoped placeholderData:
//     it only carries the previous page forward when the (agent, status) view
//     is unchanged, so a filter/agent switch still resets to the skeleton;
//   - post-send refreshes are plain cache invalidations.
// Query root: ["commands", "list"]. The token stack itself stays in the page —
// it is pagination UI state, not server cache.
// ---------------------------------------------------------------------------

export const COMMAND_LIST_QUERY_ROOT = ["commands", "list"] as const;

// Server page size for the list; a constant, so it stays out of the key.
export const COMMAND_LIST_PAGE_SIZE = 50;

export interface CommandListPage {
  commands: Command[];
  nextPageToken: string;
}

export function useCommandListPage(opts: {
  agent: string;
  status: CommandStatus;
  pageToken: string;
}) {
  const { agent, status, pageToken } = opts;
  // The explicit generic anchors inference: the placeholderData callback's
  // contextual typing alone leaves TQueryData at {}.
  return useQuery<CommandListPage>({
    queryKey: [...COMMAND_LIST_QUERY_ROOT, agent, status, pageToken],
    // One RPC attempt per (re)load — pages own their failure UX, and the
    // empty-page fallback below keeps the list page's no-error-banner look.
    retry: false,
    placeholderData: (previous, previousQuery) => {
      // Page turn within the same filtered view: keep the previous page's
      // rows on screen while the next page loads (the pager shows the
      // refreshing state). A different agent/status is a new view — no
      // placeholder, so the skeleton renders.
      if (!previous || !previousQuery) return undefined;
      const [, prevAgent, prevStatus] = previousQuery.queryKey;
      if (prevAgent !== agent || prevStatus !== status) return undefined;
      return previous;
    },
    queryFn: async ({ signal }): Promise<CommandListPage> => {
      const res = await commandServiceClient.listCommands(
        create(ListCommandsRequestSchema, {
          agent,
          pageSize: COMMAND_LIST_PAGE_SIZE,
          pageToken,
          status,
        }),
        { signal }
      );
      return { commands: res.commands, nextPageToken: res.nextPageToken };
    },
  });
}
