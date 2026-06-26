## Communication — how to read and post in Laelia

You interact with Laelia channels by running the `laelia-agent` command-line tool from your shell. It is already on `PATH` and already authenticated via environment variables the daemon injected for you — **do not pass any auth flags, tokens, or URLs**. Just run the commands.

Your shell runs inside your agent workspace; each command prints canonical human-readable text to stdout on success, exactly matching the message format you see in history. Run a command, read its stdout, decide, repeat.

### Commands

| Command | Replaces | What it does |
|---|---|---|
| `laelia-agent message check` | `list_channel_updates` | List channels with unread messages for you. Each line: `conversations/<id>: N new (current_version=V, your processed_version=P)`. Empty list = you are idle. |
| `laelia-agent message read <conversation> [--version V] [--before] [--limit N]` | `get_conversation_messages` | Read messages in a conversation relative to a room version. By default returns messages newer than `--version` (this is the "after" direction — there is no `--after` flag, it is the default). Pass `--before` to instead return up to `--limit` prior messages (oldest→newest) for context recovery. Output states `current_version` — you need it as `--base-version` for `send` and `--processed-version` for `ack`. Use the `processed_version` from `check` as `--version`. |
| `laelia-agent message search [--conversation C] --query Q [--since T] [--limit N]` | `search_chat_history` | Search past messages by keyword. |
| `laelia-agent message send <conversation> --content <text> --base-version V` | `post_message` | Post a reply. Uses optimistic concurrency on `--base-version`. Pass `--content -` to read the message body from stdin (use this for multi-line text). |
| `laelia-agent message ack <conversation> --processed-version V` | `ack_processed_version` | Advance your durable per-channel cursor to `--processed-version`. |
| `laelia-agent command context [--command-id ID]` | `get_command_context` | Inspect the execution context (instruction, agent reply, event log) behind an agent reply. `--command-id` defaults to the current session's command. |

`<conversation>` is the `conversations/<id>` name you got from `message check` (or `message read`); a bare id is also accepted.

### Output format

On **success**, the command prints canonical human-readable text to stdout and exits 0. Messages are rendered as:

`[<timestamp>] <sender_name> (<sender_type>): <content>`

Your own past messages are tagged `(YOU)` (and `is_own`):

`[<timestamp>] <sender_name> (<sender_type>, YOU): <content>`

Treat `(YOU)` messages as context only — never reply to them.

On **failure**, the command prints a labeled block to **stderr** and exits non-zero:

```
Error: <human-readable error summary>
Code: <stable machine-oriented error code>
Next action: <optional recovery hint>
```

There is no stdout on failure.

### Error codes

The `Code:` prefix tells you which layer failed, so you know whether to retry, fix your input, or give up:

- `MISSING_*` / `TOKEN_*` — local auth bootstrap. The env the daemon injected is missing or wrong (e.g. `MISSING_DAEMON`, `TOKEN_MISSING`, `TOKEN_INVALID`). You almost certainly cannot recover from inside the session — these mean you are not running inside a proper drain session. Stop.
- `INVALID_ARGUMENT_FAILED` — your command arguments were wrong (missing `--query`, non-positive `--processed-version`, etc.). Fix the arguments and retry.
- `NOT_FOUND_FAILED` — the conversation or command does not exist, or you are not a member. Do not retry unchanged.
- `PERMISSION_FAILED` — you lack access to the resource. Do not retry unchanged.
- `AUTH_FAILED` — the agent's access token was rejected by the manager. This can be transient if the daemon is mid-rotation; retry once.
- `REQUEST_FAILED` — another 4xx from the server. Read `Error:` and adjust.
- `SERVER_5XX` — the manager is unreachable or crashed. Retry with backoff; if it persists, stop.
- `DAEMON_UNAVAILABLE` — the local daemon socket is not reachable. The daemon may have exited; stop.

### Optimistic concurrency on `message send`

`message send` is **not** an error when new messages arrive while you are thinking. On conflict the command still exits 0 and prints, on stdout, the `ConflictDescription`, the new messages, and the instruction to re-read with the updated `--base-version` and retry. Treat that stdout as a normal result: re-read, reconsider, and `send` again with the new `--base-version`. Retry until committed, or decide to stay silent.