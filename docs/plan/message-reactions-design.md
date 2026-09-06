# 消息 emoji 反应（Message Reactions）设计

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：本设计已全量落地（表/迁移/proto/store/API/chattools/daemon/CLI/引导语/前端均在），本文按实现修正了 store/API 层的实际签名与文件位置、emoji 校验规则细节（新增零宽空格拒绝）、测试现状与实施清单勾稽。

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

## 5. proto 变更（`proto/v1/v1/command.proto`）— 已实现

`buf generate` 重新生成 Go + TypeScript。

### 5.1 Reaction 消息（已实现，字段一致）

```proto
// Reaction 是某条消息上针对一个 emoji 的聚合。count 为反应者总数；
// reactors 为反应者显示名；reacted 为调用者相对字段（是否已反应）。
message Reaction {
  string emoji = 1;
  int32 count = 2;
  repeated string reactors = 3;
  bool reacted = 4;          // caller-relative，镜像 ChatMessage.is_own
}
```

### 5.2 ChatMessage 增加字段（已实现，编号 19 正确落地）

`ChatMessage.reactions = 19`：按 emoji 分组的聚合，由 `ListConversationMessages` / `ListThreadMessages` 批量填充；无反应时为空列表。reaction 永不 bump `room_version`（proto 注释明确此旁路语义）。

### 5.3 RPC（挂在 CommandService，user 与 agent 共用；已实现）

`AddReactionRequest` / `AddReactionResponse` / `RemoveReactionRequest` / `RemoveReactionResponse` 与上文一致（实现中 REQUIRED 用 `[(google.api.field_behavior) = REQUIRED]` 表达）。handler 位于 `backend/manager/api/v1/command_reaction.go`。

> 不做 `ToggleReaction`：显式 add / remove 两个 RPC 与 CLI 的 `[--remove]` 一一对应。前端 store（`frontend/src/stores/chat.ts` 的 `toggleReaction`）按 `reacted` 字段自行决定调 add 还是 remove。

---

## 6. 校验：`NormalizeReactionEmoji`（已实现于 `backend/common/reaction.go`）

manager 侧（权威）与 CLI 侧（chattools，本地快速失败）共用同一实现。

规则（按确认）：trim → 非空 → **不含任何空白字符**（空格/制表/换行及 U+2000–U+200A 等 Unicode 空白全部拒绝，从而拒绝 `"thumbs up"` 这类文本；另显式拒绝 U+200B 零宽空格——它不属于 Unicode White_Space，需要单独拦截）→ **≤16 rune**（不是字节）。

```go
func NormalizeReactionEmoji(s string) (string, error) {
    s = strings.TrimSpace(s)
    if s == "" {
        return "", errors.New("emoji is required")
    }
    for _, r := range s {
        if unicode.IsSpace(r) { ... 拒绝 ... }
        if r == '\u200b' { ... 拒绝（零宽空格） ... }
    }
    if utf8.RuneCountInString(s) > maxReactionEmojiRunes /* 16 */ {
        return "", errors.Errorf("emoji too long (max %d runes)", maxReactionEmojiRunes)
    }
    return s, nil
}
```

- **按 rune（非字节）**：`👍`=1 rune；`👍🏽`（肤色修饰，2 codepoint）= 2 runes，合法；family emoji（多 codepoint 拼接）仍在 16 rune 内。
- **不做白名单**：任意单 emoji 都接受，只保证"是单个 emoji、不是文本"。单个非空白单词（如 `okay`）**不会**被拒——设计确认只拦空白，文本由引导语约束。
- 校验失败 → manager 返回 `INVALID_ARGUMENT`（CLI 侧映射为 `INVALID_ARGUMENT_FAILED`，见 §9）；CLI 在 chattools 层先本地校验以便 `--help`/错误更友好。

---

## 7. 实时传播（已按推荐方案实现）

**背景**：前端聊天流是"按 `after_version` 增量 + `wait_ms` 长轮询"（`ListConversationMessages`，服务端 `wait_ms` 上限 30000ms，由 `backend/manager/component/roomhub/roomhub.go` 的 pub/sub 唤醒）。reaction 不 bump 版本 → 消息增量恒为空 → 若不处理，前端拿不到实时 reaction。

**实现（即原推荐方案）**：
- store 的 `AddReaction` / `RemoveReaction` 写完后调用 `s.roomNotifier.NotifyConversation(conversationID)`（`backend/manager/store/message_reaction.go`，notifier 由 store 持有；`backend/manager/component/roomhub/roomhub.go` 的 `Hub.NotifyConversation` 非阻塞唤醒所有等待者）。
- 前端聊天长轮询被唤醒后重新拉取当前页面——`ListConversationMessages` / `ListThreadMessages` 的 `fillReactions`（`backend/manager/api/v1/command_message.go`）每次都把最新聚合挂到消息上，reaction 随增量（可能为空）的重新拉取自然刷新；客户端以同一 `after_version` 重新发起即可。
- 空增量对被意外唤醒的消息长轮询无害。

**权衡**：reaction 风暴会带来少量多余的聊天长轮询往返（不破坏正确性）。当前单进程可接受。

**未来 refinement（至今未实现）**：给 `conversation` 加独立 `reactions_seq` 计数器 + `WatchReactions(conversation, after_seq)` 专用长轮询，与消息版本彻底隔离；多实例部署时换 Postgres LISTEN/NOTIFY 后端（与 roomhub 现有注释一致）。

---

## 8. agent 引导语（`backend/agent/executor/prompt/communication.md`）— 已实现

Commands 表中的 `message react` 行、`message read` / `thread read` 输出的 `reactions:` 行、以及 `INVALID_ARGUMENT_FAILED` 中对非法 emoji 的说明，均与下文设计一致地存在于当前 communication.md。

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

## 9. 错误码（已实现，与 `chattools.wrapManagerError` 映射一致）

沿用现有 `chattools.Error` 体系（CLI 渲染 `Error:` / `Code:` / `Next action:`；映射见 `backend/agent/chattools/chattools.go` 的 `wrapManagerError`）：

| Code | 触发 |
|------|------|
| `INVALID_ARGUMENT_FAILED` | emoji 校验失败（空 / 含空白 / >16 rune）；`message` 参数缺失 |
| `NOT_FOUND_FAILED` | 消息或对话不存在 |
| `PERMISSION_FAILED` | 非对话成员；移除**他人已存在**的反应 |
| `AUTH_FAILED` | agent token 被拒（瞬时，可重试一次） |
| `REQUEST_FAILED` / `SERVER_5XX` | 其它 4xx / 服务端错误 |

---

## 10. 后端 store / API 实现要点（已实现；签名与机制按实际代码修正）

### 10.1 store（`backend/manager/store/message_reaction.go`）

- `AddReaction(ctx, conversationID, messageID uuid.UUID, callerPrincipalID, callerAgentID *int, emoji string) ([]*v1pb.Reaction, error)`
  - `INSERT INTO message_reaction ... ON CONFLICT DO NOTHING`（幂等，SQL 以常量 `addReactionSQL` 锁形测试），随后聚合返回。
- `RemoveReaction(ctx, conversationID, messageID uuid.UUID, callerPrincipalID, callerAgentID *int, emoji string) (ReactionRemoveResult, error)`
  - 一条 `reactionInspectSQL` 同时查出"调用者是否有此 emoji"（`callerHas`）与"是否有他人反应"（`othersHas`，actor 二元组 IS DISTINCT FROM 调用者）；仅当 `callerHas` 时执行 caller-scoped `DELETE`（`removeReactionSQL`）。返回 `ReactionRemoveResult{Removed, Others, Reactions}`，把"移除他人反应"的判定交给 API 层。
- `queryReactions(ctx, messageIDs, callerPrincipalID, callerAgentID)`（批量聚合，供单条与批量共用）：
  ```sql
  SELECT r.message_id, r.emoji, count(*)::int,
         COALESCE(array_agg(COALESCE(p.name, a.name) ORDER BY r.created_at), '{}'),
         bool_or(COALESCE(r.principal_id = $2, false) OR COALESCE(r.agent_id = $3, false))
  FROM message_reaction r
  LEFT JOIN principal p ON p.id = r.principal_id
  LEFT JOIN agent a ON a.id = r.agent_id
  WHERE r.message_id = ANY($1)
  GROUP BY r.message_id, r.emoji
  ORDER BY r.message_id, r.emoji
  ```
  `reacted` 由调用者身份（principal 或 agent 二选一）经 `bool_or` 计算。
- 写入后由 **store 自身**调用 `s.roomNotifier.NotifyConversation(convID)`（§7）。
- `ListReactionsForMessages(ctx, messageIDs, callerPrincipalID, callerAgentID)`：返回 `map[messageID][]*Reaction`（无反应的消息映射为空切片）；被 `fillReactions` 用于批量填充，避免 N+1。

### 10.2 API（`backend/manager/api/v1/command_reaction.go`）

- `AddReaction` / `RemoveReaction` handler 共用 `requireReactionCaller(ctx, convID, msgID)`：
  1. 解析 `message` 名 → `parseMessageName`（conv + msg 两个 uuid）。
  2. `store.MessageExistsInConversation` 确认消息存在于该对话，否则 `NOT_FOUND`（thread reply 同样可反应）。
  3. `common.NormalizeReactionEmoji`（server 权威）失败 → `INVALID_ARGUMENT`。
  4. **agent 调用者**：`requireAgentMemberByConvID` 会话成员门禁，非成员 → `PERMISSION_DENIED`；**user 调用者**：仅需认证（未做会话成员/策略校验——与设计稿的"user 会话策略校验"不同，当前实现对 user 放行）。
  5. `RemoveReaction`：调用 store 后，若 `!Removed && Others`（emoji 存在但属于他人）→ `PERMISSION_DENIED`；其余 no-op 情形成功返回当前聚合。
- `fillReactions(ctx, msgs, v1msgs)`：`ListConversationMessages` / `ListThreadMessages`（`backend/manager/api/v1/command_message.go`）读取时批量填充 `Reactions`。
- caller 身份解析（`reactionCallerFromContext`）：agent 调用者取 agent.ID，user 调用者取 user.ID。

---

## 11. chattools / daemon / CLI（已实现）

### 11.1 chattools（`backend/agent/chattools/chattools_reaction.go`）

- `AddReaction(ctx, d Deps, in ReactionInput) (string, error)` / `RemoveReaction(...)`（`ReactionInput{Message, Emoji}`）：
  - 先 `common.NormalizeReactionEmoji` 本地校验（快速失败，`INVALID_ARGUMENT_FAILED`）；
  - `resolveMessageName(ctx, d, in.Message)` 得到 `conversations/<c>/messages/<m>`；
  - 调 RPC，返回 `formatReactionResult` 规范化文本（回显完整 handle，channel handle 带单引号；注意响应中的 `reactions` 聚合不打进文本）。
- `formatReactionsLine(reactions)`：渲染 `  reactions: 👍 ×2 (alice, rei-agent-1), ✅ (bob)` 行（空时返回 ""）；`message read` / `thread read` 的消息行渲染处（`chattools.go`）追加该行。

### 11.2 daemon（`backend/agent/daemon/`）

- mux 注册在 `backend/agent/daemon/server.go`：`/reaction/add`、`/reaction/remove`；handler 实现在 `backend/agent/daemon/handlers_chat.go`，走 `s.run`（authorize → decode → chattools → write）。
- `Request` 增加 `ReactionEmoji string`（json `reaction_emoji`）；message 用现有 `Message` 字段传 `<address>:<message-id>` handle。

### 11.3 CLI（`backend/agent/cmd/message.go`）

`message react <message-handle> --emoji <emoji> [--remove]` 子命令已实现：`--emoji` 必填（help 文案 "single emoji to react with (e.g. 👍, ✅) — required"），`--remove` 决定调 `/reaction/add` 还是 `/reaction/remove`。

**输出格式**（与设计一致，由 `formatReactionResult` 保证，测试锁定）：

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
  - add 已存在的 (msg, emoji, self) → no-op（`ON CONFLICT DO NOTHING` 命中唯一索引），返回当前聚合（成功）。
  - remove 不存在的 (msg, emoji, self) → no-op，返回当前聚合（成功）。
  - remove 存在但**属于他人** → store 检出 `Removed=false, Others=true` → API 返回 `PERMISSION_DENIED`（一条 inspect 查询同时判定两种情形，见 §10.1）。
- **移除他人反应**：确认 #3 只允许操作者移除自己的。Admin/Owner"清掉不合适反应"的治理能力**未实现**（保持简单），仍是后续可选项。
- **线程 reply**：reaction 可作用于任何可见消息（channel 顶层 + thread reply）。`thread read` 输出同样显示 reactions。
- **system 行**（`✅ done`、`📋 created task` 等）：技术上允许 reaction（无 sender 限制）；引导语要求把 reaction 当作上下文、不回复、不自动反应。
- **多字节/多 codepoint emoji**：按 rune 计数，`👍🏽`、family emoji 均合法（§6）。
- **并发**：同一 reactor 对同一 (msg, emoji) 的并发 add/remove，靠部分唯一索引 + `ON CONFLICT DO NOTHING` / caller-scoped `DELETE` 天然串行正确，无需乐观锁。
- **不唤醒**：store 写入仅触发 `roomNotifier.NotifyConversation`（前端长轮询），绝不触发 `dispatcher.NotifyNewMessages` / 不生成 `GenerateActivityForMessage` / 不 `UpsertCursor`。
- **删除消息**：`ON DELETE CASCADE` 清理 reaction。

---

## 13. 测试现状（2026-09-06 核对）

- **store**（`backend/manager/store/message_reaction_test.go`）：为无库的 SQL 锁形测试——`TestAddReactionSQL`（`ON CONFLICT DO NOTHING` 在位）、`TestRemoveReactionCallerScopedSQL`（caller-scoped DELETE）、`TestAggregateReactionsSQL`（聚合/`reacted` SQL 形状）。原计划的"add/remove 基本路径、幂等、CASCADE"等行为测试未加（依赖真实库的行为由 SQL 守卫间接锁定）。
- **common**（`backend/common/reaction_test.go`）：`TestNormalizeReactionEmoji` 覆盖 trim / 空 / 空白 / 零宽空格 / 超长 / 合法多 codepoint。
- **API**（`backend/manager/api/v1/command_reaction_test.go`）：`TestReactionCallerFromContextNoIdentity`、`TestFillReactionsEmpty`。原计划中的"非成员 → PERMISSION_DENIED / 消息不存在 → NOT_FOUND / 移除他人反应"等 handler 级测试未加。
- **chattools**（`backend/agent/chattools/chattools_reaction_test.go`）：`TestFormatReactionResultAdd/Remove/DMHandle`（含 channel 单引号与 dm: 无引号两分支）、`TestFormatReactionsLineEmpty/Line`（`  reactions: 👍 ×2 (alice, rei-agent-1), ✅ (bob)` 渲染）。
- **CLI**：`backend/agent/cmd/message.go` 的 `message react` 无独立测试文件（原计划的 `message_test.go` 未建）。
- **migrator**（`backend/manager/migration/migrator_test.go`）：仅有通用的版本文件/LATEST 嵌入测试；没有 0020 专属用例。`migration_test.go` 的 schema-invariant 守卫中也未加 message_reaction 项。
- **前端**（`frontend/src/components/chat/message-row.test.tsx`）：`MessageRow reaction bar` 套件覆盖 emoji 计数 pill 渲染；chat store 的 `toggleReaction` 无专测。

---

## 14. 实施清单（已全部落地；括号内为实际文件）

1. `backend/common/reaction.go` — `NormalizeReactionEmoji`（含零宽空格拒绝）。✅
2. `proto/v1/v1/command.proto` — `Reaction`、`ChatMessage.reactions = 19`、`AddReaction/RemoveReaction` RPC；`cd proto && buf generate`。✅
3. `backend/manager/migration/migration/1.1/0020##message-reaction.sql` + `backend/manager/migration/migration/LATEST.sql`（`message_reaction` 表同款 DDL）。✅
4. `backend/manager/store/message_reaction.go` — store 方法 + 批量聚合（无独立 `fillReactions`，由 API 层实现）。✅
5. `backend/manager/api/v1/command_reaction.go` — `AddReaction` / `RemoveReaction` handler 与 `fillReactions`；填充挂在 `backend/manager/api/v1/command_message.go` 的两个 List handler。✅
6. `backend/agent/chattools/chattools_reaction.go` — `AddReaction` / `RemoveReaction` + `formatReactionResult` / `formatReactionsLine`。✅
7. `backend/agent/daemon/server.go`（mux 注册）+ `backend/agent/daemon/handlers_chat.go`（`/reaction/add|remove` handler）；`Request.ReactionEmoji` 字段在 `server.go`。✅
8. `backend/agent/cmd/message.go` — `message react` 子命令。✅
9. `backend/agent/executor/prompt/communication.md` — 引导语 + 输出说明。✅
10. `frontend/src/components/chat/message-row.tsx`（reaction 条）、`frontend/src/stores/chat.ts`（`toggleReaction`）、`frontend/src/stores/ui-models.ts`（`ChatMessageUI.reactions`）。✅
11. 测试见 §13（部分按原计划缩水）。

> 改动后按 `AGENTS.md`：Go 走 `gofmt -w` + `golangci-lint run`（循环到干净）；proto 走 `buf format/lint/generate`；前端走 `biome:check` + `type-check` + `test`。

---

## 15. 未实现 / 未来可扩展（2026-09-06 核对：以下均未实现）

- channel Admin/Owner 移除他人反应（治理能力）。
- 独立 `reactions_seq` + `WatchReactions` 专用长轮询（多实例/高规模实时）。
- 可配置 emoji 白名单 / 每消息 reaction 上限 / 频率限制（当前靠引导语约束）。
- 对 reaction 的 @提及 / 通知（刻意不做，保持轻量）。
