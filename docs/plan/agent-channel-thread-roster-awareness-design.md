# Plan: Channel/Thread Roster Awareness + User Self-Description for Agents

## Context

Today an autonomous agent in laelia has no way to see *who else is in a channel or thread*. The drain loop discovers unread messages and threads, but the agent cannot enumerate the members of a conversation, cannot read another agent's `persona_prompt`, and cannot perceive users well enough to proactively `@mention` the right person/agent for a task. Concretely:

- No chattool wraps the existing `ListChannelMembers` RPC; no tool lists thread participants at all.
- `ChannelMember` proto carries only `display_name` — no self-description (users have none at all; agents have `persona_prompt` but it is not surfaced here).
- The agent `PostMessage` path never populates structured `Mentions` (`command.go:833` passes `nil`), so a thread `@agent` typed by an agent does not subscribe/wake anyone. The agent is told to `@mention` in the prompt but the manager does not parse it.
- Users have no bio/description field and no self-service profile page.

Intended outcome: an agent can run `channel members` / `thread participants` to perceive the users and agents in scope (with short descriptions), run `agent detail` for a specific agent's full persona, and simply type `@someone` in its reply — the manager parses the `@` tokens, resolves them to conversation members, and routes thread subscription/wake. Users gain a self-description they can edit from a profile page.

## Decisions (confirmed with user)

1. **Thread participants**: derive from the distinct senders of the thread's messages (root + replies). No new participation table.
2. **Persona exposure**: roster shows name/type/role + a *short* description; a separate `agent detail` tool fetches the full `persona_prompt` on demand.
3. **@-mentions**: the agent only emits content-only `@someone`; the **manager** parses `@` tokens from content, resolves them to members, and populates structured `Mentions` (agent path only). No `--mention` CLI flag.
4. **User self-description**: full stack — proto + store + RPC + frontend profile page + admin edit.

## Part 1 — Proto & generated code

File: `proto/v1/v1/command.proto`

- Extend `ChannelMember` (L374-380) with `string description = 6;` — users: `User.description`; agents: `AgentACPConfig.persona_prompt`. The proto carries the full text; the chattool formatter truncates for display.
- Add `GetConversationAgentProfile` RPC to `CommandService`:
  ```
  rpc GetConversationAgentProfile(GetConversationAgentProfileRequest) returns (AgentProfile) {}
  message GetConversationAgentProfileRequest { string conversation = 1; string agent = 2; }  // agent = "agents/<id>"
  message AgentProfile { string name = 1; string title = 2; string persona_prompt = 3; string status = 4; }
  ```
  Gated by conversation membership so an agent can only fetch co-members' profiles.
- Add `ListThreadParticipants` RPC to `CommandService`:
  ```
  rpc ListThreadParticipants(ListThreadParticipantsRequest) returns (ListThreadParticipantsResponse) {}
  message ListThreadParticipantsRequest { string conversation = 1; string thread_root = 2; }
  message ListThreadParticipantsResponse { repeated ChannelMember members = 1; }
  ```
  Reuses `ChannelMember` (role left 0 for threads).

File: `proto/v1/v1/user_service.proto`

- Add `string description = 16;` to `User` (L205-243) — short self-description ("后端工程师, 专注于 agent 的构建"). `UpdateUser` already supports `update_mask`, so no new RPC is needed; just allow the field through the mask.

Regenerate: `cd proto && buf format -w proto && buf lint proto && buf generate`. This regenerates `backend/generated-go/v1/*` and `frontend/src/types/proto-es/v1/*`.

## Part 2 — Store layer

`backend/manager/migration/latest.sql` — add column to `principal`:
```sql
description TEXT NOT NULL DEFAULT ''
```
(One cumulative schema file; add near the `principal` columns around L18-31.)

`backend/manager/store/principal.go`
- Add `Description string` to `UserMessage` (L51-66) and to the `ListUsers`/`GetUserBy...` scan select-list and the principal row struct.

`backend/manager/api/v1/user_service.go`
- Populate `Description` in `convertToUser`; honor it in `UpdateUser` via `update_mask` (mirror the existing `title`/`phone` mask handling).

`backend/manager/store/conversation_member.go` — no change to `ListConversationMembers` (already returns members); description is resolved per-member in the handler.

`backend/manager/store/chat_message.go` — add a new query:
```go
func ListThreadParticipants(ctx, db, convID, threadRootID uuid.UUID) ([]ThreadParticipant, error)
```
Select distinct senders from `chat_message` where `thread_root_message_id = $1` (and `conversation_id = $2`), returning `(sender_type, principal_id, agent_id)` tuples. Each distinct sender becomes one roster entry.

## Part 3 — Manager handlers

`backend/manager/api/v1/channel.go` (`ListChannelMembers`, L295-318):
- Extend the member build loop to populate `Description` alongside `DisplayName`. Add a sibling `resolveMemberDescription(ctx, store, memberType, memberID) string`:
  - user (type 1): `store.GetPrincipal(memberID)` → `Description`.
  - agent (type 2): `store.GetAgentByResourceID(memberID)` → `info.AcpConfig.PersonaPrompt`.
  - Keep per-member resolution (channels are small); note batch as a future optimization.

`backend/manager/api/v1/command.go`:
- Implement `GetConversationAgentProfile`: parse conversation + agent resource id, `requireConversationMember`, `store.GetAgentByResourceID`, return title + `info.AcpConfig.PersonaPrompt` + status.
- Implement `ListThreadParticipants`: `requireConversationMember`, validate `thread_root` is a root in this conversation (reuse `IsThreadRoot`), call `store.ListThreadParticipants`, resolve each sender to a `ChannelMember` (reuse the `resolveMemberDisplayName`/`resolveMemberDescription` helpers from channel.go — extract them to a shared file `api/v1/member_resolve.go`).

`backend/manager/api/v1/mention.go` (new file) — content-mention parser:
```go
func parseContentMentions(ctx, store, convID uuid.UUID, content string) []*v1pb.Mention
```
- Tokenize `@<bareword>` and `@"quoted multi-word"` from content.
- For each token, case-insensitively match against the conversation members' `display_name` (load members once via `store.ListConversationMembers`, build a name→member map). Exact match only; ambiguous/no-match → skip.
- Build `Mention{Type:"user"|"agent", Id:<resource id>, Name:<display_name>}`. Exclude the posting agent itself (caller passes its agent id) to avoid self-subscribe.
- Return the list; caller merges into the message + `subscribeAndNotifyThread`.

`backend/manager/api/v1/command.go` (`PostMessage`, around L829):
- After building the message content, call `parseContentMentions(ctx, s.store, convUUID, req.Msg.Content)` (passing `&agent.ID` to exclude self).
- Pass the resolved mentions into `store.CreateChatMessageBumpVersion` (set the `Mentions` field, already supported — `chat_message.go:43`) and into `subscribeAndNotifyThread(ctx, convUUID, threadRoot.UUID, newVersion, mentions, &agent.ID)`. This makes a thread `@agent` typed by an agent actually subscribe/wake that agent, matching the user `SendMessage` path (`channel.go:459-514`).
- User `SendMessage` path is left unchanged (frontend already provides structured mentions); optional future unification noted.

## Part 4 — chattools (new tools + tidying)

`backend/agent/chattools/chattools_channel.go` (new file, mirrors `chattools_task.go`):

- `ListChannelMembers(ctx, d, ListChannelMembersInput{Conversation}) (string, error)` — calls `d.Client.ListChannelMembers`, formats with `formatMemberLine` (truncated description).
- `ListThreadParticipants(ctx, d, ListThreadParticipantsInput{Conversation, ThreadRoot}) (string, error)` — calls `d.Client.ListThreadParticipants`.
- `GetAgentProfile(ctx, d, GetAgentProfileInput{Conversation, Agent}) (string, error)` — calls `d.Client.GetConversationAgentProfile`, prints full title + persona_prompt + status.
- Local helpers (kept in this file, following the per-domain mapper convention noted in exploration):
  - `memberTypeString(int32) string` (1→`user`, 2→`agent`), `memberRoleString(int32) string` (1→`owner`, 2→`member`).
  - `formatMemberLine(*v1pb.ChannelMember) string` → `[user] Alice (owner) — 后端工程师, 专注 agent 构建`.
  - `truncateDescription(s string, n int) string` (n≈140, `…` suffix).
- Reuse `normalizeConversationName` and `normalizeThreadRoot` from `chattools.go`. Reuse `wrapManagerError` for error mapping.

Tidying (opportunistic, in `chattools_reminder.go`):
- Collapse `parseFireAt` + `mustParseRFC3339` (L300-319) into a single function returning `(time.Time, error)` to remove the "Unreachable" silent-fallback landmine identified in exploration. No behavior change for valid input.

## Part 5 — Wiring (daemon + CLI + prompt)

`backend/agent/daemon/server.go`:
- Add three handlers (`handleChannelMembers`, `handleThreadParticipants`, `handleAgentProfile`) following `handleFileList` (L680-687). Build `Input` from the shared `Request` (add `Agent`/`Thread` fields to `Request` if not present — check existing fields first). Wrap with `s.run(...)`.
- Register routes in `Server.Start()` (near L151-175): `/channel/members`, `/thread/participants`, `/agent/profile`.

`backend/agent/cmd/`:
- New `channel.go` with `laelia-agent channel members --conversation <c>` (model on `file.go`).
- Extend thread command (or new `thread.go`) with `laelia-agent thread participants --conversation <c> --root <r>`.
- New `agent.go` with `laelia-agent agent detail --conversation <c> --agent <a>` (or accept a display-name and resolve via members list — simpler to require the agent resource id as returned by `channel members`).
- All read `LAELIA_*` env via existing `loadIdentity` and call `cmd.call("/<path>", Request{...})`.

`backend/agent/executor/prompt/communication.md`:
- Document the three new commands (usage, output format, examples) in the same style as the existing CLI reference.

`backend/agent/executor/prompt.go` (`AgentFirstPromptBody`, L51-89):
- Add a line in the decision step: before `@mention`ing someone for a task, run `channel members` (or `thread participants`) to see who is present and their descriptions; use `agent detail` for a specific agent's full persona. Keep the existing 9-step structure; this is an inline hint, not a new mandatory step.

## Part 6 — Frontend

Proto-es regen (from Part 1) gives `User.description` types.

`frontend/src/stores/types.ts` (L98-102) — extend `updateUser` fields type: add `description?: string`.

`frontend/src/stores/user.ts` (L57-80) — pass `description` through in `createUser`/`updateUser`.

New page `frontend/src/pages/dashboard/settings-profile.tsx` — `SettingsProfilePage`, Style A (copy `settings-storage.tsx` layout): a form seeded from `useAppStore(s => s.currentUser)`, with a `Textarea` (from `@/components/ui/textarea`, model on `agent-profile.tsx:544-554`) for `description` plus the existing editable fields (title/email/phone). Save via `updateUser(currentUser.name, {...}, maskPaths)` with a diff-driven mask (mirror `user-list.tsx:163-201`); toast via `toastManager`.

Routing:
- `frontend/src/router/handles.ts` — add `SETTINGS_ROUTE_PROFILE`.
- `frontend/src/router/routes/dashboard.tsx` (L132-153) — add `path: "profile"` child under `/settings`. Update the `/settings` `<Navigate>` default if desired (keep `storage` default).
- `frontend/src/components/user-menu.tsx` — add a "Profile" link to the new route.

Admin edit (`frontend/src/pages/dashboard/user-list.tsx`):
- Add `editDescription` state, a description `FieldRow` + `Textarea` in the edit Sheet (mirror the title block L522-529), seed in `openEdit` (L154), push `"description"` into `maskPaths` in `handleSaveEdit` (L163-201) when changed.

i18n — add `settings.profile.*` / `user.field-description` keys to both `frontend/src/locales/en-US.json` and `zh-CN.json` (user-field keys ~L93-99).

Optional human-facing enhancement: render `description` as `sublabel` in `frontend/src/components/chat/member-picker.tsx` (L42-52) and in `mention-detail-sheet.tsx` so users see each other's bios. Small, low-risk.

## Verification

Backend:
- `gofmt -w` modified files; `golangci-lint run --allow-parallel-runners` until clean.
- Unit tests: extend `backend/agent/chattools/chattools_test.go` with cases for `memberTypeString`/`memberRoleString`/`formatMemberLine`/`truncateDescription`. Add a manager test for `parseContentMentions` (matches `@"Alice"`, bare `@Bob`, skips unknown, excludes self, handles multi-word `@"UI UX"`).
- `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`.
- If ACP stdio integration touched: `LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1` (expect known pre-existing failures per memory; skip `TestACPSessionUpdate`).

End-to-end (manual): start manager `--port 8181 --debug`; in a channel with ≥1 user (description set) and ≥2 agents (persona_prompt set), trigger an agent drain and have the agent run `laelia-agent channel members` — verify roster shows both types with descriptions. Have the agent `thread send --root <r> @<other-agent>`; verify the `@`-mentioned agent is subscribed (thread_participant row) and woken on next drain, and that the posted message's `mentions` field is populated. Frontend: open `/settings/profile`, set a description, reload, confirm it persists and appears in the admin edit Sheet and member picker.

Frontend:
- `pnpm --dir frontend biome:check`; `pnpm --dir frontend lint --fix`; `pnpm --dir frontend type-check`; `pnpm --dir frontend test`.