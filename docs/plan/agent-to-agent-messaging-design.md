# Design: Name-Based Addressing + Agent-to-Agent Messaging

## Context

Laelia agents are always-on autonomous processes that communicate by posting chat messages via the
`laelia-agent` CLI. Today every agent command addresses a conversation by its canonical id form
`conversations/<id>` and every message by `conversations/<c>/messages/<m>`. The agent obtains these ids
only from server output (the turn batch, `message read`, `task list`) and is instructed to copy them
verbatim.

This works for **user-triggered** work: a user posts in a channel → the agent's turn batch carries
that channel's `conversations/<id>` → the agent replies there. It **breaks for agent-initiated work**,
which is exactly what we want to enable:

- **Agent-to-agent delegation** — jane wants to ask rei to fetch & analyze a doc. There is no
  conversation id yet; jane only knows rei by name.
- **Proactive agent→user DM**, reminders rooted at an agent-chosen target, etc.

The deeper problem: an agent cannot attach a file to a conversation that does not yet exist, because
attachments require uploading to a `conversations/<id>` that must already exist. The first message
into a fresh conversation therefore cannot carry an attachment.

**The refactor.** Switch the entire agent CLI/chattools/daemon surface from id-based addressing to
**name-based addressing**. The agent writes a human-readable *address* (`#channel-name`,
`dm:@peer-name`, `#channel-name:<thread-root>`); agent-side code resolves the address to the real
conversation/message id, **creating the conversation if it does not exist (DMs only)**. This solves
the first-message/attachment problem for free and removes the fragile "copy the id verbatim"
instruction.

**The feature.** On top of name-based addressing, agent-to-agent messaging becomes: jane runs
`message send dm:@rei --content "fetch & analyze doc X"`. The resolver creates (or reuses) the
jane↔rei DM, posts, and wakes rei. rei is woken next turn, reads the request, does the work, replies
`message send dm:@jane --content "..."`. jane is re-woken on the reply. Fully **async**, reusing the
existing durable-cursor + wake infrastructure — no new synchronous call path.

## Confirmed design decisions

1. **Async semantics.** Sender ends its turn after sending; the peer is woken via the existing
   `notifyConversationAgents` path and pulls the message next turn; the reply re-wakes the sender.
   No blocking, no new runtime shape.
2. **Flat peer agents.** No parent/child subagent hierarchy. Any agent can address any other agent.
3. **Visibility.** Agent-to-agent DMs do NOT appear in the user channel list. Admins CAN view an
   agent's agent-DMs in that agent's detail-page chat tab — view only (no composer, cannot send).
   Reuses the existing admin bypass in `requireConversationMember`; no new authz code.
4. **Channel title uniqueness.** Add `UNIQUE (title) WHERE type = 2`. Project is pre-launch, so no
   migration/backfill concerns. `#title` resolves unambiguously.
5. **Agents may create DMs, not channels.** `dm:@peer` (peer = agent or user) creates the DM if
   absent. `#title` resolves to an existing channel only; if not found → error (channels stay
   user-created).
6. **DM peer can be an agent or a user.** `dm:@peer` resolves the name to an agent (→ agent↔agent
   DM, new type 3) or a user (→ user↔agent DM, existing type 1). Agent can proactively DM a user.
7. **Agent-side resolution.** The chattools layer resolves addresses to ids before calling the
   (mostly unchanged, id-based) manager RPCs.

## Address grammar

One conversation address, one message address, parsed by a single agent-side resolver.

```
conversation-address ::= "#" <title>
                      | "dm:@" <peer-name>

message-address      ::= conversation-address ":" <message-uuid>
```

- `#<title>` — a channel (type 2). `title` is the unique channel title. Resolves by
  `FindChannelByTitle`. **No creation.** Not-found → `NOT_FOUND_FAILED`.
- `dm:@<peer-name>` — a DM. `peer-name` resolves to an agent (by display name / resource id) **or**
  a user (by principal name). Agent peer → type-3 agent-DM via `GetOrCreateAgentDM`. User peer →
  type-1 user DM via `GetOrCreateDirectConversation`. **Created if absent.** If `peer-name` matches
  both an agent and a user → `INVALID_ARGUMENT_FAILED` "ambiguous peer name".
- `<conv-addr>:<uuid>` — a thread root message inside that conversation. The `<uuid>` is the bare
  message id (thread roots always have an id, so threads never hit the "no id yet" problem). The
  parser locates the **last** `:` whose suffix is a valid UUID; that suffix is the message id,
  everything before is the conversation address. This tolerates `:` appearing in a channel title.

Legacy input compatibility: if an input already matches `conversations/<id>` or
`conversations/<c>/messages/<m>`, the resolver passes it through unchanged (one-form detection). This
keeps any internal/programmatic callers working and makes the transition non-breaking on the input
side. **Outputs** emit only the new name forms.

## Architecture overview

```
LLM ── shell ──> laelia-agent CLI ──unix socket──> daemon ──> chattools
                                                                   │
                                                  resolveAddress() │  (NEW: name → id, create DM if absent)
                                                                   ▼
                                          manager CommandServiceClient (id-based RPCs, mostly unchanged)
                                                                   │
                                                                   ▼
                                                  manager store (conversation/chat_message/cursor)
```

New manager RPCs feed the resolver (`ResolveChannelByTitle`, `GetOrCreateAgentDM`,
`GetOrCreateUserDM`). `ListPeerAgents` powers the `agent list` discovery tool. Everything else
(PostMessage, ListConversationMessages, tasks, reminders, files, threads) stays id-based; the
resolver feeds them ids.

## Data model changes

### `backend/manager/migration/latest.sql`

1. **New conversation type 3 = AGENT_DM.** Document `type: 1=DM(user+agent), 2=channel, 3=AGENT_DM`.
   No CHECK needed (type has none today).

2. **Agent-DM dedup columns** (race-free GetOrCreate, mirroring `insertDirectConversationSQL`):
   ```sql
   ALTER TABLE conversation ADD COLUMN IF NOT EXISTS agent_dm_a INTEGER REFERENCES agent(id) ON DELETE SET NULL;
   ALTER TABLE conversation ADD COLUMN IF NOT EXISTS agent_dm_b INTEGER REFERENCES agent(id) ON DELETE SET NULL;
   ALTER TABLE conversation ADD CONSTRAINT conversation_agent_dm_order_check
     CHECK (agent_dm_a IS NULL OR agent_dm_b IS NULL OR agent_dm_a < agent_dm_b);
   CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_agent_dm_unique
     ON conversation(agent_dm_a, agent_dm_b) WHERE type = 3;
   ```
   Invariant: for type 3 both set and `agent_dm_a < agent_dm_b` (store orders the pair before insert).
   NULL for type 1/2. The existing `idx_conversation_dm_unique` is partial on `type=1` so no collision.

3. **Channel title uniqueness** (decision 4):
   ```sql
   CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_channel_title_unique
     ON conversation(title) WHERE type = 2;
   ```
   Enforced by the DB. `CreateChannel`/`UpdateChannel` return a friendly `ALREADY_EXISTS`-style error
   on conflict (map `pgconn.PgCode 23505`).

4. **Owner of record for agent-DMs.** Reuse the existing `SYSTEM_BOT` principal `id=1`
   (`latest.sql:118`). Type-3 rows set `created_by = owner_id = 1`, `agent_id = NULL`. Agent-sent
   messages borrow `principal_id = 1` exactly as `PostMessage` already does
   (`command.go:779-783`, `principalID := 1; if conv.OwnerID > 0 { principalID = conv.OwnerID }` —
   `conv.OwnerID = 1` here). No nullable schema change, no new principal.

### Store layer — `backend/manager/store/`

- **Constants** in `conversation.go`:
  ```go
  const (
      ConversationTypeDM       int32 = 1
      ConversationTypeChannel  int32 = 2
      ConversationTypeAgentDM  int32 = 3
  )
  ```
- **`GetOrCreateAgentDM(ctx, agentAID, agentBID int) (*ConversationMessage, error)`** in
  `conversation.go` — order-independent, race-free, mirrors `GetOrCreateDirectConversation`
  (`conversation.go:39-103`):
  - Resolve both resource ids; order `lo, hi := min(a,b), max(a,b)`.
  - `INSERT … ON CONFLICT (agent_dm_a, agent_dm_b) WHERE type = 3 DO NOTHING RETURNING …`; on
    `sql.ErrNoRows` re-read the winning row.
  - On the create path: add both agents as `MemberTypeAgent`/`MemberRoleMember`, then
    `SeedCursorOnJoin` for both agent ids (start caught-up; only future messages surface).
  - `created_by = owner_id = 1`, `agent_id = NULL`, `type = 3`, `title = ''`.
- **`FindChannelByTitle(ctx, title string) (*ConversationMessage, error)`** —
  `SELECT … WHERE type = 2 AND title = $1` (unique by the new index). Returns `sql.ErrNoRows` if
  absent.
- **`FindUserByName(ctx, name string) (*Principal, error)`** (or reuse an existing principal lookup
  — verify in `backend/manager/store/`; a `principal.name` lookup may already exist). For `dm:@user`.
- **`ListPeerAgents`** uses the existing `store.ListAgents` (already returns name +
  `info.acp_config.persona_prompt`).

No change to `chat_message.go`, `agent_channel_cursor.go`, or `conversation_member.go` —
`CreateChatMessageBumpVersion`, `SeedCursorOnJoin`, `IsConversationMember`, membership-based
`ListChannelsWithUpdates`/`HasUpdates` already handle type 3 (two AGENT members) generically.

## Manager RPC changes — `proto/v1/v1/command.proto` + handlers

All new RPCs are agent-callable: **no `auth_method` annotation** (matches `PostMessage`/`CreateTask`,
CUSTOM path, identity from `GetAgentFromContext`).

```proto
rpc ResolveChannelByTitle(ResolveChannelByTitleRequest) returns (ResolveChannelByTitleResponse) {
  option (google.api.http) = { get: "/v1/agents/-/channels:resolveByTitle" };
}   // request: { title }  → response: { conversation }  (NOT_FOUND if absent; no creation)

rpc GetOrCreateUserDM(GetOrCreateUserDMRequest) returns (GetOrCreateUserDMResponse) {
  option (google.api.http) = { post: "/v1/agents/-/userDm:getOrCreate" body: "*" };
}   // request: { peer_user_name }  → response: { conversation }  (agent↔user DM, type 1)

rpc GetOrCreateAgentDM(GetOrCreateAgentDMRequest) returns (GetOrCreateAgentDMResponse) {
  option (google.api.http) = { post: "/v1/agents/-/agentDm:getOrCreate" body: "*" };
}   // request: { peer_agent }  // "agents/<id>"  → response: { conversation }  (agent↔agent DM, type 3)

rpc ListPeerAgents(ListPeerAgentsRequest) returns (ListPeerAgentsResponse) {
  option (google.api.http) = { get: "/v1/agents/-/peerAgents" };
}   // → { agents: [{ name, display_name, persona_prompt, connection_state }] }, caller excluded
```

Handlers in `backend/manager/api/v1/command.go` / `channel.go`:
- `ResolveChannelByTitle` → `store.FindChannelByTitle` → `convertToV1Conversation`.
- `GetOrCreateUserDM` → resolve `peer_user_name` to a user principal →
  `store.GetOrCreateDirectConversation(agent.ID, user.ID)`. (This is the agent-callable twin of the
  user-only `GetOrCreateConversation` at `command.go:525-551`.)
- `GetOrCreateAgentDM` → `GetAgentByResourceID(peer_agent)` →
  `store.GetOrCreateAgentDM(agent.ID, peer.ID)`; reject self-address (`CodeInvalidArgument`).
- `ListPeerAgents` → `store.ListAgents`, map to `PeerAgent` (trim to name/display/persona/state),
  exclude caller.

### Conversation proto — `address` field (the emit-side chokepoint)

Add to the `Conversation` message (`command.proto:352-372`):
```proto
string address = 11; // name-based display address: "#<title>" | "dm:@<peer>". Empty when N/A.
```
Populate it in **`convertToV1Conversation`** (`channel.go:562-574`, the single builder):
- type 2 → `"#" + title`
- type 1 → `"dm:@" + user display name` (the DM peer for the calling agent is the user)
- type 3 → `"dm:@" + peer agent display name`

The handler needs the caller's perspective to pick the *peer* for DM addresses (the "other" member).
`convertToV1Conversation` gains a `peerName string` param (callers already resolve owner/peer names
for `owner_name`/`title`; pass the peer name through). For `ListChannelsForAgent` and
`ListConversationMessages` responses this lets the frontend and chattools render addresses without an
extra round-trip.

### Guard the user-facing send path

`CommandService.SendMessage` (`channel.go:366`) rejects type 3:
```go
if conv.Type == store.ConversationTypeAgentDM {
    return nil, connect.NewError(connect.CodePermissionDenied,
        errors.New("agent-DM conversations are agent-only; users can view but cannot send"))
}
```
(fetched `conv` before the type check). User path into a type-3 DM is forbidden; admin **view**
remains via the existing admin bypass in `requireConversationMember`.

## Agent-side resolver — `backend/agent/chattools/`

New file `backend/agent/chattools/address.go`:

```go
// resolveConversationAddress turns "#title" | "dm:@peer" | legacy "conversations/<id>" into a
// canonical "conversations/<id>" resource name, creating DMs if absent.
func resolveConversationAddress(ctx context.Context, d Deps, addr string) (convID string, err error)

// splitMessageAddress splits "#title:<uuid>" | "dm:@peer:<uuid>" | legacy "conversations/<c>/messages/<m>"
// into (conversationAddress, messageID).
func splitMessageAddress(addr string) (convAddr, msgID string)

// resolveMessageAddress = resolveConversationAddress(splitMessageAddress(addr)) + msgID.
func resolveMessageAddress(ctx context.Context, d Deps, addr string) (convID, msgID string, err error)
```

Resolution logic:
1. If `addr` starts with `conversations/` → passthrough (legacy form).
2. If starts with `dm:` → parse peer name; resolve to agent (`ListPeerAgents`) or user
   (`FindUserByName`); ambiguous → error; call `GetOrCreateAgentDM` or `GetOrCreateUserDM`; return
   `conversations/<id>`.
3. If starts with `#` → strip `#`; call `ResolveChannelByTitle`; not-found → `NOT_FOUND_FAILED`.
4. Else → `INVALID_ARGUMENT_FAILED`.

The `Deps` struct (`chattools.go:29`) already carries `Client`; resolution adds at most one
round-trip per command (cached across a single CLI invocation by the daemon handler scope; not
across turns).

### Rewire input sites

Replace `normalizeConversationName` (`chattools.go:77-85`) usage with `resolveConversationAddress` at
every input site: `GetConversationMessages`, `PostMessage`, `AckProcessedVersion`, `UploadFile`,
`ListFiles`, `GetThreadMessages`, `PostThreadMessage`, `ListMembers`, `ListTasks`, `CreateTask`,
`ListReminders`, `SearchChatHistory` (the `Conversation` field of each input struct). Replace
`normalizeThreadRoot` (`chattools.go:92-100`) usage with `resolveMessageAddress` for the thread
`--root`/`Message`/`Name` fields:
- `--root` (thread read/send, members) → `resolveMessageAddress`.
- task `claim`/`unclaim`/`review`/`done` positional `<message-name>` → `resolveMessageAddress`.
- reminder `convert` positional `<message-name>` → `resolveMessageAddress` (root must be a top-level
  message; resolver returns its conversation id + message id).
- reminder `update`/`cancel`/`complete`/`fail` `<name>` = `reminders/{message_id}` → keep as-is
  (reminder names are not conversation addresses; the `{message_id}` is already an id and is the
  reminder's identity, not a conversation address). `reminders/<id>` stays id-based (keyed by message
  id, not conversation name).

`normalizeConversationName` is retained only as the legacy-form passthrough branch inside the
resolver; the old call sites stop calling it directly.

### Rewire output (emit) sites — replace ids with addresses

The `Conversation.address` field carries the name form, so emit sites switch to `conv.GetAddress()`
(or the resolved address the handler already holds):

| Emit site | Today | After |
|---|---|---|
| `formatMessageLine` `message:` line (`chattools.go:171`) | `conversations/<c>/messages/<m>` | `<address>:<message-id>` |
| ListChannelUpdates line (`chattools.go:419-420`) | `conversations/<id>` | `<address>` (+ version counts) |
| ListThreadUpdates line (`chattools.go:564-566`) | `<conv> thread <root>` | `<address>:<root-id>` |
| task line `name` (`chattools_task.go:76`) | `conversations/<c>/messages/<m>` | `<address>:<message-id>` |
| ClaimTask echo (`chattools_task.go:154-158`) | `<conv>`, `<root>` | `<address>`, `<address>:<root-id>` |
| CreateTask/Ack echoes | `conversations/<id>` | `<address>` |
| conflict-resolution strings (`chattools.go:400-401`, `692-693`) | `conversations/<id>` | `<address>` |
| members header (`chattools_channel.go:112,119`) | `conversations/<id>` | `<address>` |
| file list/upload headers (`chattools.go:512,473`) | `conversations/<id>` | `<address>` (file ids stay as-is) |
| `BuildTurnBatch` cursor (`turn_batch.go:69`) | `target (conversations/<id>, processed_version=…)` | `<address> (processed_version=…)` |
| reminder lines (`chattools_reminder.go:87,146,241,258,297,310`) | `reminders/{message_id}` | unchanged (id-based by design) |

`resolveChannelTarget` (`turn_batch.go:116-136`) is the existing precedent that emits `#title`/
`dm:@peer`; promote it into `address.go` as the id→address formatter reused by all emit sites (single
source of truth for the grammar). For type 3 add the `dm:@<peer-agent>` case.

## CLI changes — `backend/agent/cmd/`

- Positional `<conversation>` args become `<address>` args (semantically: `message send <address>`
  accepts `#title`, `dm:@peer`, or the legacy id form). Flag names unchanged.
- `message send`/`thread send`: the dest is a conversation address; `--root` takes a message
  address (`<address>:<uuid>` or legacy `conversations/<c>/messages/<m>`). The user's
  `message send --target "#channel:threadid"` sketch is satisfied by `message send` taking a
  conversation address and `thread send --root <address>:<uuid>` for replies; `--target` vs
  positional naming is cosmetic — keep the existing positional `<dest>` + `--root` to minimize
  churn. Threads are addressed by the `:<uuid>` suffix on `--root`.
- New command `agent list` → `laelia-agent agent list` (calls `ListPeerAgents`, renders the global
  roster with personas, like `members` but across all agents). File: new `backend/agent/cmd/agent.go`
  mirroring `members.go`.
- `daemon.Request` (`server.go:208-254`): the `Conversation`/`Root`/`Message` fields keep carrying
  the **address** strings now (the resolver runs inside chattools, after the daemon handler).
  Preferred: no new handler for agent-to-agent send; `message send dm:@rei` flows through the
  existing `/message/send` handler and the resolver. Add one daemon route `/agent/list` →
  `chattools.ListPeerAgents`.

## Prompt rewrite — `backend/agent/executor/prompt/communication.md` + `executor/prompt.go`

Replace every `conversations/<id>` / `conversations/<c>/messages/<m>` instruction with the address
grammar:
- `<address>` = `#<channel-title>` | `dm:@<peer-name>`.
- `<message-address>` = `<address>:<message-id>` (copy verbatim from `message read`/`task list`
  output).
- "You no longer copy long ids. Addresses are human-readable; `message send "#announcements"`,
  `message send dm:@rei`. The manager resolves the address; a DM is created if absent, a channel
  must already exist."
- New **"Delegating to a peer agent"** section: discover via `agent list` (global roster with
  personas), address `dm:@<agent>`, **async** — post and end your turn; the peer's reply wakes you
  next turn (do NOT poll or block). Reuse the same DM for the whole delegation thread.
- Update the `agent list` row in the Commands table.

## Frontend

- **`frontend/src/pages/dashboard/chat-conversation.tsx`**: add `CONVERSATION_TYPE_AGENT_DM = 3`;
  hide the composer (textarea/attach/as-task/Send, `handleSend`, Enter handler, mention popup) when
  `channel?.type === 3`; show a view-only banner `chat.agent-dm-view-only`.
- **`frontend/src/pages/dashboard/agent-chat.tsx`**: render type-3 rows with a distinct icon and the
  peer agent's name (already in `title`/`address` from the backend). Clicking navigates to
  `/chat/<convId>` as today; the composer guard above makes it view-only.
- **Left rail** (`conversation-list.tsx`): no change — user `ListChannels` excludes type 3 by
  membership.
- **Locale**: add `chat.agent-dm-view-only` to `frontend/src/locales/en-US.json`.
- proto-es types regenerate from `buf generate`; use `Conversation.address` + `conversationType===3`.

Admin view access is already granted by the existing admin bypass in `requireConversationMember`
(`authz_helper.go:51-57`); non-admin users are denied. No new authz code.

## Phasing (recommended delivery order)

- **Phase 1 — Name-based addressing foundation.** Schema (agent-DM columns + unique channel-title
  index), store (`GetOrCreateAgentDM`, `FindChannelByTitle`, user-by-name lookup), the four new RPCs
  + `Conversation.address`, the agent-side resolver, rewire all input + output sites, prompt rewrite.
  At the end of Phase 1, every command accepts `#title`/`dm:@peer` and emits addresses; `dm:@rei`
  already creates an agent-DM end-to-end.
- **Phase 2 — Agent-to-agent UX + frontend.** `ListPeerAgents` + `agent list` CLI, delegation
  prompt section, frontend view-only agent-DM rendering. (The send path already works from Phase 1.)

## Critical files

- `proto/v1/v1/command.proto` — 4 new RPCs, `Conversation.address`.
- `backend/manager/migration/latest.sql` — type 3, agent-DM columns/index/check, unique channel-title index.
- `backend/manager/store/conversation.go` — `GetOrCreateAgentDM`, `FindChannelByTitle`, type constants; `conversation_member.go` (no change expected).
- `backend/manager/api/v1/channel.go` — `convertToV1Conversation` (address field), `ResolveChannelByTitle`/`GetOrCreateAgentDM`/`GetOrCreateUserDM`/`ListPeerAgents` handlers, `SendMessage` type-3 guard, `ListChannelsForAgent` peer-name for type 3.
- `backend/manager/api/v1/command.go` — handler home for the new RPCs (alongside `PostMessage`); `parseConversationID` stays for id-form passthrough.
- `backend/agent/chattools/address.go` (NEW) — resolver; rewire all input/emit sites in `chattools.go`, `chattools_channel.go`, `chattools_task.go`, `chattools_reminder.go`, `turn_batch.go`.
- `backend/agent/daemon/server.go` — `/agent/list` route; address fields flow through existing handlers.
- `backend/agent/cmd/agent.go` (NEW) + `message.go`/`thread.go`/`task.go`/`reminder.go` — `<address>` args.
- `backend/agent/executor/prompt/communication.md` + `executor/prompt.go` — grammar + delegation docs.
- `frontend/src/pages/dashboard/chat-conversation.tsx` + `agent-chat.tsx` — view-only type 3.

## Verification

1. **Schema**: after migration, `\d conversation` shows `agent_dm_a`, `agent_dm_b`, the CHECK, both
   new unique indexes. Idempotent re-run (`IF NOT EXISTS`).
2. **Unit**: add `TestAgentDMUniqueIndexPresent` and `TestChannelTitleUniqueIndexPresent` in
   `backend/manager/migration/migration_test.go` (mirror `TestUniqueConstraintsPresent`); add
   resolver tests in `backend/agent/chattools` covering `#title` (found / not-found), `dm:@agent`
   (create + reuse), `dm:@user`, ambiguous peer, legacy `conversations/<id>` passthrough, and the
   `:<uuid>` thread-suffix split.
3. **Channel-title uniqueness**: a second `CreateChannel("dup")` → `ALREADY_EXISTS`.
4. **Delegation e2e** (two online agents jane, rei):
   - `laelia-agent agent list` from jane lists rei with its persona.
   - `laelia-agent message send dm:@rei --content "fetch & analyze doc X"`.
   - DB: one `conversation` row `type=3`, `created_by=owner_id=1`, `agent_dm_a/b` ordered, two AGENT
     members, one `chat_message` (`principal_id=1, sender_agent_id=jane, sender_type=2`); both
     agents' cursors seeded.
   - rei woken (`notifyConversationAgents` except jane); rei's `ListChannelsWithUpdates` returns the
     DM; turn batch renders `dm:@jane: 1 new`.
   - rei replies `message send dm:@jane --content "analysis: …"` (or `message send` to the same DM);
     jane re-woken, batch shows `dm:@rei`.
   - The DM does NOT appear in any user's left rail; it appears in both agents' detail-page chat
     tabs titled with the peer's name; an admin opening `/chat/<id>` sees messages with no composer;
     a non-admin is denied.
5. **Race**: two concurrent `GetOrCreateAgentDM(jane, rei)` produce one row (unique index +
   `ON CONFLICT DO NOTHING` + re-read).
6. **Self-address** `dm:@jane` from jane → `INVALID_ARGUMENT_FAILED`.
7. **Agent proactive user DM**: `message send dm:@<user> --content …` creates a type-1 user↔agent DM.
8. **First-message attachment**: `file upload <path>` without `--conversation`, then
   `message send dm:@rei --content … --attach <id>` — verify the file attaches to the freshly
   created agent-DM (resolution creates the DM before the upload/attach).
9. **Frontend**: `pnpm --dir frontend type-check` + `biome:check`; manual: agent-DM view-only in the
   agent detail tab.
10. **Backend**: `gofmt -w` on changed files; `golangci-lint run --allow-parallel-runners --fix` to
    clean; `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`.
11. **ACP executor test** (`LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1`):
    not required by default — the refactor adds CLI/daemon handlers and chattool resolution but does
    not touch the ACP stdio/runtime path. Run it if the prompt injection or turn-batch rendering is
    materially altered; the only turn-batch change is the cursor label format (display-only, no stdio
    change).

## Open notes (non-blocking)

- **Peer-name → user principal lookup**: confirm whether a `principal.name` lookup already exists in
  `backend/manager/store/`; if not, add a minimal `FindUserByName`. (Agents and users share a name
  namespace for `dm:@peer`; collision → ambiguous error, by design.)
- **`:` in channel titles**: the thread-suffix parser uses "last `:` followed by a valid UUID" so
  `:` in a title is tolerated; document this in the prompt so agents don't hand-construct addresses.
- **Reminder names** stay id-based (`reminders/{message_id}`) since a reminder is keyed by its root
  message id, not a conversation name; the resolver does not touch them.