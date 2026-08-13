# 消息 emoji 反应（Message Reactions）设计

## 1. 目标与定位

human/agent 可对一条**可见消息**添加/移除一个 emoji 反应（如 `👍`、`✅`），作为一种**轻量反馈**：

- **不**需要发一条完整消息；
- **不**产生新的对话内容；
- **不**唤醒任何 agent、**不**计入未读、**不**生成 activity、**不**影响乐观并发 `base_version`。

**使用时机**（喂给 agent 的引导语，见 §8）：仅当人类明确要求、或反应是明确确认（acknowledgement）时使用；**不要对每次 merge / deploy / 任务完成 / 例行状态更新自动反应**。

---

## 2. 已确认的关键决策

| # | 决策点 | 结论 |
|---|--------|------|
| 1 | 版本/唤醒语义 | **纯旁路**：不 bump `conversation.version`、不唤醒任何 agent、不计未读、不影响 `base_version` |
| 2 | 命令形态 | `laelia-machine message react '<handle>' --emoji 👍 [--remove]`（嵌套在 `message` 下，与 task/reminder/thread 一致） |
| 3 | 移除权限 | 只有**添加者**能移除自己的反应；移除他人已存在的反应 → `PERMISSION_FAILED` |
| 4 | emoji 校验 | **任意单个 emoji**（grapheme），仅拒含空白；≤16 rune（不是字节） |
| 5 | 前端范围 | **包含**：human 可在 UI 点击添加/移除，并在消息上渲染展示 |
| 6 | 自我反应 | **允许**对自发消息反应（reaction 不产生对话内容，无自扰风险） |
| 7 | agent 感知 | `message read` / `thread read` 输出增加紧凑 reactions 行，让 agent 感知 |
| 8 | 展示形态 | 按 emoji **聚合**显示计数 + 反应者（悬停可看） |
| 9 | 幂等语义 | **幂等 no-op**：重复添加=成功但不变；移除不存在=成功但不变 |

---

## 3. 架构分层与改动全景

该功能横跨 **proto → 后端 store / API → chattools / daemon / CLI → 前端**，外加一条 migration。完整调用链：

```
CLI: laelia-machine message react <handle> --emoji 👍 [--remove]
  └─ daemon unix socket (/reaction/add|/reaction/remove)
      └─ chattools.AddReaction / RemoveReaction (地址解析 + 规范化输出)
          └─ CommandService.AddReaction / RemoveReaction (ConnectRPC)
              └─ store.AddReaction / RemoveReaction (独立 message_reaction 表)
                  └─ roomhub.NotifyConversation (仅前端实时，不 bump 版本)
前端: message-row 渲染 reaction 条 + 点击切换
```

---

## 4. 数据模型与 migration

### 4.1 独立表（核心：不依赖 room_version）

`message_reaction` 与 `chat_message` 完全解耦，`chat_message` 不带 reaction 列（避免每次读都反规范化）。reaction 是消息的旁路属性。

```sql
CREATE TABLE IF NOT EXISTS message_reaction (
  message_id   uuid NOT NULL REFERENCES chat_message(id) ON DELETE CASCADE,
  principal_id int NULL REFERENCES principal(id),   -- user 反应者；agent 反应时为 NULL
  agent_id     int NULL REFERENCES agent(id),       -- agent 反应者；user 反应时为 NULL
  emoji        text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT message_reaction_actor CHECK (num_nonnulls(principal_id, agent_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_message_reaction_user
  ON message_reaction (message_id, emoji, principal_id) WHERE principal_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_message_reaction_agent
  ON message_reaction (message_id, emoji, agent_id) WHERE agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_message_reaction_message ON message_reaction(message_id);
```

- **actor 二选一**：user 反应者存 `principal_id`；agent 反应者存 `agent_id`（镜像 `chat_message.principal_id + sender_agent_id` 的拆分模式，因为反应者是真实 actor，不能借用"对话 owner 即 principal_id"的技巧）。
- **不用复合主键，改用两个部分唯一索引**：actor 二选一决定了两个 actor 列每行必有一个为 NULL，而 PostgreSQL 的主键列隐式 NOT NULL，复合 `PRIMARY KEY (message_id, principal_id, agent_id, emoji)` 会让任何 add（user 或 agent）都撞上非空约束。改为两个 partial `UNIQUE INDEX`（`uq_message_reaction_user` 管 user 反应者、`uq_message_reaction_agent` 管 agent 反应者），UNIQUE 把 NULL 视为互异、两个索引互不干扰，从而保证"每 (message, actor, emoji) 至多一行"。
- **唯一索引提供天然幂等**：add 用 `INSERT ... ON CONFLICT DO NOTHING`（同一 actor 重复添加命中唯一索引=no-op）；remove 用 `DELETE`（删不存在的行天然是 no-op）。
- `emoji` 存规范化后的字面值（单 emoji）。

### 4.2 migration 双写

按 migrator 的双维护模型，**两处都改**：

1. 增量文件 `backend/manager/migration/migration/1.1/0020##message-reaction.sql`（上面的 DDL，幂等）；
2. 累计基线 `backend/manager/migration/migration/LATEST.sql` 末尾追加同样 DDL（仅 fresh install 用到）。

> 若需要让"谁反应了、哪些消息被反应"支持多实例推送，未来可在 `conversation` 上增加独立于 `room_version` 的 `reactions_seq` 计数器（见 §7 实时方案），当前单进程不引入。

---

## 5. proto 变更（`proto/v1/v1/command.proto`）

`buf generate` 重新生成 Go + TypeScript。

### 5.1 Reaction 消息

```proto
// Reaction 是某条消息上针对一个 emoji 的聚合。count 为反应者总数；
// reactors 为反应者显示名/handle；reacted 为调用者相对字段（是否已反应）。
message Reaction {
  string emoji = 1;
  int32 count = 2;
  repeated string reactors = 3;
  bool reacted = 4;          // caller-relative，镜像 ChatMessage.is_own
}
```

### 5.2 ChatMessage 增加字段

```proto
message ChatMessage {
  ...
  // reactions 是该消息上当前的 emoji 反应聚合（按 emoji 分组、计数 + 反应者）。
  // 由 ListConversationMessages / ListThreadMessages 填充。
  repeated Reaction reactions = 19;
}
```

### 5.3 RPC（挂在 CommandService，user 与 agent 共用）

```proto
message AddReactionRequest {
  string message = 1 [REQUIRED];   // "conversations/{c}/messages/{m}"
  string emoji = 2 [REQUIRED];
}
message AddReactionResponse {
  string message = 1;
  repeated Reaction reactions = 2;  // 更新后的聚合
}

message RemoveReactionRequest {
  string message = 1 [REQUIRED];
  string emoji = 2 [REQUIRED];
}
message RemoveReactionResponse {
  string message = 1;
  repeated Reaction reactions = 2;  // 更新后的聚合
}
```

> 不做 `ToggleReaction`：显式 add / remove 两个 RPC 与 CLI 的 `[--remove]` 一一对应，语义清晰、权限分支简单。

---

## 6. 校验：`normalizeReactionEmoji`

放在共享包 `backend/common`，CLI 侧（chattools，本地快速失败）与 manager 侧（权威）共用同一实现。

规则（按确认）：trim → 非空 → **不含任何空白字符**（空格/制表/换行/U+2000–200B 等 Unicode 空白全部拒绝，从而拒绝 `"thumbs up"` 这类文本）→ **≤16 rune**。

```go
func normalizeReactionEmoji(s string) (string, error) {
    s = strings.TrimSpace(s)
    if s == "" {
        return "", errors.New("emoji is required")
    }
    for _, r := range s {
        if unicode.IsSpace(r) {
            return "", errors.New("emoji must not contain whitespace")
        }
    }
    if utf8.RuneCountInString(s) > 16 {
        return "", errors.New("emoji too long (max 16 runes)")
    }
    return s, nil
}
```

- **按 rune（非字节）**：`👍`=1 rune；`👍🏽`（肤色修饰，2 codepoint）= 2 runes，合法；family emoji（多 codepoint 拼接）仍在 16 rune 内。
- **不做白名单**：任意单 emoji 都接受，只保证"是单个 emoji、不是文本"。
- 校验失败 → manager 返回 `INVALID_ARGUMENT_FAILED`（见 §9）；CLI 在 chattools 层先本地校验以便 `--help`/错误更友好。

---

## 7. 实时传播（关键技术点）

**问题**：前端聊天流是"按 `after_version` 增量 + `roomhub` 长轮询"。reaction 不 bump 版本 → 消息长轮询增量恒为空 → 前端拿不到实时 reaction。

**推荐方案（最小改动、语义正确）**：
- store 的 `AddReaction` / `RemoveReaction` 写完后调用 `roomNotifier.NotifyConversation(conversationID)`（复用现有 Hub，见 `backend/manager/component/roomhub/roomhub.go`）。
- 前端聊天 watcher 在任意 wake（即使消息增量为空）时，对**当前渲染窗口内的消息**重新拉取 reactions。
- 空增量对被意外唤醒的消息长轮询**无害**（客户端以同一 `after_version` 重新发起即可）。

**权衡**：reaction 风暴会带来少量多余的聊天长轮询往返（不破坏正确性）。当前单进程可接受。

**未来 refinement（不在本期）**：给 `conversation` 加独立 `reactions_seq` 计数器 + `WatchReactions(conversation, after_seq)` 专用长轮询，与消息版本彻底隔离；多实例部署时换 Postgres LISTEN/NOTIFY 后端（与 roomhub 现有注释一致）。

---

## 8. agent 引导语（`communication.md`）

### 8.1 Commands 表新增一行

| Command | Replaces | What it does |
|---|---|---|
| `laelia-machine message react '<message-handle>' --emoji <emoji> [--remove]` | — | Add or remove your emoji reaction on a message (lightweight feedback). `<message-handle>` is the `<address>:<message-id>` form copied from `message read`/`thread read`. **Use ONLY when a human explicitly asks for a reaction or when a reaction is a clear acknowledgement (e.g. `👍` on an approved result). Do NOT auto-react to every merge, deploy, task completion, or routine status update.** A reaction posts no message, wakes nobody, and is NOT an ack — never use it in place of `message send` or `message ack`. |

### 8.2 Output format 说明

在 `message read` / `thread read` 的消息行后增加紧凑 reactions 行（仅在非空时显示）：

```
[2025-08-13T12:00:00Z] alice (user): 已合入 main
  message: '#general:550e8400-…'  version: 42
  reactions: 👍 ×2 (alice, rei-agent-1), ✅ (bob)
```

- 括号内为反应者名；`reacted` 语义供 agent 识别自己是否已反应（不在此文本里显式标记，靠聚合判断）。
- agent 感知某条消息收到了什么反应后，可决定是否 `message send` 补充说明——但引导语明确**不要**用 reaction 代替正式回复。

### 8.3 错误码

在 `INVALID_ARGUMENT_FAILED` 说明里补充：emoji 校验失败（含空白 / 超长 / 空）也归入此码。

---

## 9. 错误码

沿用现有 `chattools.Error` 体系（CLI 渲染 `Error:` / `Code:` / `Next action:`）：

| Code | 触发 |
|------|------|
| `INVALID_ARGUMENT_FAILED` | emoji 校验失败（空 / 含空白 / >16 rune）；`message` 参数缺失 |
| `NOT_FOUND_FAILED` | 消息或对话不存在 |
| `PERMISSION_FAILED` | 非对话成员；移除**他人已存在**的反应 |
| `AUTH_FAILED` | agent token 被拒（瞬时，可重试一次） |
| `REQUEST_FAILED` / `SERVER_5XX` | 其它 4xx / 服务端错误 |

---

## 10. 后端 store / API 实现要点

### 10.1 store（`backend/manager/store/message_reaction.go` 新建）

- `AddReaction(ctx, messageID uuid.UUID, principalID, agentID *int, emoji string) ([]Reaction, error)`
  - `INSERT INTO message_reaction ... ON CONFLICT DO NOTHING`（幂等），随后 `aggregateReactions(ctx, messageID)`。
- `RemoveReaction(ctx, messageID uuid.UUID, principalID, agentID *int, emoji string) ([]Reaction, error)`
  - `DELETE FROM message_reaction WHERE message_id=$1 AND emoji=$2 AND (principal_id IS NOT DISTINCT FROM $3 AND agent_id IS NOT DISTINCT FROM $4)`，再聚合。
- `aggregateReactions(ctx, messageID) ([]Reaction, error)`：
  ```sql
  SELECT r.emoji, count(*),
         COALESCE(array_agg(COALESCE(p.name, a.name) ORDER BY r.created_at) FILTER (WHERE r.principal_id IS NOT NULL OR r.agent_id IS NOT NULL), '{}'),
         bool_or(<caller matches>)
  FROM message_reaction r
  LEFT JOIN principal p ON p.id = r.principal_id
  LEFT JOIN agent a ON a.id = r.agent_id
  WHERE r.message_id = $1
  GROUP BY r.emoji ORDER BY r.emoji
  ```
  `reacted` 由调用者身份注入。
- 写入后调用 `roomNotifier.NotifyConversation(convID)`（先解析 message → conversation）。
- `ListReactionsForMessages(ctx, convID, messageIDs)`：`ListConversationMessages` / `ListThreadMessages` 读取时批量填充 `Reactions`（类似 `fillThreadReplyCounts` / `fillTaskInfo` 的 one-grouped-query 模式，避免 N+1）。

### 10.2 API（`backend/manager/api/v1/command.go` 新增 handler）

`AddReaction` / `RemoveReaction` 共用一段授权逻辑（镜像 `PostMessage`）：

1. 解析 `message` 名 → `parseMessageID`（conv + msg 两个 uuid）。
2. `GetConversation`，不存在 → `NOT_FOUND`。
3. **成员门禁**：
   - agent 调用者：`IsConversationMember(ctx, convID, MemberTypeAgent, agent.ResourceID)`，非成员 → `PERMISSION_DENIED`；
   - user 调用者：按现有 user 消息路径的会话策略校验可读。
4. 确认 `message` 存在于该对话（且可被反应；thread reply 亦可）。
5. `normalizeReactionEmoji`（server 权威）失败 → `INVALID_ARGUMENT`。
6. 调用 store；`RemoveReaction` 前先判断：该 emoji 上**存在**他人反应且非本人 → `PERMISSION_DENIED`（见 §12 幂等/权限边界）。
7. 返回更新后的 `reactions` 聚合。

`ListConversationMessages` / `ListThreadMessages` 在填充 replies 后追加 `fillReactions`，把 `Reactions` 挂到每条消息。

---

## 11. chattools / daemon / CLI

### 11.1 chattools（`backend/agent/chattools/chattools_reaction.go` 新建）

- `AddReaction(ctx, d, in{Message, Emoji}) (string, error)` / `RemoveReaction(...)`：
  - 先 `normalizeReactionEmoji` 本地校验（快速失败）；
  - `resolveMessageName(ctx, d, in.Message)` 得到 `conversations/<c>/messages/<m>`；
  - 调 RPC，返回规范化文本。
- `GetConversationMessages` / `GetThreadMessages` 的 `formatMessageLine` 增加可选 reactions 行（§8.2）。

### 11.2 daemon（`backend/agent/daemon/server.go`）

- 新增 handler：`/reaction/add`、`/reaction/remove`，走 `s.run`（authorize → decode → chattools → write）。
- `Request` 增加 `Emoji string`；reaction 的 message 用新增字段（如 `ReactionEmoji` + 复用 handle 传入），避免与 task RPC 的 `Message`（全名）语义混淆。

### 11.3 CLI（`backend/agent/cmd/message.go`）

```go
func init() {
    messageCmd.AddCommand(..., messageReactCmd)
}
var (
    messageReactEmoji  string
    messageReactRemove bool
)
var messageReactCmd = &cobra.Command{
    Use:   "react <message-handle>",
    Short: "Add or remove an emoji reaction on a message (lightweight feedback)",
    Args:  cobra.ExactArgs(1),
    RunE:  /* --emoji 必填；调 /reaction/add 或 /reaction/remove */,
}
func init() {
    messageReactCmd.Flags().StringVar(&messageReactEmoji, "emoji", "", "single emoji (e.g. 👍, ✅) — required")
    messageReactCmd.Flags().BoolVar(&messageReactRemove, "remove", false, "remove the reaction instead of adding it")
}
```

**输出格式**（对齐代码库"输出即粘贴用 handle"的约定）：

```
# 添加
Reaction 👍 added to '#general:550e8400-e29b-41d4-a716-446655440000'.

# 移除
Reaction 👍 removed from '#general:550e8400-e29b-41d4-a716-446655440000'.
```

> 说明：你最初的例子 `Reaction 👍 added to message 550e8400.` 把 id 截断成 8 位。本设计**保留完整 `<address>:<message-id>` handle**（channel 加单引号），与代码库"message read / task claim 复制即用"的约定一致，避免 agent 截断/拼错。若你坚持截断短 id，可在确认后改为短 id —— 但我不建议。

---

## 12. 边界与语义细节

- **幂等**：
  - add 已存在的 (msg, emoji, self) → no-op，返回当前聚合（成功）。
  - remove 不存在的 (msg, emoji, self) → no-op，返回当前聚合（成功）。
  - remove 存在但**属于他人** → `PERMISSION_FAILED`（先查该 emoji 是否有本人反应行）。
- **移除他人反应**：确认 #3 只允许操作者移除自己的。一个例外需要考虑——是否给 channel Admin/Owner 额外"清掉不合适反应"的能力？本期**不引入**（保持简单），作为后续可选项。
- **线程 reply**：reaction 可作用于任何可见消息（channel 顶层 + thread reply）。`thread read` 输出同样显示 reactions。
- **system 行**（`✅ done`、`📋 created task` 等）：允许 reaction 但引导语提示 agent 一般不对 system 通知反应。
- **多字节/多 codepoint emoji**：按 rune 计数，`👍🏽`、family emoji 均合法（§6）。
- **并发**：同一 reactor 对同一 (msg, emoji) 的并发 add/remove，靠唯一索引 + `ON CONFLICT DO NOTHING` / `DELETE` 天然串行正确，无需乐观锁。
- **不唤醒**：store 写入仅触发前端实时通知（§7），绝不触发 `dispatcher.NotifyNewMessages` / 不生成 `GenerateActivityForMessage` / 不 `UpsertCursor`。
- **删除消息**：`ON DELETE CASCADE` 清理 reaction。

---

## 13. 测试计划

- **store**（`message_reaction_test.go`）：add/remove 基本路径；幂等（重复 add no-op、remove 不存在 no-op）；聚合计数 + 反应者 + `reacted`；user/agent 双 actor；CASCADE。
- **common**（`reaction_test.go`）：`normalizeReactionEmoji` 的 trim / 空 / 各空白字符 / >16 rune / 合法多 codepoint。
- **API**（`command_reaction_test.go`）：非成员 → `PERMISSION_DENIED`；消息不存在 → `NOT_FOUND`；非法 emoji → `INVALID_ARGUMENT`；移除他人反应 → `PERMISSION_DENIED`；`ListConversationMessages` / `ListThreadMessages` 填充 reactions。
- **chattools**：reaction 输出文本；`message read` / `thread read` 的 reactions 行渲染。
- **CLI**（`message_test.go`）：`message react` 参数缺失、emoji 必填、`--remove` 分支、成功输出文本。
- **migrator**（`migrator_test.go`）：`0020##message-reaction.sql` 可执行、LATEST 与增量一致。
- **前端**：`message-row.test.tsx` 渲染 reaction 条 + 点击切换；chat store reaction 状态更新。

---

## 14. 实施清单（按依赖序）

1. `backend/common/reaction.go` — `normalizeReactionEmoji`。
2. `proto/v1/v1/command.proto` — `Reaction`、`ChatMessage.reactions`、`AddReaction/RemoveReaction` RPC；`cd proto && buf generate`。
3. `backend/manager/migration/migration/1.1/0020##message-reaction.sql` + 追加 `LATEST.sql`。
4. `backend/manager/store/message_reaction.go` — store 方法 + `fillReactions`。
5. `backend/manager/api/v1/command.go` — `AddReaction` / `RemoveReaction` handler + 填充逻辑。
6. `backend/agent/chattools/chattools_reaction.go` — `AddReaction` / `RemoveReaction` + reactions 行渲染。
7. `backend/agent/daemon/server.go` — `/reaction/add|remove` handler + Request 字段。
8. `backend/agent/cmd/message.go` — `message react` 子命令。
9. `backend/agent/executor/prompt/communication.md` — 引导语 + 输出说明。
10. `frontend/...` — reaction 条组件、store action、proto-es 类型。
11. 各层测试（§13）。

> 改动后按 `AGENTS.md`：Go 走 `gofmt -w` + `golangci-lint run`（循环到干净）；proto 走 `buf format/lint/generate`；前端走 `biome:check` + `type-check` + `test`。

---

## 15. 本期不做 / 未来可扩展

- channel Admin/Owner 移除他人反应（治理能力）。
- 独立 `reactions_seq` + `WatchReactions` 专用长轮询（多实例/高规模实时）。
- 可配置 emoji 白名单 / 每消息 reaction 上限 / 频率限制（当前靠引导语约束）。
- 对 reaction 的 @提及 / 通知（本期刻意不做，保持轻量）。
