# Page test coverage

Goal: every page under `src/pages/dashboard` (and `src/pages/auth`) reaches
~70% line coverage, except the four large pages documented below. Coverage is
measured with `pnpm --dir frontend test:coverage` (v8 provider, include:
`src/pages/**`, `src/stores/**`, `src/router/auth-redirect.ts`).

## Covered pages

All other pages have dedicated `*.test.tsx` files next to the page source.
The tests exercise the page↔store interaction layer (form payloads, update
masks, toggle persistence, navigation, error/loading states) — the layer
where the recent signup/verification bugs lived.

## Pages intentionally not covered

These four pages are excluded from the 70% target. They are large, stateful
CRUD/streaming pages whose behavior is dominated by browser APIs and
third-party components that jsdom cannot meaningfully exercise. Forcing
coverage would mean mocking away the very logic the tests should verify,
producing low-value assertions with high maintenance cost.

| Page | Lines | Why it is not covered |
|---|---|---|
| `chat-conversation.tsx` | 1219 | The main chat page. Real-time streaming (chat stream store), mention detection with caret coordinates (`getCaretCoordinates` measures the DOM via `getComputedStyle`/`getBoundingClientRect`, which return zeros in jsdom), image attachments (`FileReader` + `resizeImageFile` canvas pipeline), markdown rendering, thread/tasks/channel-members/mention panels, `useIsDesktop` responsive branching, and `useSearchParams`-driven conversation switching. A meaningful test would need to stub ~10 child components plus the stream store, leaving only glue code to assert. |
| `agent-profile.tsx` | 1580 | Full agent CRUD: avatar upload/remove (`useAvatarEditor` + canvas resize), env key-value editor, string-list editor, async model combobox, secret input, connection badge, permission gating (`useHasPermission`), and multiple independent save flows (profile / env / model) plus delete-with-confirm. The interesting logic lives in the editors and the RPC layer, both already covered by their own component/store tests. |
| `machine-profile.tsx` | 1707 | Same shape as agent-profile for machines: env editor, async member picker, connection badges, model combobox, secret input, permission gating, delete, token management. |
| `human-detail.tsx` | 534 | Member detail: avatar editor, groups/roles/permission display, direct-message navigation, delete. Smaller than the others but shares the avatar-editor and permission-gating complexity; its unique logic is a thin composition of already-tested pieces. |

If these pages regress, the failure mode is usually in the shared pieces
(editors, stores, RPC clients) that do have tests. Revisit this decision if
a bug is traced to the page-level composition itself.
