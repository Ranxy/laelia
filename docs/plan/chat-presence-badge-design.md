# Chat 在线状态角标(presence badge)设计与实现方案

## Context

chat 页面目前完全不展示任何参与者的在线状态:用户无法在发起对话前感知 agent/human 是否在线,也无法判断一条 DM 发出去后对方"现在"是否能看到。

现状盘点:

- **Agent 在线状态:后端已有权威信号。** agent 的 liveness 由 machine 连接模型维护——dispatcher 中该 agent 存在活跃 AgentChannel 即 `connected`,叠加 deleted/enabled 生命周期后由 `computeConnectionState`(`backend/manager/api/v1/agent_convert.go`)导出 `AgentStatus.ConnectionState`(ONLINE/OFFLINE/ERROR/KICKED/STOPPED),经 `ListAgents` 暴露给前端。前端 `fetchAgents` 已携带该字段,chat 页目前只在每会话首次进入时拉一次、不刷新。
- **Human 在线状态:后端完全没有。** 用户认证是无状态 JWT(无 session 表),web 客户端没有任何心跳/活跃度上报。唯一沾边的字段是 `UserProfile.last_login_time`,仅在登录那一刻写入,不能反映真实在线。
- **chat 页头像出现的位置**:左侧会话列表 DM 行(`conversation-list.tsx`,peer 头像)、聊天窗口头部(`chat-conversation.tsx`,目前只有通用的 Bot/User/Hash 图标,不是真实头像)、消息行、thread 面板、频道成员面板。

本方案新增一套轻量 human presence 机制(前端心跳 + manager 内存 registry),agent 侧完全复用现有连接状态;UI 上在会话列表 DM 行头像与聊天窗口头部头像的右下角渲染标准聊天软件样式的绿色圆点角标。

## 关键设计决策(已与用户确认)

1. **角标位置**:左侧会话列表 DM 行头像 + 聊天窗口头部(打开会话的对方头像)。消息行头像不加角标,与微信/Slack 的信息密度一致。
2. **human 在线判定**:新增轻量心跳机制——前端每 30s 调一次 `SyncPresence`,顺带上报自己的活跃并批量查询关注名单的在线状态;后端记录 `last_active` 内存时间戳。
3. **离线阈值**:90s TTL(落在用户选择的 60~120s 区间)。心跳间隔 30s → 允许连续丢 2 拍;浏览器把后台 tab 的 timer 限流到 ≥60s 时仍留在窗口内,不会闪断。
4. **agent 在线判定**:仅 `AgentStatus.ConnectionState === ONLINE` 显示绿标(复用 Agents 管理页的判定),OFFLINE/STOPPED/ERROR/KICKED 一律不算在线。
5. **agent presence 不走新 RPC**:agent 在线状态由 ChatLayout 每 30s 一次的 silent `fetchAgents` 刷新(整册返回,复用已有的 `agentsEqual` 跳过逻辑),避免后端在 presence handler 里按 agent 逐个重查 dispatcher liveness、重复 `computeConnectionState` 的生命周期规则。
6. **offline 不显示灰色角标**:只在在线时显示绿点;离线头像保持原样(需求只要求在线态可感知)。
7. **频道(type 2)不显示角标**:频道头像位置是 # 图标,成员众多、语义不明,超出本期范围。

## 数据流

```
[浏览器] ──每 30s──▶ POST SyncPresence(names: [DM 里的人类 peer, ...])
                       │ manager: presence.Touch(当前调用者)          ← 心跳,无法代他人上报
                       │          for name: users/ → registry TTL 判定
                       ◀────────── { presences: [{name, online}, ...] }
[ChatLayout 30s tick] ──同时──▶ silent fetchAgents(整册刷新 agent ONLINE 状态)
[store] onlineUsers / agents(connectionState) 更新
[UI] Avatar online → 右下角绿点
```

- 调用者身份来自 auth 拦截器注入的 ctx(`GetUserFromContext` / `GetAgentFromContext`),请求体里没有任何"我是谁"字段,无法伪造他人在线。
- agent 调用 SyncPresence 也会 Touch 自己(注册表只记不读,agent 在线不由它消费),为将来 agent 侧 UX 留口子。

## Proto (`proto/v1/v1/command.proto`)

放在 CommandService(chat 域,前端 chat 页已持有 `commandServiceClient`),AIP 自定义方法命名:

```proto
// SyncPresence records the calling principal's presence heartbeat and returns
// the current online state of the requested principals. The caller's own
// presence is updated as a side effect; presence for other principals cannot
// be reported, only queried. Any authenticated principal (user or agent) may
// call it. Human presence is a windowed heartbeat: a user is online while its
// last heartbeat is within the manager's presence TTL. Agent presence is NOT
// answered here (always online=false) — use AgentService.ListAgents, whose
// status.state is authoritative.
message SyncPresenceRequest {
  // names are the principals to query ("users/<handle>" or "agents/<id>"),
  // capped at 200 entries server-side; duplicates are ignored. Agents are
  // accepted but always answered offline.
  repeated string names = 1;
}

// Presence is one principal's online state.
message Presence {
  string name = 1;
  bool online = 2;
}

message SyncPresenceResponse {
  repeated Presence presences = 1;
}
```

```proto
rpc SyncPresence(SyncPresenceRequest) returns (SyncPresenceResponse) {}
```

- 无特殊注解:默认要求已认证(任意 user/agent),无 permission 检查、不审计。在线状态对工作区内所有已认证成员可见(与 ListUsers 的可见性一致,Slack 同理)。
- `buf format -w proto && buf lint proto && cd proto && buf generate`。

## 后端

### 新组件 `backend/manager/component/presence/presence.go`

内存 registry,与 `roomhub` 同级、同约束(单进程;包注释注明多实例部署需要共享存储后端,如 Postgres LISTEN/NOTIFY,同接口替换):

```go
package presence

// DefaultTTL 是心跳窗口:最后一次心跳超过该时长视为离线。
const DefaultTTL = 90 * time.Second

type Registry struct {
    mu       sync.Mutex
    lastSeen map[string]time.Time // key: principal resource name ("users/<handle>")
}

func New() *Registry
func (r *Registry) Touch(name string, now time.Time)              // upsert
func (r *Registry) Online(names []string, now time.Time, ttl time.Duration) map[string]bool
```

设计取舍:

- **不持久化**:重启即全员离线 ≤30s(首次心跳恢复),符合预期语义;零迁移、零表。
- **不做后台清扫**:条目数以工作区 principal 总数为上界(每条 ~50B),过期条目只是恒为 offline 的死键,不值得为它加 goroutine/生命周期管理。
- `Touch` 只在 handler 里调用;`Online` 纯读。time 由调用方注入便于测试。

### Handler `backend/manager/api/v1/command_presence.go`

```go
func (s *CommandService) SyncPresence(ctx, req) (resp, error) {
    if user, ok := GetUserFromContext(ctx); ok {
        s.presence.Touch(user.Name, time.Now())
    } else if agent, ok := GetAgentFromContext(ctx); ok {
        s.presence.Touch(agent.Name, time.Now())
    }
    names := dedupe(req.Msg.Names) // cap 200,超出报 INVALID_ARGUMENT
    online := s.presence.Online(names, time.Now(), presence.DefaultTTL)
    // 只回显合法请求的名字;registry 未知的名字(从未心跳)→ offline
}
```

- `NewCommandService` 增加 `presence *presence.Registry` 依赖,`grpc_routes.go` 中 `presence.New()` 构造并注入(与 roomhub 同处)。
- 键即 `UserMessage.Name`(资源名 "users/<handle>"),与前端查询用的 `conv.peer`、`channel.peer` 同一形态,零转换。

## 前端

### Presence store slice `frontend/src/stores/presence.ts`(新)

```ts
export interface PresenceSlice {
  // onlineUsers 跟踪 human 的在线状态,key 为 "users/<handle>"。
  // agent 不在这里——agent 在线来自 agents slice 的 connectionState。
  onlineUsers: Record<string, boolean>;
  syncPresence: (names: string[]) => Promise<void>;
}
```

- `syncPresence`:去重、过滤非 `users/` 前缀;调 `commandServiceClient.syncPresence`;结果与现有 `onlineUsers` 浅比较,无变化不 `set`(避免无谓 re-render,与 `agentsEqual` 同思路);失败静默(下个周期重试,角标保持上一次状态)。
- **空名单也照常发请求**:服务端从 auth ctx 记录调用者心跳,空名单的调用本身就是心跳——保证没有任何 DM 的用户也能被他人看到在线(实现阶段修正,设计初稿的"空名单直接返回"会漏掉这类用户的心跳)。
- 挂入 `stores/index.ts` 与 `types.ts` 的 `AppStoreState`;`reset()` 经 `getInitialState()` 自动清理,无需特判。

### 心跳与刷新调度 `composables/use-presence-heartbeat.ts`(新,挂载于 dashboard-layout)

心跳 tick 挂载在 **dashboard 布局层**而非 chat 路由(实现阶段的设计修正):"在线 = 用户开着 laelia"是标准聊天软件语义;只从 chat 页心跳会让正在浏览其他页面的用户在 90s 后错误显示离线。角标 UI 仍只存在于 chat 组件,数据都在 store 中:

```ts
const PRESENCE_POLL_INTERVAL_MS = 30000;

const tick = () => {
  const state = useAppStore.getState();
  const names = new Set<string>();
  for (const c of state.channels) {
    if (c.peer) names.add(c.peer); // 左栏 DM peers;slice 内过滤出 users/
  }
  void state.syncPresence([...names]); // 心跳 + 查询 human peers
  void state.fetchAgents({ pageSize: 100 }, { silent: true }); // 刷新 agent ONLINE 状态
};
```

- 挂载即 tick 一次(角标无需等待首个 30s 周期);`visibilitychange` 在 tab 重新可见时立即补 tick(后台被浏览器深度节流后,切回瞬间消除陈旧绿点)。
- interval 保持 30s 不做隐藏降频:浏览器对隐藏 tab 的 timer 下限是 1/min,60s < 90s TTL,普通后台仍在线;被 Chrome intensive throttling 降到更低频时,用户显示离线——这符合"挂后台很久 ≈ away"的聊天软件语义,切回即恢复。

### Avatar 角标 `frontend/src/components/chat/avatar.tsx`

`Avatar` 增加可选 `online?: boolean`(`undefined` = 不显示角标,所有现有调用点零改动;`true` = 绿点;`false` = 无点):

```tsx
<span className="relative inline-flex shrink-0">
  {/* 原 img / pixel-identicon 渲染不变,fallback 逻辑不变 */}
  {online && (
    <span
      className="absolute right-0 bottom-0 size-2.5 rounded-full bg-success ring-2 ring-background"
      aria-hidden
    />
  )}
</span>
```

- 颜色用语义 token `bg-success`(`--color-success` #16a34a,tailwind.css 已定义),禁止裸色值;`ring-2 ring-background` 提供标准聊天软件的"描边留白"效果。
- 点径 size-2.5(10px)对 size-8(32px)头像 ≈ 31%,即常见比例;size-6 及以下头像用 size-2。
- 组件内部 absolute 定位,不涉及 overlay 分层策略(仅组件内部组合)。
- i18n:点上加 `title={t("chat.presence-online")}`,新增 en/zh 文案(悬停提示 + a11y)。

### 接入点 1:会话列表 `conversation-list.tsx`

`ConversationRow` 已是 memo + 纯 primitive props,新增 primitive `peerOnline?: boolean`:

- 父组件 map 行时计算:
  - agent DM(type 1,peer 为 `agents/<id>`):`agents.some(a => a.name === peer && a.status?.state === ONLINE)`(父组件 useMemo 建 `Set<agentName>`);
  - user DM(type 4):`onlineUsers[peer] === true`;
  - 频道行:`undefined`(不渲染)。
- 在线状态变化时仅对应行 re-render(memo bail-out 语义保持)。

### 接入点 2:聊天窗口头部 `chat-conversation.tsx`

头部头像区(isDm/isAgentDm/isUserDm 且 `channel.peer` 存在时)把通用 Bot/User 图标替换为 **peer 真实头像 + 角标**:

- `useAvatar(peer ? \`${peer}/avatar\` : undefined)` 取头像(与左栏同模式,hook 无条件调用);
- `online` 派生逻辑同上(agent peer 走 agents slice,user peer 走 presence slice);
- `channel.peer` 为空(异常情况)时回退现有图标;
- 频道头部保持 # 图标不变;
- agent↔agent DM(type 3,admin 视角)peer 为 agent,同样显示真实头像 + 在线角标。

### 接入点 3(追加):`/members` 目录页 `pages/dashboard/members.tsx`

与 agents 页保持一致的**字符串**式展示(非绿点):

- human 行右侧的 "User" 类型文案替换为 presence 徽标:在线 → `Badge variant="success"` "Online"(`chat.presence-online`),离线 → `Badge variant="secondary"` "Offline"(`chat.presence-offline`),与 agents 行的 ConnectionBadge 展示语言完全一致;
- agent 行保持原 ConnectionBadge 不变(其 `connectionState` 已由心跳 tick 的 silent fetchAgents 每 30s 刷新);
- 数据:human 行 `onlineUsers[member.name] === true`(`member.name` 即 `users/<handle>`)。

### 接入点 4(追加):channel 成员抽屉 `components/chat/channel-members-panel.tsx`

与 chat 列表一致的**绿点**式展示:

- `ChannelMemberRow` 新增 `online` prop → `Avatar online`;
- agent 成员(`memberType === 2`):`agents` slice 建 `Set` 查 `agents/<memberId>`;human 成员查 `onlineUsers["users/<memberId>"] === true`(memberId 即 handle);
- 面板同时被 chat 成员 Sheet 与 channel 详情页复用,两处自动生效。

### 心跳查询名单的扩展

tick 收集的 names 从"左栏 DM peers"扩展为"所有已加载、UI 会展示 presence 的 human":

1. `channels` 的 DM peers(左栏 + DM 头部);
2. `users` 名册(members/settings 页加载后,`/members` 页所有 human 行);
3. `channelMembersByConv` 各已加载 roster 的 user 成员(成员抽屉/channel 详情页)。

未加载的名册自然不查询,对应界面在下个 tick 后出现角标数据;名单仍由 slice 统一去重并按 200 上限截断。

## 测试

后端:

- `component/presence/presence_test.go`:Touch 后 Online 为 true;TTL 边界(now、now-TTL、now-TTL-1ns);未触碰名字 → false;Touch 覆盖更新。
- `api/v1/command_presence_test.go`(沿用包内既有 ctx 注入测试模式):caller 心跳被记录;查询返回 TTL 内/外用户;`agents/` 前缀恒 false;>200 名单 INVALID_ARGUMENT;空名单 OK。

前端:

- `stores/presence.test.ts`:合并写入、无变化不触发 set、失败保活、非 users/ 前缀被过滤。
- `conversation-list.test.tsx`:在线 peer 的 DM 行渲染绿点(testid `presence-badge`),离线/频道行不渲染。
- `lib/presence.test.ts`:isAgentOnline 仅 ONLINE 为 true;peerPresenceOnline 的 agent/user/无 peer 分支。
- `members.test.tsx`:human 行按心跳显示 Online/Offline 字符串徽标;agent 行仍走 ConnectionBadge。
- `channel-members-panel.test.tsx`:在线 human/agent 头像渲染绿点,离线不渲染。

## 验证清单(按 AGENTS.md)

```bash
# proto
buf format -w proto && buf lint proto && cd proto && buf generate

# backend
gofmt -w <changed files>
golangci-lint run --allow-parallel-runners
go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go

# frontend
pnpm --dir frontend biome:check
pnpm --dir frontend lint --fix && pnpm --dir frontend biome:lint
pnpm --dir frontend type-check
pnpm --dir frontend test
node frontend/scripts/check-react-layering.mjs
```

## 改动文件清单

| 层 | 文件 | 动作 |
|---|---|---|
| proto | `proto/v1/v1/command.proto` | +SyncPresence RPC/messages |
| proto | `frontend/src/types/proto-es/v1/command_pb.*`、`backend/generated-go/v1/*` | buf generate 生成 |
| 后端 | `backend/manager/component/presence/presence.go` | 新增 registry |
| 后端 | `backend/manager/component/presence/presence_test.go` | 新增 |
| 后端 | `backend/manager/api/v1/command_presence.go` | 新增 handler |
| 后端 | `backend/manager/api/v1/command_presence_test.go` | 新增 |
| 后端 | `backend/manager/api/v1/command.go` | NewCommandService 注入 presence |
| 后端 | `backend/manager/server/grpc_routes.go` | 构造并传入 registry |
| 前端 | `frontend/src/stores/presence.ts` + `presence.test.ts` | 新增 slice |
| 前端 | `frontend/src/stores/types.ts`、`frontend/src/stores/index.ts` | 挂载 slice |
| 前端 | `frontend/src/components/chat/avatar.tsx` | +online prop/角标 |
| 前端 | `frontend/src/components/chat/conversation-list.tsx` | DM 行传 online |
| 前端 | `frontend/src/pages/dashboard/chat-conversation.tsx` | 头部 peer 头像 + 角标 |
| 前端 | `frontend/src/pages/dashboard/members.tsx` | human 行 Online/Offline 字符串徽标(追加) |
| 前端 | `frontend/src/components/chat/channel-members-panel.tsx` | 成员抽屉行头像绿点(追加) |
| 前端 | `frontend/src/lib/presence.ts` + `presence.test.ts` | 在线判定共享 helper |
| 前端 | `frontend/src/composables/use-presence-heartbeat.ts` | 心跳 tick(挂载于 dashboard-layout;names 扩展至全部已加载 human) |
| 前端 | `frontend/src/app/layouts/dashboard-layout.tsx` | 挂载 usePresenceHeartbeat |
| 前端 | `frontend/src/locales/en-US.json`、`zh-CN.json` | +chat.presence-online / chat.presence-offline |

## 已知边界

- **单进程约束**:presence registry 与 roomhub 一样是进程内存态;manager 目前单实例部署(机器/agent 均连单点),未来多实例需把 registry 换成共享存储(PG LISTEN/NOTIFY 或表),接口不变。
- **manager 重启**:全员离线 ≤30s(首个心跳周期恢复),可接受。
- **深度后台 tab**:Chrome intensive throttling 下心跳可能被降到分钟级以上,用户会转为离线,切回前台由 visibilitychange 立即恢复——与聊天软件"挂机变离线"的直觉一致。
- **service account / system bot**:从不心跳,恒为离线,符合语义。
- **agent 未运行机器时**:STOPPED/OFFLINE 不显示角标,与 Agents 管理页判定一致。