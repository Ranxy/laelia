# Page test coverage

Goal: every page under `src/pages/dashboard` (and `src/pages/auth`) reaches
~70% line coverage, except the one large page documented below. Coverage is
measured with `pnpm --dir frontend test:coverage` (v8 provider, include:
`src/pages/**`, `src/stores/**`, `src/router/auth-redirect.ts`).

## Covered pages

All other pages have dedicated `*.test.tsx` files next to the page source.
The tests exercise the page↔store interaction layer (form payloads, update
masks, toggle persistence, navigation, error/loading states) — the layer
where the recent signup/verification bugs lived.

## Pages intentionally not covered

This page is excluded from the 70% target. It is a large, stateful streaming
page whose behavior is dominated by browser APIs and third-party components
that jsdom cannot meaningfully exercise. Forcing coverage would mean mocking
away the very logic the tests should verify, producing low-value assertions
with high maintenance cost.

| Page | Lines | Why it is not covered |
|---|---|---|
| `chat-conversation.tsx` | 1219 | The main chat page. Real-time streaming (chat stream store), mention detection with caret coordinates (`getCaretCoordinates` measures the DOM via `getComputedStyle`/`getBoundingClientRect`, which return zeros in jsdom), image attachments (`FileReader` + `resizeImageFile` canvas pipeline), markdown rendering, thread/tasks/channel-members/mention panels, `useIsDesktop` responsive branching, and `useSearchParams`-driven conversation switching. A meaningful test would need to stub ~10 child components plus the stream store, leaving only glue code to assert. |
If this page regresses, the failure mode is usually in the shared pieces
(editors, stores, RPC clients) that do have tests. Revisit this decision if
a bug is traced to the page-level composition itself.
