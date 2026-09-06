# 重构 Agents 页面为左右两栏 + Tab 详情

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：方案已落地但形态此后继续演进——agent 目录现由 Members 页承载（`/members` 左栏），agent 详情挂在 `/members/agents/:agentId` 并复用共享 `DetailTabsLayout`（tab 扩为 profile/commands/reminders/chat/mcp/workspace 六个），`/agents` 仅作旧链接重定向；后端 `ListChannelsForAgent` RPC、store 查询与 channel slice 均已实现（现状见文末「实现现状」）。

## Context

（本节描述的是重构前的旧状态，仅作历史背景；重构已实现并进一步演进，见文末「实现现状」。）

重构前 `AgentsPage`(`frontend/src/pages/dashboard/agents.tsx`)是一个单页表格:列含 name/status/hostname/os/ip/actions,行点击弹出只读详情 `Dialog`,另有一个独立的 ACP 配置 `Sheet`(provider/model/custom-env)由 "Configure" 按钮触发。已有的 `agents/:agentId` 路由(`AgentWorkspaceLayout`)误用了 Tabs(chat 跳到私聊、tasks 实际渲染 commands 子页)。

目标:把 agent 页面重构成左右两栏——左侧 agent 列表,右侧选中 agent 的详情,详情由三个 tab 组成:`profile`(原 Dialog 只读信息 + 原 Sheet 的 ACP 编辑 + token 操作)、`命令`(复用现有 `CommandListPage`)、`Chat`(该 agent 加入的 channel 列表)。选中 agent 与当前 tab 进入 URL(可深链、刷新保留、后退可用)。

关键缺口:后端目前没有"列出某 agent 加入的所有 channel"的 RPC——`ListChannels` 是按当前用户过滤的。`conversation_member_meta` 表已用 `MemberTypeAgent=2` + `member_id=agentResourceID` 记录 agent 成员关系,只需补一个查询。(已实现:见 Phase 1 与「实现现状」。)

用户已确认:① 新增后端 RPC;② URL 驱动路由;③ 命令 tab 直接复用现有 commands 页;④ 详情 Dialog 与 ACP Sheet 全部并入 profile tab 并删除弹窗。

---

## Phase 1 — Backend

### 1.1 新增 `ListChannelsForAgent` RPC(`proto/v1/v1/command.proto`)

放在 `CommandService`(不要放 `AgentService`):`agent.proto` 不导入 `command.proto`,而 `command.proto` 已导入 `agent.proto`;若把引用 `Conversation` 的 RPC 放进 `agent.proto` 会形成 proto 循环导入。`CommandService` 本身已有大量带 IAM 注解的 RPC,且 `Conversation` 类型就在 `command.proto`,可直接复用,无需新导入。

在 `command.proto` 的 `ListChannels` RPC 之后追加:

```proto
rpc ListChannelsForAgent(ListChannelsForAgentRequest) returns (ListChannelsForAgentResponse) {
  option (google.api.http) = { get: "/v1/{name=agents/*}/channels" };
  option (laelia.v1.auth_method) = IAM;
  option (laelia.v1.permission) = "laelia.agents.get";
}

message ListChannelsForAgentRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference).type = "laelia/Agent"
  ];
  int32 page_size = 2;
  string page_token = 3;
}

message ListChannelsForAgentResponse {
  repeated Conversation channels = 1;
  string next_page_token = 2;
}
```

已实现差异:落地的 RPC(见 `proto/v1/v1/command.proto` `CommandService` 中 `ListChannelsForAgent`,位于 `ListChannels` 之后)**没有**加 `google.api.http` 注解(纯 ConnectRPC,不走 HTTP 路由),只保留 `auth_method=IAM` 与 `permission=laelia.agents.get`;请求/响应 message 与上面一致。

权限 `laelia.agents.get` 由 `backend/common/permission/permission_gen.go` 生成(`AgentsGet`,源清单在 `backend/common/permission/permission.json`);IAM 拦截器按注解自动鉴权(`backend/manager/api/v1/iam.go` 的 `authorize`,约 55 行),无需在 handler 里手写鉴权。早期草稿提到的 `iam.go` 内 `PermAgentRead` 常量已不存在,权限集中管理已迁移到 `backend/common/permission`。

### 1.2 新增 store 查询 `ListAgentConversations`(`backend/manager/store/conversation.go`)

镜像 `ListUserConversationsWithUnread`(`backend/manager/store/conversation.go:632`),把 `member_type` 绑定为 `MemberTypeAgent`、`member_id` 绑定为 agent 的 `resourceID` 字符串(与 `findDirectConversation` 中 agent member_id 的存法一致)。`member_id` 即请求里 `agents/{resourceID}` 解出的 resourceID,无需 int↔string 转换。

已实现签名比设计多一个 viewer 过滤参数(非 reviewAll 用户只能看到自己也在的 channel):

```go
func (s *Store) ListAgentConversations(ctx context.Context, agentResourceID string, viewer *ConversationMemberFilter, limit, offset int) ([]*UserConversation, error)
```

v1 unread 一律返回 0(agent_channel_cursor 是 agent 自身的已读位置,对"管理员查看 agent 加入的 channel 名单"无意义)。加注释说明刻意为 0。SQL 复用 `ListUserConversationsWithUnread` 的 JOIN,把 `LEFT JOIN user_channel_cursor` 去掉、`WHERE cm.member_type = MemberTypeAgent AND cm.member_id = $1`。

### 1.3 新增 handler(`backend/manager/api/v1/channel.go`)

在 `ListChannels` 之后加 `ListChannelsForAgent`(已实现于 `backend/manager/api/v1/channel.go:130`),复用其分页(`parseLimitAndOffset` maximum=100、`limitPlusOne`,现位于 `backend/manager/api/v1/common.go:219`)、`GetConversationMemberCount`、`resolveUserName`、DM title 回退、`convertToV1Conversation`(现位于 `backend/manager/api/v1/channel_convert.go:19`)。鉴权由 IAM 拦截器按 proto 注解完成;handler 只需 `GetUserFromContext` 做 unauthenticated 兜底。`name` 用现有 `common.GetAgentResourceID` 解出 resourceID 传入 store。

已实现差异:handler 额外做一层可见性收窄——非 `conversations.reviewAll`(`permission.ConversationsReviewAll`)持有者只看到自己也是成员的 channel(`store.ConversationMemberFilter` 传给 `ListAgentConversations`);user DM 标题回退为 owner 用户名,agent↔agent DM 通过 `resolveAgentDMPeer` 解析对端 agent。

### 1.4 注册 handler

`v1connect.NewCommandServiceHandler` 在 `buf generate` 后会带上新方法签名;在 `backend/manager` 现有 `CommandService` handler 注册处一并注册(无需新增独立注册,接口实现即可,编译会强制实现)。

---

## Phase 2 — Proto 重生成

```bash
buf format -w proto
buf lint proto
cd proto && buf generate
```

确认 `frontend/src/types/proto-es/v1/command_pb` 出现 `ListChannelsForAgentRequestSchema`/`ResponseSchema` 及 `CommandService` 上的 `listChannelsForAgent` 方法;`backend/generated-go/v1` 出对应 Go stub。

---

## Phase 3 — Frontend store

### 3.1 `fetchChannelsForAgent` 加进 channel slice(`frontend/src/stores/channel.ts`)

返回 `Conversation[]` 且用 `commandServiceClient`,与 `fetchChannels` 同源,放 channel slice 而非 agent slice。(已实现:字段与函数如下;slice 接口定义在 `channel.ts` 本文件内,由 `stores/types.ts` 引入;`fetchChannels` 现位于 `channel.ts:227`,`fetchChannelsForAgent` 在 `channel.ts:281`。)

`ChannelSlice` 增加:

```ts
agentChannelsByAgent: Record<string, Conversation[]>;
agentChannelsLoading: boolean;
fetchChannelsForAgent: (agentName: string) => Promise<void>;
```

实现镜像 `fetchChannels`:

```ts
async fetchChannelsForAgent(agentName) {
  set({ agentChannelsLoading: true });
  try {
    const res = await commandServiceClient.listChannelsForAgent(
      create(ListChannelsForAgentRequestSchema, { name: agentName, pageSize: 100, pageToken: "" })
    );
    set((s) => ({ agentChannelsByAgent: { ...s.agentChannelsByAgent, [agentName]: res.channels ?? [] } }));
  } finally {
    set({ agentChannelsLoading: false });
  }
}
```

---

## Phase 4 — Frontend 路由(`frontend/src/router/routes/dashboard.tsx`)

(设计时的 `/agents` 父子路由已实现;此后 agent 详情整体迁入 Members 页——见文末「实现现状」。)

当时的设计:把现有并列的 `agents` 与 `agents/:agentId` 合并为父子结构:父路由 `agents` 渲染新的两栏 `AgentsPage`(左列表 + `<Outlet/>` 右详情),子路由 `agents/:agentId` 渲染新的 `AgentDetailLayout`(顶部标题 + Tabs + `<Outlet/>`),tab 用字面路径而非 `:tab` 参数(因为 `commands/:commandId` 是字面子路由,会与 `:tab` 冲突)。

```tsx
{
  path: "agents",
  lazy: () => import("@/pages/dashboard/agents").then((m) => ({ Component: m.AgentsPage })),
  children: [
    { index: true, handle: { name: AGENT_ROUTE_LIST }, element: <AgentDetailEmptyState /> },
    {
      path: ":agentId",
      lazy: () => import("@/app/layouts/agent-detail-layout").then((m) => ({ Component: m.AgentDetailLayout })),
      children: [
        { index: true, handle: { name: AGENT_ROUTE_PROFILE },
          lazy: () => import("@/pages/dashboard/agent-profile").then((m) => ({ Component: m.AgentProfilePage })) },
        { path: "commands", handle: { name: COMMAND_ROUTE_LIST },
          lazy: () => import("@/pages/dashboard/command-list").then((m) => ({ Component: m.CommandListPage })) },
        { path: "commands/:commandId", handle: { name: COMMAND_ROUTE_DETAIL },
          lazy: () => import("@/pages/dashboard/command-detail").then((m) => ({ Component: m.CommandDetailPage })) },
        { path: "chat", handle: { name: AGENT_ROUTE_CHAT },
          lazy: () => import("@/pages/dashboard/agent-chat").then((m) => ({ Component: m.AgentChatPage })) },
      ],
    },
  ],
}
```

`AgentDetailEmptyState` 可以是 `agents.tsx` 导出的一个空态组件,或简单 `<Navigate to=...>`——推荐就地空态文案。

`frontend/src/router/handles.ts` 增加 `AGENT_ROUTE_PROFILE = "agent.profile"`、`AGENT_ROUTE_CHAT = "agent.chat"`(落地后还加了 `AGENT_ROUTE_MCP`、`AGENT_ROUTE_WORKSPACE`);`resolvePath` 在 `frontend/src/router/route-index.ts`(注意是 `.ts`,设计稿误写为 `.tsx`)。

旧 `agent-workspace-layout.tsx` 布局已按计划删除(该文件已不存在,现仅存页面 `frontend/src/pages/dashboard/agent-workspace.tsx`,作为 workspace tab 的内容页)。

---

## Phase 5 — Frontend 组件

### 5.1 抽出可复用子组件到 `frontend/src/components/agent/`

已落地:`string-list-editor.tsx`、`key-value-env-editor.tsx` 均存在;此外 ACP 编辑器后续又抽成了独立组件 `frontend/src/components/agent/acp-config-editor.tsx`(配套 `frontend/src/lib/acp-config-draft.ts` 的 `useAcpConfigDraft` 草稿逻辑),profile tab 通过 `key` 重挂载按 agent 重新 seed。

### 5.2 重写 `frontend/src/pages/dashboard/agents.tsx` → 两栏 shell

(已实现;此后两栏目录页又被 Members 页取代,见「实现现状」。当前 `agents.tsx` 只剩 `agentLifecycle`/`lifecycleLabel` 生命周期分类辅助函数,供 profile tab 等复用,不再承载列表页。)

保留:`fetchAgents`、3s 轮询(`anyNonReady`)、Create 按钮 + 创建后展示 bootstrap token 的 Dialog、删除 AlertDialog、`agentLifecycle`、`agentsEqual`。

删除:行点击 Dialog handler、详情 Dialog(404-566)、ACP Sheet(568-750)、rotate/revoke AlertDialog、以及所有相关本地 state(`selectedAgent/detailOpen/acpConfigOpen/executable/args/allowEnv/provider/model/customEnvEntries/saving/saveError/refreshing/refreshError/rotateOpen/revokeOpen/rotating/revoking/actionError`)。token Dialog 与 delete AlertDialog 留在本文件。

布局:

```tsx
<div className="flex h-full">
  <div className="w-[360px] shrink-0 border-r border-control-border overflow-auto">
    {/* header + Create button */}
    {/* Table of agents */}
  </div>
  <div className="flex-1 overflow-hidden"><Outlet /></div>
</div>
```

表格改动:行点击 → `navigate(\`/agents/${agentId}\`)`;高亮选中行(用 `useParams<{agentId}>().agentId` 比对 `agent.name.split("/").pop()`);行内 Chat/Commands action 改为 `navigate(\`/agents/${agentId}/chat\`)` / `navigate(\`/agents/${agentId}/commands\`)`;Delete 保留(打开本文件内 delete AlertDialog)。

### 5.3 新 `frontend/src/app/layouts/agent-detail-layout.tsx`

已实现;落地后 tab 骨架又抽成了共享组件 `frontend/src/app/layouts/detail-tabs-layout.tsx`(`DetailTabsLayout`:URL 推导 activeTab、`resolvePath(handle)` 导航、`gate` 控制 trigger 显隐、`tabsTrailing`/`footer` 插槽)。`AgentDetailLayout` 现在只负责拉取 `getAgent` 以决定 workspace tab 的 `canEdit` gate,并声明六个 tab(profile/commands/reminders/chat/mcp/workspace);由于 Members 左栏已承载身份与连接状态,设计稿里的"返回按钮 + 标题 + ConnectionBadge 顶栏"没有保留(移动端由全局 MobileHeader 处理返回)。

### 5.4 新 `frontend/src/pages/dashboard/agent-profile.tsx`(`AgentProfilePage`)

已实现。原 Dialog 只读信息 + 原 Sheet 可编辑配置 + token 操作全部搬来,其中 ACP 编辑部分后续改为复用 `frontend/src/components/agent/acp-config-editor.tsx`(provider/model/executable/args/env/allow-env,配 `useAcpConfigDraft` 本地草稿);Rotate/Revoke 与 bootstrap token Dialog 仍在 profile 页内。注意 `getAgent` 无缓存(为避免跨用户读到过期 `canEdit`),profile 页在本地 state 持有完整 Agent 并在变更后重取。

### 5.5 新 `frontend/src/pages/dashboard/agent-chat.tsx`(`AgentChatPage`)

`useParams` 取 `agentId` → `agentName`;`useEffect` mount 与 `agentName` 变化时调 `fetchChannelsForAgent(agentName)`;从 store 读 `agentChannelsByAgent[agentName]` 与 `agentChannelsLoading`。渲染:loading 占位(`common.loading`)/ 空态(`agent.chat-empty`)/ channel 列表。已实现的走的是设计稿的兜底分支:`ConversationRow` 未从 `conversation-list.tsx` 导出(模块内私有 memo 组件),`agent-chat.tsx` 自渲染轻量行(title、type 图标(DM/Hash/Users)、memberCount),点击行 → `navigate(\`/${conv.name.split("/").pop()}\`)`。v1 不轮询(channel 名单稳定)。

### 5.6 命令 tab — 不改

`CommandListPage` 仍通过嵌套 `commands` 路由在 tab 内渲染;确认其仍从 `useParams` 读 `agentId`(原本就在 `agents/:agentId/commands` 下,行为不变)。

---

## Phase 6 — Cleanup

- 删除 `agents.tsx` 中的详情 Dialog、ACP Sheet、rotate/revoke AlertDialog 及其本地 state;保留 delete AlertDialog 与 token Dialog。(已执行;此后整个页面又被 Members 页取代,`agents.tsx` 仅存 `agentLifecycle`/`lifecycleLabel`。)
- 删除旧 agent-workspace 布局文件及其在 `dashboard.tsx` 中对应 import。(已执行。)
- i18n(`frontend/src/locales/en-US.json` / `zh-CN.json`):已新增 `agent.tab-profile` / `agent.tab-commands` / `agent.tab-chat` / `agent.chat-empty`(tab 键还随演进加了 `agent.tab-reminders` / `agent.tab-mcp` / `agent.tab-workspace`);空态提示最终落在 `members.no-selection`(空态归 Members 页),设计中设想的 `agent.no-selection` / `agent.profile.section-*` 未单独建键。

---

## 关键复用点

- `convertToV1Conversation`(`backend/manager/api/v1/channel_convert.go:19`)— 复用,勿重写。
- `ListUserConversationsWithUnread`(`backend/manager/store/conversation.go:632`)— 新 store 函数的模板。
- `MemberTypeAgent=2` 常量(`backend/manager/store/conversation_member.go:16`)。
- `common.GetAgentResourceID`(`backend/common/resource_name.go`)— 解析 `agents/{id}`。
- `parseLimitAndOffset` / `limitPlusOne` 分页模式(`backend/manager/api/v1/common.go:219`)。
- IAM 注解 `(laelia.v1.auth_method)=IAM` + `(laelia.v1.permission)="laelia.agents.get"` — 拦截器自动鉴权(`backend/manager/api/v1/iam.go` `authorize`,权限本体在 `backend/common/permission`)。
- `Tabs/TabsList/TabsTrigger/TabsPanel`(`frontend/src/components/ui/tabs.tsx`,value 驱动受控)。
- `agentResourceName`(`frontend/src/lib/resource.ts:5`)与 `resolvePath`(`frontend/src/router/route-index.ts`)。
- `ConversationRow` 渲染模式(`frontend/src/components/chat/conversation-list.tsx`,未导出,仅作样式参考)。
- agent store 现有 actions(`fetchAgents/getAgent/updateAgentACPConfig/refreshAgentProviders/rotateAgentToken/revokeAgentToken/deleteAgent`),profile/chat tab 直接调用。

---

## Verification

后端:
- `gofmt -w` 改动文件;`golangci-lint run --allow-parallel-runners`(反复跑至无 issue)。
- `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`。
- `go test ./backend/manager/...`(为 `ListAgentConversations` 加一个 store 测试,镜像已有 `ListUserConversationsWithUnread` 测试若存在)。
- `buf format -w proto && buf lint proto && cd proto && buf generate`。

前端:
- `pnpm --dir frontend biome:check`(format + lint + import 排序)。
- `pnpm --dir frontend type-check`。
- `pnpm --dir frontend lint`。
- `pnpm --dir frontend test`。

端到端(用 `run`/`verify` skill 启动 manager + frontend dev;以下 URL 已按落地后的 Members 结构更新):
1. 打开 `/members` → Agents 分组列出 agent,点击行 → URL 变 `/members/agents/:id`,profile tab 展示 identity 网格 + ACP 编辑器。
2. 编辑 ACP(provider/model/env)→ 保存 → 刷新页面 → 值仍在(走 `UpdateAgentACPConfig` 往返)。
3. Rotate token → token Dialog 展示新 bootstrap token;Revoke → 状态正确。
4. 通过某 channel 的 add-member UI 把 agent 加入 → 访问 `/members/agents/:id/chat` → 该 channel 出现;点击 → 跳 `/chat/:conversationId`。
5. `/members/agents/:id/commands` → `CommandListPage` 正常;打开命令 → `/members/agents/:id/commands/:commandId` 在 tab 内渲染。
6. 深链:新标签页直开 `/members/agents/:id/chat` → tab 正确激活;后退键遍历 `chat → commands → profile → /members`;任意 URL 刷新状态保留;旧 `/agents/:id/**` 链接重定向到对应 `/members/agents/:id/**`。
7. 左栏目录在所有 `members/agents/:id/*` 状态下始终可见,选中行高亮(数据来自 members 名单的静默刷新,不再有旧的 3s 轮询)。

## Risks

- **Proto 循环导入**:`command.proto` 已 import `agent.proto`,反过来 import 会成环。本方案把 RPC 放 `CommandService` 规避(已按此落地,且未加 HTTP 注解)。
- **`member_id` 编码**:agent 的 `conversation_member_meta.member_id` 存的是 resourceID 字符串(与 `findDirectConversation` 一致),与请求解出的 resourceID 直接可比,无需 int↔string。
- **Tab vs 路由冲突**:用字面 `commands`/`chat`/index-profile,不引入 `:tab`,避免与 `commands/:commandId` 冲突。
- **跨嵌套路由的高亮**:父 layout 持续挂载,选中行高亮在子路由切换时保留(现为 Members 页用 `useMatch` 读子路由参数)。
- **handler 鉴权**:必须靠 proto 注解声明 `laelia.agents.get`,否则任意登录用户可枚举任意 agent 的 channel;权限在 `backend/common/permission` 集中注册(落地后 handler 再加了一层 reviewAll 可见性收窄,见 Phase 1.3)。

## 实现现状(2026-09-06)

方案主体已落地,且此后形态继续演进。与当前代码的对应关系:

- **后端**:`proto/v1/v1/command.proto` 的 `CommandService.ListChannelsForAgent`(仅 IAM 注解,无 HTTP 路由);store 查询 `ListAgentConversations`(`backend/manager/store/conversation.go:712`,带 `viewer *ConversationMemberFilter` 可见性过滤,unread 恒为 0);handler 在 `backend/manager/api/v1/channel.go:130`(非 reviewAll 调用者只看到自己也是成员的 channel)。
- **前端 store**:`frontend/src/stores/channel.ts` 的 channel slice 内定义 `agentChannelsByAgent`/`agentChannelsLoading`/`fetchChannelsForAgent`(`channel.ts:281`;slice 接口就放在 `channel.ts`,由 `stores/types.ts` 引入,未按设计稿放 `types.ts`)。
- **路由**:agent 详情树挂在 `/members/agents/:agentId`(`frontend/src/router/routes/dashboard.tsx` 的 `members` 分支),index → `AGENT_ROUTE_PROFILE`,子路由含 `commands`、`commands/:commandId`、`reminders`、`reminders/:reminderId`、`chat`、`mcp`、`workspace`;旧 `/agents` 仅剩重定向(index → `/members`,`:agentId/*` → `/members/agents/:agentId/**`)。
- **布局**:`AgentDetailLayout`(`frontend/src/app/layouts/agent-detail-layout.tsx`)基于共享 `DetailTabsLayout`(`detail-tabs-layout.tsx`)渲染六个 tab;Members 目录页在 `frontend/src/pages/dashboard/members.tsx`(agents/humans/channels 可折叠分组 + 搜索,`useMatch` 高亮选中行)。
- **页面**:`agent-profile.tsx`、`agent-chat.tsx`、`agent-mcp.tsx`、`agent-workspace.tsx`、`command-list.tsx`、`command-detail.tsx`、`reminder-list.tsx`、`reminder-detail.tsx`;原两栏 `AgentsPage` 已不存在(`agents.tsx` 只剩生命周期分类辅助)。