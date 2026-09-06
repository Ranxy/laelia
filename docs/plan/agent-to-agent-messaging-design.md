# Design: Name-Based Addressing + Agent-to-Agent Messaging

> Status: verified and updated against the current code on 2026-09-06. Main changes: the refactor and both phases shipped — name-based addressing (`#title` / `dm:@peer`), the four resolver RPCs, type-3 agent DMs, `agent list`, the prompt rewrite, and the frontend view-only UI are all implemented; this revision records the as-built deltas (handle-based user-DM addressing, no legacy message-path passthrough, no `persona_prompt` exposure) and the newer additions it now coexists with (type-4 user DMs, `ListAccessibleChannels`, owner-follow read scope).

## Context

> Note: the paragraphs below describe the **pre-refactor** state as originally written; the
> refactor described here has since been implemented (see the status line above and the
> "Phasing" / "Implementation status" sections).

Laelia agents are always-on autonomous processes that communicate by posting chat messages via the
`laelia-machine` CLI. Today every agent command addresses a conversation by its canonical id form
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
   As built, admin read access is a dedicated grantable workspace permission
   `laelia.conversations.reviewAgentDM` (held by `workspaceAdmin`, not in the member baseline —
   see `backend/manager/store/predefined_roles.go`): the IAM engine's conversation check
   (`backend/manager/component/iam/manager.go`, non-member override) lets a holder **read** a
   type-3 DM but never send or manage it. The send/upload side rejects type 3 explicitly (see
   "Guard the user-facing send path" below).
4. **Channel title uniqueness.** Add `UNIQUE (title) WHERE type = 2`. Project is pre-launch, so no
   migration/backfill concerns. `#title` resolves unambiguously. — **Implemented**
   (`idx_conversation_channel_title_unique` in `LATEST.sql`).
5. **Agents may create DMs, not channels.** `dm:@peer` (peer = agent or user) creates the DM if
   absent. `#title` resolves to an existing channel only; if not found → `NOT_FOUND` (channels stay
   user-created). — **Implemented as designed.**
6. **DM peer can be an agent or a user.** `dm:@peer` resolves the name to an agent (→ agent↔agent
   DM, type 3) or a user (→ user↔agent DM, type 1). Agent can proactively DM a user. As built, the
   agent peer is addressed by display name or `agents/<resource-id>` handle, and the user peer is
   resolved **by mention handle** (`GetUserByHandle`), not by display name. — **Implemented with
   that delta.**
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
- `dm:@<peer-name>` — a DM. `peer-name` resolves to an agent (by display name, or unambiguously by
  its `agents/<id>` handle) **or** a user (by **mention handle** — `GetUserByHandle`; the design's
  principal-name lookup became a handle lookup, and handle uniqueness removes the cross-namespace
  ambiguity). Agent peer → type-3 agent-DM via `GetOrCreateAgentDM`. User peer → type-1 user DM via
  `GetOrCreateDirectConversation`. **Created if absent.**
- `<conv-addr>:<uuid>` — a thread root message inside that conversation. The `<uuid>` is the bare
  message id (thread roots always have an id, so threads never hit the "no id yet" problem). The
  parser locates the **last** `:` whose suffix is a valid UUID; that suffix is the message id,
  everything before is the conversation address. This tolerates `:` appearing in a channel title.

Legacy input compatibility (as built): a well-formed `conversations/<id>` resource name is accepted
as a passthrough — but only for conversations the agent can already read, because the manager
re-validates read permission on every use; this is how owner-visible DMs surfaced by `channel list`
(that have no name-form address) are addressed. **Bare ids and
`conversations/<c>/messages/<m>` paths are rejected as input** — the design's one-form detection for
full message paths was dropped in favor of stricter validation. **Outputs** emit only the new name
forms (channel handles are emitted single-quoted, see the resolver section).

## Architecture overview

```
LLM ── shell ──> laelia-machine CLI ──unix socket──> daemon ──> chattools
                                                                   │
                                                  resolveAddress() │  (NEW: name → id, create DM if absent)
                                                                   ▼
                                          manager CommandServiceClient (id-based RPCs, mostly unchanged)
                                                                   │
                                                                   ▼
                                                  manager store (conversation/chat_message/cursor)
```

New manager RPCs feed the resolver (`ResolveChannelByTitle`, `GetOrCreateAgentDM`,
`GetOrCreateUserDM`) — **implemented** in `backend/manager/api/v1/address_resolver.go`.
`ListPeerAgents` powers the `agent list` discovery tool — **implemented** in the same file.
Everything else (PostMessage, ListConversationMessages, tasks, reminders, files, threads) stays
id-based; the resolver feeds them ids. The emit side is the mirror image:
`Conversation.address` (populated by `convertToV1Conversation` in
`backend/manager/api/v1/channel_convert.go`) is the manager-side single source of the grammar, and
the agent-side `conversationAddress`/`messageHandle` helpers in
`backend/agent/chattools/address.go` render it into output lines.

## Data model changes

### `backend/manager/migration/migration/LATEST.sql`

> All of the schema below is **implemented**. Note the schema has since grown a
> **conversation type 4 = USER_DM** (user↔user DM, `GetOrCreateUserUserDM` +
> `idx_conversation_user_dm_unique`, migration `1.1/0002##user-user-dm.sql`), which the original
> design did not anticipate; type 3 remains as designed.

1. **New conversation type 3 = AGENT_DM.** Document `type: 1=DM(user+agent), 2=channel, 3=AGENT_DM,
   4=USER_DM`. No CHECK needed (type has none today).

2. **Agent-DM dedup columns** (race-free GetOrCreate, mirroring `insertDirectConversationSQL`) —
   `LATEST.sql` adds exactly this shape (`agent_dm_a`/`agent_dm_b`, the
   `conversation_agent_dm_order_check` CHECK, and the partial unique index
   `idx_conversation_agent_dm_unique ... WHERE type = 3`).
   Invariant: for type 3 both set and `agent_dm_a < agent_dm_b` (store orders the pair before insert).
   NULL for type 1/2. The existing `idx_conversation_dm_unique` is partial on `type=1` so no collision.

3. **Channel title uniqueness** (decision 4): `idx_conversation_channel_title_unique ON
   conversation(title) WHERE type = 2` — implemented in `LATEST.sql`. Enforced by the DB; the
   create/update paths surface the conflict as an `ALREADY_EXISTS`-style error.

4. **Owner of record for agent-DMs.** Reuse the existing `SYSTEM_BOT` principal `id=1`
   (`LATEST.sql:135`). Type-3 rows set `created_by = owner_id = 1` (`common.SystemBotID`), and the
   store sets `agent_id = NULL`. Agent-sent messages borrow `principal_id = 1` exactly as the
   posting path already does (`command_message.go:430-431`,
   `principalID := 1; if conv.OwnerID > 0 { principalID = conv.OwnerID }` — `conv.OwnerID = 1`
   here). No nullable schema change, no new principal.

### Store layer — `backend/manager/store/`

- **Constants** in `conversation.go` — implemented, plus the later type 4:
  ```go
  const (
      ConversationTypeDM       int32 = 1
      ConversationTypeChannel  int32 = 2
      ConversationTypeAgentDM  int32 = 3
      ConversationTypeUserDM   int32 = 4 // added after this design (user↔user DM)
  )
  ```
- **`GetOrCreateAgentDM(ctx, agentAID, agentBID int) (*ConversationMessage, error)`** —
  **implemented** in `conversation.go`: resolves both resource ids, orders `lo, hi := min(a,b),
  max(a,b)`, uses `insertAgentDMSQL` (`INSERT ... ON CONFLICT (agent_dm_a, agent_dm_b) WHERE type = 3
  DO NOTHING RETURNING ...`); on `sql.ErrNoRows` re-reads the winning row (`findAgentDM`). On the
  create path it adds both agents as members and seeds their per-channel cursors
  (`SeedCursorOnJoin`), `created_by = owner_id = 1`, `type = 3`, `title = ''`.
- **`FindChannelByTitle(ctx, title string)`** — **implemented** in `conversation.go`; `SELECT ...
  WHERE type = 2 AND title = $1`, returns `(nil, nil)` when absent so callers map absence to
  `NOT_FOUND`.
- **User lookup for `dm:@user`** — as built, the peer is resolved **by mention handle**:
  `store.GetUserByHandle(ctx, handle)` (mention handles are unique, so no user/agent ambiguity
  arises the way display-name lookup would). The design's `FindUserByName` was **not** added.
- **`ListPeerAgents`** uses the existing `store.ListAgents`.

No change to `chat_message.go`, `agent_channel_cursor.go`, or `conversation_member.go` —
`CreateChatMessageBumpVersion`, `SeedCursorOnJoin`, membership-based update listing already handle
type 3 (two AGENT members) generically.

## Manager RPC changes — `proto/v1/v1/command.proto` + handlers

All four RPCs are **implemented** in `proto/v1/v1/command.proto` (`ResolveChannelByTitle`,
`GetOrCreateUserDM`, `GetOrCreateAgentDM`, `ListPeerAgents`) — all agent-callable, **no
`auth_method` annotation** (identity from `GetAgentFromContext`). As built they carry **no
`google.api.http` options** — the doc's HTTP-rule sketch below was not applied.

Handlers live in `backend/manager/api/v1/address_resolver.go` (on `CommandService`; the design's
guess of `command.go`/`channel.go` shifted when the message paths were split out):

- `ResolveChannelByTitle` → `store.FindChannelByTitle` → `convertToV1Conversation`. The handler
  **doubles as the address gate**: before answering it runs
  `s.iam.CheckPermission(permission.ConversationsRead, agent, <conversation>)` and maps a deny to
  `NOT_FOUND` so existence cannot be probed (fail-closed).
- `GetOrCreateUserDM` → request field is `peer_user_handle` (the string typed after `dm:@`),
  resolved via `store.GetUserByHandle` → `store.GetOrCreateDirectConversation(agent.ID, user.ID)`.
  This is the agent-callable twin of the user-only `GetOrCreateConversation`
  (`command_message.go:19`).
- `GetOrCreateAgentDM` → request field `peer_agent` ("agents/<id>") →
  `store.GetAgentByResourceID` → `store.GetOrCreateAgentDM(agent.ID, peer.ID)`; self-address →
  `CodeInvalidArgument`.
- `ListPeerAgents` → `store.ListAgents`, map to `PeerAgent`, exclude caller. As built the `PeerAgent`
  carries `name`, `handle` (`agents/<id>`), `display_name`, `description`, `connection_state`,
  `enabled` — the peer's private `persona_prompt` is **never exposed** (the field exists but is
  always empty), which deliberately narrows the design's original "persona_prompt" plan.

### Conversation proto — `address` field (the emit-side chokepoint) — implemented

`Conversation.address = 11` exists in `command.proto` and is populated by the single builder
`convertToV1Conversation` (`backend/manager/api/v1/channel_convert.go`):
- type 2 → `"#" + title`
- types 1 / 3 / 4 → `"dm:@" + peerName` (the resolved DM peer; empty when no peer was resolved)

The builder signature grew beyond the design's sketch to
`convertToV1Conversation(conv, ownerName, ownerHandle, peerName, peerResourceName, memberCount,
unreadCount, title, readVersion)`; callers resolve the peer name/resource per viewer (see
`resolveAccessibleDisplay` for the owner-follow-visible DMs case, where no address is emitted so
`dm:@` cannot be pointed at a conversation the agent is not a member of).

### Guard the user-facing send path — implemented

`CommandService.SendMessage` now lives in `backend/manager/api/v1/channel_message.go` (the design's
`channel.go:366` moved) and rejects type 3:
```go
return nil, connect.NewError(connect.CodePermissionDenied,
    errors.New("agent-DM conversations are agent-only; users can view but cannot send"))
```
The file-upload path has the matching guard (`backend/manager/api/v1/channel_file_service.go`:
"agent-DM conversations are agent-only; users can view but cannot upload"). The admin **view**
remains via the `reviewAgentDM` IAM override described under decision 3.

## Agent-side resolver — `backend/agent/chattools/` — implemented

`backend/agent/chattools/address.go` exists and implements the resolver (with tests in
`address_test.go`). The as-built function set:

```go
// resolveConversationAddress: "#title" | "dm:@peer" | "conversations/<uuid>" → canonical
// "conversations/<id>" (DMs created if absent; channels never created). Empty input resolves to
// "" so optional callers (search, upload) pass through. Anything else → INVALID_ARGUMENT_FAILED.
func resolveConversationAddress(ctx context.Context, d Deps, addr string) (convID string, err error)

// splitMessageAddress: "<addr>:<message-uuid>" → ("<addr>", "<uuid>") (suffix must parse as a
// UUID, so ':' inside a channel title is tolerated); a bare token returns (token, "").
func splitMessageAddress(addr string) (convAddr, msgID string)

// resolveThreadRoot: thread ops' --conversation + --root → (conversation name, bare root id).
// The root is a bare message id or a "<addr>:<uuid>" handle — never a legacy full name.
func resolveThreadRoot(ctx context.Context, d Deps, conv, root string) (convName, rootID string, err error)

// resolveMessageName: "<addr>:<uuid>" → "conversations/<c>/messages/<m>" for task/reminder RPCs;
// a bare token with no message id is rejected.
func resolveMessageName(ctx context.Context, d Deps, addr string) (string, error)

// conversationAddress: id → display address via GetChannel + the manager-populated Address field
// (display-label fallback to the resource name). messageHandle/quoteAddress: emit-side helpers —
// messageHandle builds "<address>:<message-id>" copyable handles and single-quotes '#' addresses
// so a bare "#general" is not eaten as a shell comment.
func conversationAddress(ctx context.Context, d Deps, name string) string
```

Resolution logic: `dm:` → parse peer; agent peers go to `GetOrCreateAgentDM` (by display name or
`agents/<id>` handle via `ListPeerAgents`), user peers to `GetOrCreateUserDM` (mention handle);
`#` → `ResolveChannelByTitle`, not-found → `NOT_FOUND_FAILED`; `conversations/<uuid>` → passthrough
(manager re-validates read permission); bare ids and message paths → `INVALID_ARGUMENT_FAILED`.

The `Deps` struct (`chattools.go:31`) carries `Client`; resolution costs at most one round-trip per
command (address→id resolution is cached within a single CLI invocation by the daemon handler
scope, not across turns). The design's `normalizeConversationName` / `normalizeThreadRoot`
helpers were **removed**; every input site now goes through the resolver (18
`resolveConversationAddress` call sites plus ~10 `resolveThreadRoot`/`resolveMessageName` call
sites across `chattools.go`, `chattools_channel.go`, `chattools_task.go`, `chattools_reminder.go`,
`chattools_reaction.go`).

### Rewire input sites — done

All conversation-taking inputs resolve addresses: `GetConversationMessages`, `PostMessage`,
`AckProcessedVersion`, `UploadFile`, `ListFiles`, `GetThreadMessages`, `PostThreadMessage`,
`ListMembers`, `ListTasks`, `CreateTask`, `ListReminders`, `SearchChatHistory`. Thread
`--root`/`Message`/`Name` fields go through `resolveThreadRoot` / `resolveMessageName`. Reminder
`update`/`cancel`/`complete`/`fail` names stay `reminders/{message_id}` (id-based by design — a
reminder is keyed by its root message id, not a conversation name).

### Rewire output (emit) sites — done

Emit sites print the manager-populated `Conversation.address` form instead of raw ids:

| Emit site | Now emits |
|---|---|
| `formatMessageLine` `message:` line (`chattools.go`) | copyable handle `<address>:<message-id>` via `messageHandle` (channel handles single-quoted) |
| `message check` / cursor header (`chattools.go:437`, `chattools.go:468`) | `'<address>'` + version counts |
| turn-batch channel header (`turn_batch.go:75-76`) | `'<address>' (your processed_version=N)` |
| turn-batch message lines (`turn_batch.go` `formatBatchLine`) | `[target='<address>' msg=<id> time=… type=…]` |
| thread-update lines (`chattools.go:599`) | `'<address>'` + root/new-reply counts |
| task lines / echoes (`chattools_task.go:84,161,286`) | `<address>` / `<address>:<message-id>` via `messageHandle` |
| members / thread-participants headers (`chattools_channel.go:154,161`) | `'<address>'` |
| file list header (`chattools.go:537`) | `'<address>'` (file ids stay as-is) |
| reminder lines | unchanged (id-based `reminders/{message_id}` by design) |

The design's `resolveChannelTarget` helper (`turn_batch.go`) was superseded by `conversationAddress`
in `address.go`, which reads the manager-built `Address` field via `GetChannel` — the single
source of truth for the grammar, including the `dm:@<peer>` type-3 case.

## CLI changes — `backend/agent/cmd/` — implemented

- Positional `<conversation>` args became `<address>` args (semantically: `message send <address>`
  accepts `#title`, `dm:@peer`, or a readable `conversations/<id>`). Flag names unchanged.
- `message send`/`thread send`: the dest is a conversation address; `--root` takes a bare root
  message id or a message address (`<address>:<uuid>`) — the design's "legacy message path on
  `--root`" form is rejected, matching the stricter resolver.
- `agent list` exists (`backend/agent/cmd/agent.go` → daemon route `/agent/list` in
  `backend/agent/daemon/server.go` → `chattools.ListPeerAgents`); it renders the global roster
  (display name, `agents/<id>` handle, connection state, public description) and takes no argument.
- The discovery surface grew beyond the design: `channel list` (`ListAccessibleChannels` — the
  agent's memberships plus owner-follow-visible conversations, marked `[joined|visible]`) and
  `message check` (the drain-loop inbox) complement `agent list`.
- The daemon `Request` fields keep carrying address strings; the resolver runs inside chattools
  after the daemon handler, exactly as designed.

## Prompt rewrite — `backend/agent/executor/prompt/communication.md` — implemented

`communication.md` documents the address grammar (`#<title>` / `dm:@<peer>` /
`<address>:<message-id>`), the **single-quote rule for `#` addresses** (a bare `#general` is eaten
as a shell comment; emitted output therefore wraps channel handles in single quotes to copy
verbatim), the rejected id forms, the id-based carve-outs (files use bare ids, reminders
`reminders/{message_id}`, thread roots bare message ids), and a **"Delegating to a peer agent"**
section: discover via `agent list` (never delegate to a `(stopped)` peer; prefer the unique
`agents/<id>` handle when display names are ambiguous), address `dm:@<peer>`, post with
`--base-version 0` for a brand-new DM, then **end the turn** — the reply wakes you on a later
turn; do not poll or block; reuse the same DM for the whole delegation thread.

## Frontend — implemented

- **`frontend/src/pages/dashboard/chat-conversation.tsx`**: `CONVERSATION_TYPE_AGENT_DM = 3`; the
  composer is hidden for type-3 conversations and a view-only banner renders the
  `chat.agent-dm-view-only` locale key (present in both `frontend/src/locales/en-US.json` and
  `zh-CN.json`).
- **`frontend/src/pages/dashboard/agent-chat.tsx`**: type-3 rows render with a distinct icon and the
  peer agent's name (`agent.chat-agent-dm-row` locale key); clicking navigates to `/chat/<convId>`
  as today, which the composer guard makes view-only.
- Left rail: no change — user channel lists exclude type 3 by membership.
- proto-es types regenerated from `buf generate`; the UI uses `Conversation.address` and
  `conversationType === 3`.

Admin view access is granted by the `laelia.conversations.reviewAgentDM` IAM override (decision 3);
non-admin users are denied. No new handler-level authz code.

## Phasing (recommended delivery order)

> **Implementation status: both phases are delivered.** Phase 1 (schema, store, four RPCs,
> `Conversation.address`, resolver, input/emit rewiring, prompt grammar) and Phase 2 (`agent list`
> CLI, delegation prompt section, frontend view-only agent-DM rendering) are all in the current
> code. Since delivery, the messaging surface has also grown **beyond this design**: conversation
> type 4 (user↔user DM), `ListAccessibleChannels` + `channel list` + `message check` discovery, and
> owner-follow read scope (`follow_owner_permissions`). Those live outside this doc's scope.

## Critical files (as built)

- `proto/v1/v1/command.proto` — 4 new RPCs, `Conversation.address` (field 11), `PeerAgent`.
- `backend/manager/migration/migration/LATEST.sql` — type 3, agent-DM columns/CHECK/unique index, unique channel-title index.
- `backend/manager/store/conversation.go` — `GetOrCreateAgentDM`, `FindChannelByTitle`, type constants (+ type 4 USER_DM).
- `backend/manager/api/v1/address_resolver.go` — `ResolveChannelByTitle` / `GetOrCreateAgentDM` / `GetOrCreateUserDM` / `ListPeerAgents` handlers (with the IAM read gate).
- `backend/manager/api/v1/channel_convert.go` — `convertToV1Conversation` (populates `address`).
- `backend/manager/api/v1/channel_message.go` — `SendMessage` type-3 guard; `channel_file_service.go` — upload guard.
- `backend/manager/component/iam/manager.go` — `reviewAgentDM` non-member read override.
- `backend/agent/chattools/address.go` — resolver + emit-side helpers; input/emit sites rewired across `chattools.go`, `chattools_channel.go`, `chattools_task.go`, `chattools_reminder.go`, `chattools_reaction.go`, `turn_batch.go`.
- `backend/agent/daemon/server.go` — `/agent/list` route; address fields flow through existing handlers.
- `backend/agent/cmd/agent.go` + `message.go`/`thread.go`/`task.go`/`reminder.go` — `<address>` args.
- `backend/agent/executor/prompt/communication.md` — grammar, quoting rule, delegation docs.
- `frontend/src/pages/dashboard/chat-conversation.tsx` + `agent-chat.tsx` — view-only type 3.

## Verification

Status of the original checklist against the current code:

1. **Schema** — implemented: `LATEST.sql` adds `agent_dm_a`/`agent_dm_b`, the order CHECK, and both
   unique indexes, all idempotent (`IF NOT EXISTS` / conditional constraint add).
2. **Unit** — implemented: `TestAgentDMUniqueIndexPresent` and `TestChannelTitleUniqueIndexPresent`
   exist in `backend/manager/migration/migration_test.go` (alongside `TestUniqueConstraintsPresent`);
   resolver tests live in `backend/agent/chattools/address_test.go` (channel found/not-found, DM
   create/reuse, bare-root round-trip, the `:<uuid>` suffix split). The "legacy
   `conversations/<c>/messages/<m>` passthrough" case no longer applies — message paths are rejected
   by design.
3. **Channel-title uniqueness** — implemented (unique index; conflicts surface as already-exists
   errors on create/update).
4. **Delegation e2e** — implemented end-to-end: `agent list` → `message send dm:@rei` creates the
   type-3 row (`created_by=owner_id=1`, ordered `agent_dm_a/b`, two AGENT members, seeded cursors),
   the reply re-wakes the sender via the room hub's `notifyConversationAgents`, the DM is absent
   from user channel lists, and the admin detail-page chat tab is view-only. One wording change:
   `agent list` shows the peer's public description, not its private persona.
5. **Race** — covered by the unique index + `ON CONFLICT DO NOTHING` + re-read (`insertAgentDMSQL`).
6. **Self-address** — rejected (`CodeInvalidArgument` in the handler, and the store refuses
   identical ids).
7. **Agent proactive user DM** — implemented via `GetOrCreateUserDM` (peer by mention handle).
8. **First-message attachment** — the resolver creates the DM before upload/attach; note the upload
   guard in `channel_file_service.go` allows agents (and rejects only human senders) in type 3.
9. **Frontend** — `chat.agent-dm-view-only` present in both locales; agent-DM view-only verified in
   the agent detail chat tab.
10. **Backend lint/build gates** — unchanged (gofmt, golangci-lint, the documented `go build` line).
11. **ACP executor test** — still not required by this refactor: it touches CLI/daemon/chattools
    only; the turn-batch change remains display-only (cursor label format), no stdio change.

## Open notes (non-blocking) — resolved

- **Peer-name → user principal lookup**: resolved differently — users are addressed by **mention
  handle** via `store.GetUserByHandle`; no `FindUserByName` was added, and handle uniqueness removes
  the user/agent ambiguity the design worried about (agent peers can always be disambiguated with
  `agents/<id>`).
- **`:` in channel titles**: implemented as designed — the suffix is split only when it parses as a
  UUID, so `:` inside a title is tolerated; `communication.md` tells agents to copy addresses from
  output rather than hand-construct them.
- **Reminder names** stay id-based (`reminders/{message_id}`) as designed; the resolver does not
  touch them.