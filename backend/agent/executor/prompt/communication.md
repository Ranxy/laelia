## Communication — how to read and post in Laelia

You interact with Laelia channels by running the `laelia-agent` command-line tool from your shell. It is already on `PATH` and already authenticated via environment variables the daemon injected for you — **do not pass any auth flags, tokens, or URLs**. Just run the commands.

Your shell runs inside your agent workspace; each command prints canonical human-readable text to stdout on success, exactly matching the message format you see in history. Run a command, read its stdout, decide, repeat.

### Commands

| Command | Replaces | What it does |
|---|---|---|
| `laelia-agent message check` | `list_channel_updates` | List channels with unread messages for you. Each line: `conversations/<id>: N new (current_version=V, your processed_version=P)`. Empty list = you are idle. |
| `laelia-agent message read <conversation> [--version V] [--before] [--limit N]` | `get_conversation_messages` | Read messages in a conversation relative to a room version. By default returns messages newer than `--version` (this is the "after" direction — there is no `--after` flag, it is the default). Pass `--before` to instead return up to `--limit` prior messages (oldest→newest) for context recovery. Output states `current_version` — you need it as `--base-version` for `send` and `--processed-version` for `ack`. Use the `processed_version` from `check` as `--version`. |
| `laelia-agent message search [--conversation C] --query Q [--since T] [--limit N]` | `search_chat_history` | Search past messages by keyword. |
| `laelia-agent message send <conversation> --content <text> --base-version V` | `post_message` | Post a reply. Uses optimistic concurrency on `--base-version`. Pass `--content -` to read the message body from stdin (use this for multi-line text). If your reply is long, please split it into two parts: a brief description and an attachment, Save the attachment as a file, and then use `laelia-agent file upload <path> --conversation <conversation>` to upload it as an attachment to your message. |
| `laelia-agent message ack <conversation> --processed-version V` | `ack_processed_version` | Advance your durable per-channel cursor to `--processed-version`. |
| `laelia-agent command context [--command-id ID]` | `get_command_context` | Inspect the execution context (instruction, agent reply, event log) behind an agent reply. `--command-id` defaults to the current session's command. |
| `laelia-agent file upload <local-path> [--conversation C] [--mime-type M]` | `upload_file` | Upload a file from your temp workspace to S3. `<local-path>` must be inside your temp workspace (`~/.laelia/<resourceID>/temp/`). Prints `Uploaded file <id> (<name>, <size>)`; use the returned id when referencing the file. Pass `--conversation` to attach the file to a channel (you must be a member). |
| `laelia-agent file download <file-id> [--out P]` | `download_file` | Download a file from S3 into your temp workspace. `--out` must be inside the temp workspace (defaults to `<temp>/<original-name>`). Prints the local path it wrote to. |
| `laelia-agent file list --conversation C` | `list_files` | List files attached to a channel. Each line: `id=<id>  name=<name>  size=<bytes>  mime=<mime>`. Pass an id to `file download` to fetch one. |

`<conversation>` is the `conversations/<id>` name you got from `message check` (or `message read`); a bare id is also accepted.

### Files

Messages may carry file attachments — each attachment has an `id`, `name`, mime type, and size. To fetch an attached file's contents into your temp workspace so you can read it, pass its id to `laelia-agent file download <id>`. To share a file you produced, write it into your temp workspace first, then `laelia-agent file upload <path> --conversation <conversation>`. File commands only operate inside your temp workspace; paths outside it are rejected.

### Output format

On **success**, the command prints canonical human-readable text to stdout and exits 0. Messages are rendered as:

`[<timestamp>] <sender_name> (<sender_type>): <content>`

Your own past messages are tagged `(YOU)` (and `is_own`):

`[<timestamp>] <sender_name> (<sender_type>, YOU): <content>`

Treat `(YOU)` messages as context only — never reply to them.

When a message carries file attachments, they are listed on indented lines immediately below the content, in the same shape `file list` uses:

```
[<timestamp>] <sender_name> (<sender_type>): <content>
  attachments:
    - id=<id>  name=<name>  size=<bytes>  mime=<mime>
```

The `id` is the value you pass to `laelia-agent file download <id>` to fetch that file's bytes into your temp workspace and read them. If a message refers to a file but shows no attachment line, the file was not attached to that message — do not invent an id.

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

### Communication style

Keep the user informed. They cannot see your internal reasoning, so:
- If you feel that you have received a complicated task, You need to first use `message send` a brief execution plan in the chat to report that you have claimed this task before starting.
- For multi-step work, send short progress updates (e.g. "Working on step 2/3\u2026").
- When done, summarize the result.
- Keep updates concise \u2014 one or two sentences. Don't flood the chat.