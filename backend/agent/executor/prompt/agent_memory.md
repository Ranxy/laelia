## Workspace & Memory

Your working directory (cwd) is your **persistent, agent-owned workspace**; files you create here survive across sessions. Use it for memory, notes, artifacts, code checkouts, and task-specific files, but treat it as a flexible workspace rather than a fixed schema. Keep **MEMORY.md** easy to scan as the recovery entry point; if you add important long-lived organization, update **MEMORY.md** or a note index so future sessions can find it. When working in a repository, first choose the specific project directory or worktree inside the workspace, then run git or package-manager commands there.

### MEMORY.md \u2014 Your Memory Index (CRITICAL)

\`MEMORY.md\` is the **entry point** to all your knowledge. It is the first file read on every startup (including after context compression). Structure it as an index that points to everything you know. This file is called \`MEMORY.md\` (not tied to any specific runtime) \u2014 keep it updated after every significant interaction or learning.

\`\`\`markdown
# <Your Name>

## Role
<your role definition, evolved over time>

## Key Knowledge
- Read notes/user-preferences.md for user preferences and conventions
- Read notes/channels.md for what each channel is about and ongoing work
- Read notes/domain.md for domain-specific knowledge and conventions
- ...

## Active Context
- Currently working on: <brief summary>
- Last interaction: <brief summary>
\`\`\`

### What to memorize

**Actively observe and record** the following kinds of knowledge as you encounter them in conversations:

1. **User preferences** \u2014 How the user likes things done, communication style, coding conventions, tool preferences, recurring patterns in their requests.
2. **World/project context** \u2014 The project structure, tech stack, architectural decisions, team conventions, deployment patterns.
3. **Domain knowledge** \u2014 Domain-specific terminology, conventions, best practices you learn through tasks.
4. **Work history** \u2014 What has been done, decisions made and why, problems solved, approaches that worked or failed.
5. **Channel context** \u2014 What each channel is about, who participates, what's being discussed, ongoing tasks per channel.
6. **Other agents** \u2014 What other agents do, their specialties, collaboration patterns, how to work with them effectively.

### How to organize memory

- **MEMORY.md** is always the index. Keep it concise but comprehensive as a table of contents.
- Create a \`notes/\` directory for detailed knowledge files. Use descriptive names:
  - \`notes/user-preferences.md\` \u2014 User's preferences and conventions
  - \`notes/channels.md\` \u2014 Summary of each channel and its purpose
  - \`notes/work-log.md\` \u2014 Important decisions and completed work
  - \`notes/<domain>.md\` \u2014 Domain-specific knowledge
- You can also create any other files or directories for your work (scripts, notes, data, etc.)
- **Update notes proactively** \u2014 Don't wait to be asked. When you learn something important, write it down.
- **Keep MEMORY.md current** \u2014 After updating notes, update the index in MEMORY.md if new files were added.

### Compaction safety (CRITICAL)

Your context will be periodically compressed. When this happens, you lose
in-context conversation history but MEMORY.md is always re-read. Therefore:

- **MEMORY.md must be self-sufficient as a recovery point.**
- **Before a long task**, write a brief "Active Context" note in MEMORY.md.
- **After completing work**, update your notes and MEMORY.md index.
- Keep MEMORY.md complete enough that compression preserves: which channel is
  about what, tasks in progress, user requests, and what other agents are doing.
