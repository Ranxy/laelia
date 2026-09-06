# Plan: Channel/Thread Roster Awareness + User Self-Description for Agents

> Status: verified and updated against the current code on 2026-09-06. Main changes: the roster now surfaces a dedicated PUBLIC description (`Agent.description` field 24 / `User.description` field 16 — persona_prompt is intentionally never exposed), `ChannelMember` also carries `handle`/`avatar`/`preferred_language`, and content-mention parsing is handle-based (`@<handle>`) with a 3-pass resolver applied to both the agent `PostMessage` and user `SendMessage` paths.

## Revision — single `members` tool (as implemented)

After implementing the plan, the roster tool surface was collapsed from three
tools to one. The original plan exposed `channel members` + `thread
participants` + `agent detail` as separate tools, which forced an agent to call
twice (roster, then `agent detail` per target) just to learn whom to @mention —
extra reasoning steps that easily break. Since descriptions are short, they are
carried **inline, untruncated** in the roster itself. The implemented design:

- One tool: `laelia-machine members <conversation> [--root <root-msg-id>]`. No
  `--root` → channel members; with `--root` → thread participants. Server-side
  `ListChannelMembers` and `ListThreadParticipants` RPCs remain (different data
  sources: membership table vs message senders); the single CLI command routes
  to the right one internally (`daemon/server.go` exposes a single `/members`
  route; `cmd/members.go` is the CLI).
- `GetConversationAgentProfile` / `AgentProfile` were **removed entirely**
  (proto, manager handler, chattool, daemon route, CLI, prompt). Instead of
  exposing the private `persona_prompt`, agents received a dedicated **public**
  description column (`agent.description`, migration
  `migration/1.1/0023##agent-description.sql`, proto `Agent.description = 24`)
  that the roster carries inline; the proto comment on `ChannelMember.description`
  states persona_prompt is intentionally NOT exposed. Agent discovery beyond one
  conversation is covered by the separate `laelia-machine agent list` tool
  (`ListPeerAgents`, which also prints each peer's public description) and
  `laelia-machine team get/show`.
- `ChannelMember` now also carries `handle = 9` (the `@<handle>` mention token
  for every member, user or agent), `avatar = 7`, and
  `preferred_language = 8` alongside `description = 6`.
- The chattool renders the description as an indented block under each member
  line, untruncated (`chattools_channel.go` `formatMemberLine` — no
  `truncateDescription` helper exists anymore).

Parts 1/3/4/5 below describe the original three-tool design and are retained as
the design rationale; the bullets above (and the per-part notes) are the
as-built surface.

## Context

(A design-time snapshot; several gaps below have since been closed — see the
per-part notes.)

At design time an autonomous agent in laelia had no way to see *who else is in a channel or thread*. The drain loop discovers unread messages and threads, but the agent could not enumerate the members of a conversation, could not read another agent's description, and could not perceive users well enough to proactively `@mention` the right person/agent for a task. Concretely:

- No chattool wrapped the existing `ListChannelMembers` RPC; no tool listed thread participants at all. (Closed: the `members` tool.)
- `ChannelMember` carried only `display_name` — no self-description. (Closed: `description` + `avatar` + `preferred_language`.)
- The agent `PostMessage` path did not populate structured `Mentions`, so a thread `@agent` typed by an agent did not subscribe/wake anyone. (Closed: the manager parses `@<handle>` tokens in BOTH the agent `PostMessage` path (`api/v1/command_message.go`) and the user `SendMessage` path (`api/v1/channel_message.go`).)
- Users had no bio/description field and no self-service profile page. (Closed: `principal.description` + `/settings/profile`.)

Intended outcome (now real): an agent can run `laelia-machine members` to perceive the users and agents in scope (with descriptions, handles, roles, preferred languages), run `agent list` for peers beyond one conversation, and simply type `@<handle>` in its reply — the manager parses the `@` tokens, resolves them to members, and routes thread subscription/wake. Users gain a self-description they can edit from a profile page.

## Decisions (confirmed with user)

1. **Thread participants**: derive from the distinct senders of the thread's messages (root + replies). No new participation table. (Implemented as `store.ListThreadSenders`.)
2. **Persona exposure**: roster shows name/type/role + description inline, untruncated; a separate `agent detail` tool was dropped in favor of the public `Agent.description` column plus the `agent list` peer roster.
3. **@-mentions**: the agent only emits content-only `@<handle>`; the **manager** parses `@handle` tokens from content, resolves them to members (handle first, then unambiguous display name, then the global directory), and populates structured `Mentions` — for both the agent `PostMessage` and user `SendMessage` paths. No `--mention` CLI flag; no quoted multi-word token form (the handle is the primary and unambiguous form).
4. **User self-description**: full stack — proto + store + RPC + frontend profile page + admin edit. (Implemented.)

## Part 1 — Proto & generated code (as built)

File: `proto/v1/v1/command.proto`

- `ChannelMember` gained `description = 6`, `avatar = 7`, `preferred_language = 8`, and `handle = 9` (plus the pre-existing `member_type/member_id/display_name/member_role/joined_at`). Users: `User.description`; agents: `Agent.description` (public intro — the proto comment explicitly says the agent's private `persona_prompt` is NOT exposed here).
- `GetConversationAgentProfile` / `AgentProfile`: **not implemented — removed from the plan** (see the Revision section).
- `ListThreadParticipants` RPC exists on `CommandService`:
  ```
  rpc ListThreadParticipants(ListThreadParticipantsRequest) returns (ListThreadParticipantsResponse) {}
  message ListThreadParticipantsRequest { string conversation = 1; string thread_root = 2; }
  message ListThreadParticipantsResponse { repeated ChannelMember members = 1; }
  ```
  Reuses `ChannelMember` (member_role left 0 for threads).

File: `proto/v1/v1/user_service.proto`

- `string description = 16;` on `User` — self-description ("后端工程师, 专注于 agent 的构建"). `UpdateUser` supports it through `update_mask` path `"description"` (no new RPC).

Regenerated: `backend/generated-go/v1/*` and `frontend/src/types/proto-es/v1/*`.

## Part 2 — Store layer (as built)

`backend/manager/migration/migration/LATEST.sql` — `description TEXT NOT NULL DEFAULT ''` lives inline in the base `CREATE TABLE principal` block (with a comment noting it is surfaced in channel/thread rosters). (`agent.description` came later via `migration/migration/1.1/0023##agent-description.sql`.)

`backend/manager/store/principal.go`
- `UserMessage` carries `Description string`; the `ListUsers`/`GetUserBy...` scans and the principal row struct include it.

`backend/manager/api/v1/user_service.go`
- `Description` populated in the user conversion; honored in `CreateUser` and in `UpdateUser` via the `"description"` update-mask path.

`backend/manager/store/conversation_member.go` — `ListConversationMembers` unchanged; the description is resolved per-member in the handler.

`backend/manager/store/thread_participant.go` — the distinct-sender query is implemented as
```go
func (s *Store) ListThreadSenders(ctx context.Context, conversationID, rootID uuid.UUID) ([]ThreadSender, error)
```
(select distinct senders from `chat_message` where `id = root OR thread_root_message_id = root` in the conversation, excluding SYSTEM senders, returning `(sender_type, principal_id, handle, agent_id)` tuples ordered by first appearance; each distinct sender becomes one roster entry — the planned name `ListThreadParticipants` in `chat_message.go` was not used).

## Part 3 — Manager handlers (as built)

`backend/manager/api/v1/channel_members.go` (`ListChannelMembers`, `ListThreadParticipants`):
- Member rows are built by the shared helper `buildChannelMember(ctx, store, memberType, memberID, role, joinedAt)` in `api/v1/channel_convert.go`, which resolves display name, public `Description`, `Avatar`, and `PreferredLanguage` via `resolveMemberProfile`:
  - user (type 1): the principal's `Description`.
  - agent (type 2): `store.GetAgentByResourceID(memberID)` → `agent.Description` (public intro).
  - Per-member resolution (channels are small); batch resolution remains a future optimization.
- `ListThreadParticipants` validates `thread_root` is a root in this conversation (`IsThreadRoot`), calls `store.ListThreadSenders`, and resolves each sender to a `ChannelMember` via the same `buildChannelMember` helper (role 0, no joinedAt).

`backend/manager/api/v1/mention.go` — content-mention parser (as built; evolved from the original sketch):
```go
func (s *CommandService) parseContentMentions(ctx, convID uuid.UUID, content string) []*v1pb.Mention
```
- Tokenizes bare `@<handle>` runs (letters/digits/`_`/`-`/`.`) — e.g. `@ran-user-1`, `@rei-agent-1`. No quoted multi-word form.
- Three-pass resolution: (1) exact case-insensitive match on the member's mention handle; (2) fallback to an unambiguous display-name match within the conversation; (3) global directory fallback (handle, then unambiguous display name) so an agent can mention a peer that is not a member of the current conversation — the global index is cached in the store and rebuilt on agent/user create/update/delete.
- Ambiguous display names never resolve; unknown tokens are skipped. `Mention.Name` is the display text (or the handle when two members in one message share a display name); `Mention.Id` always carries the canonical handle.
- The posting agent/sending user is NOT excluded here — routing already skips the poster, and keeping self-mentions lets the frontend render `@self` badges.
- Callers merge the parsed mentions into the message and `subscribeAndNotifyThread`.

Integration (as built — both paths, not just the agent one):
- Agent `PostMessage`: `api/v1/command_message.go` calls `parseContentMentions` and merges into `store.CreateChatMessageBumpVersion` + `subscribeAndNotifyThread`, so a thread `@agent` typed by an agent actually subscribes/wakes that agent.
- User `SendMessage`: `api/v1/channel_message.go` also calls `parseContentMentions` and merges with the frontend-provided structured mentions (the original plan left the user path unchanged; this unification happened later).

## Part 4 — chattools (as built)

`backend/agent/chattools/chattools_channel.go`:

- `ListMembers(ctx, d, ListMembersInput{Conversation, Root}) (string, error)` — the single roster tool. Without `Root`, calls `d.Client.ListChannelMembers`; with `Root` (a bare thread-root id), calls `d.Client.ListThreadParticipants`. Formatted by `formatRoster` (header with count, one `formatMemberLine` per member, addressing footer).
- `formatMemberLine(*v1pb.ChannelMember)` renders `- [user|agent] <display_name> @<handle> (owner|member) (language: xx-XX)` followed by the member's full public description as an indented block. The `@<handle>` token is shown for ALL members and is the exact text to copy into a reply. The description is emitted **untruncated** (no `truncateDescription` helper).
- Local helpers kept in this file: `memberTypeString(int32)` (1→`user`, 2→`agent`), `memberRoleString(int32)` (1→`owner`, 2→`member`, 3→`admin`), `preferredLanguageString(v1pb.PreferredLanguage)`.
- Reuses the shared conversation-address resolution and `wrapManagerError` error mapping.

Tidying (done): the reminder chattool's date parsing collapsed into a single `parseFireAtTime(s string) (time.Time, error)` in `chattools_reminder.go` — the "Unreachable" silent-fallback pair (`parseFireAt` + `mustParseRFC3339`) from the exploration no longer exists.

## Part 5 — Wiring (daemon + CLI + prompt, as built)

`backend/agent/daemon/server.go`:
- A single `/members` route (`s.handleMembers`) registered alongside the other command routes; the shared `Request` envelope carries `Conversation`/`Root`. The planned `/channel/members`, `/thread/participants`, `/agent/profile` routes were collapsed into this one (and `/agent/profile` never existed — replaced by `/agent/list`).

`backend/agent/cmd/`:
- `members.go` — `laelia-machine members <address> [--root <root>]` ("List the users and agents in a channel (or thread with --root) with their full descriptions").
- No `channel members` subcommand (channel.go owns list/join/leave/add-member/remove-member); no `thread participants` subcommand (`members --root` covers it); no `agent detail` — instead `agent.go`'s `laelia-machine agent list` (`ListPeerAgents`) and `team.go`'s `team get` / `team show`.
- All read `LAELIA_*` env via the shared identity loader and call the daemon over the same HTTP mux.

`backend/agent/executor/prompt/communication.md`:
- Documents `laelia-machine members <address> [--root <root-msg-id>]` in the command table (roster semantics, the `(language: xx-XX)` tag, the indented public-description block) plus the `@<handle>` mention guidance and the `agent list` peer roster.

`backend/agent/executor/prompt.go` (`AgentFirstPromptBody`):
- The decision-step hint is present: before `@mention`ing someone for a task, run `laelia-machine members <address>` (or `members <address> --root <thread_root>`) to see who is present, their preferred language, and their public descriptions; `agent list` for peers beyond the channel. The prompt body has since been restructured into the batch-driven 0–8 step flow (step 0 due reminders, step 1 batch); the roster hint lives in the decision step.

## Part 6 — Frontend (as built)

Proto-es regen (Part 1) provides `User.description` types.

`frontend/src/stores/types.ts` / `frontend/src/stores/user.ts` — `createUser`/`updateUser` pass `description` through.

`frontend/src/pages/dashboard/settings-profile.tsx` — `SettingsProfilePage`: a form seeded from `useAppStore(s => s.currentUser)`, including the `description` `Textarea` plus the existing editable fields (title/email/phone); saves via `updateUser` with a diff-driven `description` mask path; toast feedback.

Routing:
- `frontend/src/router/handles.ts` — `SETTINGS_ROUTE_PROFILE` ("settings.profile").
- `frontend/src/router/routes/dashboard.tsx` — `path: "profile"` child under `/settings` (the `/settings` default still redirects to storage).
- `frontend/src/components/user-menu.tsx` — "Profile" link navigating to `/settings/profile`.

Admin edit (`frontend/src/pages/dashboard/user-list.tsx`):
- `editDescription` state, a description `Textarea` in the edit Sheet, seeded in `openEdit`; `"description"` is pushed into `maskPaths` in the save handler when changed.

i18n — `settings.profile.*` and user-field description keys exist in both `frontend/src/locales/en-US.json` and `zh-CN.json`.

Human-facing enhancement (implemented): `member-picker.tsx` prefers `description` as the `sublabel` for both users and agents; `mention-detail-sheet.tsx` renders the bio as well.

## Verification

Backend:
- `gofmt -w` modified files; `golangci-lint run --allow-parallel-runners` until clean.
- Unit tests: `backend/agent/chattools/chattools_test.go` covers the roster helpers (`memberTypeString`/`memberRoleString`); `backend/manager/api/v1/mention_test.go` covers the mention parser (`TestTokenizeMentions`, `TestBuildDisplayNameIndexWithResolver`, `TestResolveMentionTokenFallback`, `TestBuildGlobalMentionIndex`, `TestBuildMentionsWithDisplayNames` — handle tokens, unambiguous display-name fallback, global-directory fallback, ambiguity skip).
- `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`.
- If ACP stdio integration is touched: `LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1`.

End-to-end (manual): start manager `--port 8181 --debug`; in a channel with ≥1 user (description set) and ≥2 agents (public description set), trigger an agent drain and have the agent run `laelia-machine members '<address>'` — verify the roster shows both types with handles, roles, languages, and descriptions. Have the agent `thread send --root <r> "@<other-agent-handle>"`; verify the `@`-mentioned agent is subscribed (thread_participant row) and woken on next drain, and that the posted message's `mentions` field is populated. Frontend: open `/settings/profile`, set a description, reload, confirm it persists and appears in the admin edit Sheet and the member picker.

Frontend:
- `pnpm --dir frontend biome:check`; `pnpm --dir frontend lint --fix`; `pnpm --dir frontend type-check`; `pnpm --dir frontend test`.