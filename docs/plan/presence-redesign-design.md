# Human 在线状态系统重构设计(presence redesign)

> 状态:已按本方案实现完毕。**部分取代** [`chat-presence-badge-design.md`](./chat-presence-badge-design.md):human presence 的机制部分(SyncPresence RPC、内存 registry、心跳 hook 的名单收集与合并缓存)由本文彻底取代,旧文档中的相关实现细节已删除;该文档的角标 UI 设计、agent 在线判定与接入点位置/形态仍然有效并保留在其文中。两文档冲突时,以本文为准。

## Context

当前 human 在线状态系统(`chat-presence-badge-design.md` 落地版)是:前端在 dashboard 布局挂一个 30s 心跳,每次心跳调 `CommandService.SyncPresence`,请求体携带"Zustand store 里恰好已加载的 human 名单"去批量查询;后端用进程内存 registry(`backend/manager/component/presence`)记录每个 principal 的最后心跳时间,90s TTL 滑动窗口判定在线。Agent 在线走独立通道(`ListAgents` 的 `ConnectionState`)。

### 症状:刷新页面后所有 human 显示离线

根因是时序竞态,链条如下:

1. 页面刷新后,TanStack Query 缓存与 Zustand store 全部为空;
2. `DashboardLayout` 挂载,`usePresenceHeartbeat` 的 useQuery 因 `staleTime: 0` 立即发出第一拍;
3. 第一拍的 `collectWantedNames()` **同步读取 store**——此时 `fetchUsers`/`fetchChannels` 尚未返回,名单为空;
4. 服务端只回显请求过的名字(调用者自己的心跳被记录但不回显),于是第一拍返回空集;
5. store 填充后**没有任何机制触发重新查询**(query 不依赖 store 内容,消费方又是 `enabled: false` 的只读视图),只能等 30s 后的下一个 interval beat;
6. 结果:刷新后最长 30s 内 presence map 为空,而 `/members` 页把"无数据"渲染成明确的 **Offline 徽章**(`onlineUsers[member.name] === true ? Online : Offline`——缺失数据与真离线不可区分),chat 页所有绿点消失。

### 系统性缺陷清单(重构要解决的)

| # | 缺陷 | 后果 |
|---|------|------|
| 1 | 后端 registry 纯内存、不持久化(`component/presence/presence.go`) | manager 重启全员离线;无法多实例部署;无法支持 last_seen |
| 2 | 心跳(写)与查询(读)耦合在同一个 RPC,且**查询集合由客户端 UI 恰好加载了什么决定** | 在线数据的正确性绑定在无关的 UI 状态上——上面刷新 bug 的土壤;新开 DM/名册加载后新对象最长 30s 显示离线 |
| 3 | 纯拉模型、无变更推送、每客户端每 30s 全量轮询 | O(客户端×已加载用户) 的查询负载;状态传播延迟 ≤30s |
| 4 | 前端缓存"只合并不淘汰"(`presenceQueryFn` merge 逻辑) | 名册卸载后学到 last-known 状态,**可能永久显示假"在线"** |
| 5 | 心跳 query 与消费 query 用 `enabled: false` 强耦合 | 心跳 hook 一旦卸载,所有消费面数据永久冻结 |
| 6 | `Presence` proto 无时间戳、无 multi-device 概念;agent 的 Touch 是死代码(`usersOnly` 过滤) | 表达能力缺失,语义混乱 |
| 7 | `/members` 页把缺失数据渲染成明确 "Offline" | 放大了 1-5 的所有毛病的可见性 |

### 已确认的关键决策(与用户对齐)

1. **状态落 Postgres**——不保留内存 registry。重启不丢、天然支持多实例、免费获得 last_seen;
2. **保留轮询传输**——不引入 WebSocket/SSE,修正确拉模型即可满足 presence 语义;接口与数据模型预留推送演进路径;
3. **last_seen 一起做**——离线时展示"最后在线 x 分钟前";
4. **agent 不纳入**——agent 在线语义是 machine 连接级真相(`ConnectionState`),继续走 `ListAgents`;新系统只管 human。

## 目标与非目标

**目标**

- 刷新页面后首屏即拿到正确的 presence 数据(与 store 加载时序无关);
- manager 重启不清空在线状态;`last_seen` 持久化并可展示;
- 前端任何界面"无数据"与"离线"可区分,不再出现假离线/假在线;
- 心跳写路径与查询读路径解耦,互为故障隔离;
- 删除 `collectWantedNames`、merge-forever 缓存、`enabled: false` 反转等结构性负担。

**非目标**

- 亚秒级实时推送(见"演进路径");
- agent / service account / system bot 的在线状态(维持现状);
- 多设备/多会话维度的 presence(单 boolean + last_seen 足够,多设备心跳天然归并为一行);
- 从"打开 laelia"之外推断在线(如从邮件点击、API token 调用推断)。

## 总体架构

```
[浏览器 dashboard 布局]  usePresenceHeartbeat → usePolling(30s, fireOnMount)
      │                      └─► PresenceService.SendHeartbeat        (纯写,身份来自 auth ctx)
      │                                                    │
      │                                                    ▼
      │                              Postgres user_presence 表 (handle PK, last_seen_at)
      │                                                    ▲
[任意挂载 presence 的界面] usePresenceMap (useQuery, 30s refetchInterval)
                             └─► PresenceService.ListPresence          (纯读,服务端定义全量集合)
                                       └─ online = now - last_seen_at ≤ 90s
[消费面] useUserPresence(name) → online | offline | unknown(首帧加载中)
         离线且有 last_seen_at → "最后在线 x 分钟前"
```

核心变化:**写路径与读路径拆成两个独立 RPC、两条独立循环**;读集合由服务端定义(全量),与客户端 UI 状态彻底解耦。

## 数据库

### 新表 `user_presence`

```sql
CREATE TABLE IF NOT EXISTS user_presence (
    handle       text PRIMARY KEY,
    -- last_seen_at 是该用户最后一次 Web 心跳的时间;在线判定 = now - last_seen_at ≤ TTL
    last_seen_at timestamptz NOT NULL
);
```

设计取舍:

- **独立窄表而非 `principal` 加列**:presence 是易失运行态,不是档案数据;窄行 upsert 的 MVCC/WAL 写放大远小于更新 `principal` 宽行(含多个 jsonb)。
- **不建 FK**:`handle` 对应 `principal.handle`(有唯一索引)但不引用。写路径免外键检查锁;软删/彻底删除用户的残留行只是恒为 offline 的死数据(行数 ≤ 历史用户总数,每行 ~40B),不值得为它加联锁清理。若日后想清理,在 principal 删除流程顺手 `DELETE` 即可。
- **不做后台清扫**:同上,行数有界。
- **只存 human**:agent / service account / system bot 永不写入此表。

### 迁移(双轨规则)

- `LATEST.sql` 追加上述幂等 DDL;
- 增量文件 `backend/manager/migration/migration/1.1/0030##user-presence.sql`:建表 + 数据回填——用 `principal.profile` 里的 `lastLoginTime`(protojson 输出,`profile->>'lastLoginTime'` 为 RFC3339 字符串)为存量用户播种 last_seen,让上线第一天 "最后在线" 就有意义:

```sql
INSERT INTO user_presence (handle, last_seen_at)
SELECT handle, (profile ->> 'lastLoginTime')::timestamptz
FROM principal
WHERE deleted = FALSE AND profile ? 'lastLoginTime'
ON CONFLICT (handle) DO NOTHING;
```

- `migration_test.go` 增加 schema 不变量守卫:`user_presence` 表存在且含 `handle`/`last_seen_at` 列。

## 后端

### Store 层:`backend/manager/store/presence.go`(新)

替换整个 `backend/manager/component/presence` 包(删除)。沿用 store 层既有模式(`*sql.DB` + `errors.Wrap`,时间由调用方注入便于测试):

```go
// PresenceTTL 是滑动在线窗口:最后一次心跳距今超过该时长即离线。
// 前端 30s 心跳 → 容忍丢两拍;浏览器后台节流到 1/min 的 tab 仍在窗口内。
const PresenceTTL = 90 * time.Second

// TouchPresence upsert 一次心跳。窄行主键 upsert,幂等。
func (s *Store) TouchPresence(ctx context.Context, handle string, now time.Time) error {
    // INSERT INTO user_presence (handle, last_seen_at) VALUES ($1, $2)
    // ON CONFLICT (handle) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at
}

// PresenceRow 是一条用户在线状态。
type PresenceRow struct {
    Handle     string
    LastSeenAt time.Time
}

// ListPresence 返回全部心跳历史行,并按 ttl 判定 online。
// 集合由服务端定义(全量),不接受客户端名单——这是与旧设计的本质区别。
func (s *Store) ListPresence(ctx context.Context, now time.Time, ttl time.Duration) ([]PresenceRow, error) {
    // SELECT handle, last_seen_at FROM user_presence
}

// PresenceOnline 是纯判定函数(供 handler 与测试复用)。
func PresenceOnline(lastSeen, now time.Time, ttl time.Duration) bool
```

- store 单测:`presence_test.go` 沿用包内 SQL 形状守卫模式——锁定 `ON CONFLICT (handle) DO UPDATE` 子句(upsert 幂等性是并发正确性的一部分)、`PresenceOnline` 的 TTL 边界(恰在 TTL 上限为 true,+1ns 为 false)。

### Proto:`proto/v1/v1/presence_service.proto`(新)

```proto
service PresenceService {
  // SendHeartbeat records the calling human user's presence heartbeat.
  // Identity comes from the auth context; a caller can never report
  // presence for someone else. Agent callers are answered OK as a no-op —
  // agents are not tracked here (their state lives in AgentService).
  rpc SendHeartbeat(SendHeartbeatRequest) returns (SendHeartbeatResponse) {}

  // ListPresence answers the presence of every tracked human user. The
  // set is defined by the server (bounded by workspace size), so clients
  // never tell the server what to query. A user absent from the response
  // has never sent a heartbeat (effectively offline).
  rpc ListPresence(ListPresenceRequest) returns (ListPresenceResponse) {}
}

message SendHeartbeatRequest {}
message SendHeartbeatResponse {}

message ListPresenceRequest {}

// Presence is one human user's online state.
message Presence {
  // users/<handle> resource name, same shape the frontend keys everywhere.
  string name = 1;
  // online is true while the user's last heartbeat is within the
  // manager's presence TTL (computed at query time).
  bool online = 2;
  // last_seen_at is the time of the last heartbeat; used by clients to
  // render "last seen" hints for offline users.
  google.protobuf.Timestamp last_seen_at = 3;
}

message ListPresenceResponse {
  repeated Presence presences = 1;
}
```

- 命名遵循 AIP 自定义方法 verb+noun;不涉及权限检查与审计(与 ListUsers 同等可见性:工作区内所有已认证成员可见,Slack 同理)。
- `buf format -w proto && buf lint proto && cd proto && buf generate`,重新生成 `backend/generated-go/` 与 `frontend/src/types/proto-es/`(新增 `presence_service_pb.*`)。

### API 层:`backend/manager/api/v1/presence_service.go`(新)

```go
func (s *PresenceService) SendHeartbeat(ctx, req) (resp, error) {
    if user, ok := GetUserFromContext(ctx); ok {
        return nil, s.store.TouchPresence(ctx, user.GetResourceID(), time.Now()) // "users/<handle>" → handle
    }
    return connect.NewResponse(&v1pb.SendHeartbeatResponse{}), nil // agent caller: no-op
}

func (s *PresenceService) ListPresence(ctx, req) (resp, error) {
    rows, err := s.store.ListPresence(ctx, time.Now(), store.PresenceTTL)
    // → []*v1pb.Presence{ name: "users/"+handle, online: PresenceOnline(...), lastSeenAt: ... }
}
```

- key 仍用 `principal.handle`(即 `UserMessage.GetResourceID()` 的 `users/<handle>` 形态),前端所有消费点的 key 零转换;
- 安全上限 `maxPresenceResults = 2000`:响应行数截断(自托管工作区远低于此;超限属于规模问题,届时加分页,见演进路径);
- **删除**:`command_presence.go` + `command_presence_test.go`、`component/presence/` 整包、`NewCommandService` 的 presence 参数、`grpc_routes.go` 中旧 registry 构造(换 `NewPresenceService(stores)`);proto 中 `CommandService.SyncPresence` + `SyncPresenceRequest/Response` 一并删除(前后端同二进制分发,无版本偏移,直接破坏性移除;已确认 `backend/agent/` 无任何 Go 调用方)。

## 前端

### 新 hook:`frontend/src/hooks/use-presence.ts`(替换 `use-presence-heartbeat.ts`,旧文件删除)

```ts
export const PRESENCE_QUERY_KEY = ["presence"] as const;
const PRESENCE_INTERVAL_MS = 30_000;

export type UserPresence = { online: boolean; lastSeenAt?: Date };

// —— 写路径:纯副作用,不是 query。挂在 dashboard 布局。——
export function usePresenceHeartbeat() {
  usePolling(() => {
    void presenceServiceClient.sendHeartbeat({}).catch(() => {}); // 失败静默,下一拍自然重试
  }, PRESENCE_INTERVAL_MS, { fireOnMount: true });
  // agent 角标的同频 fetchAgents 刷新保持不变。
}

// —— 读路径:全量替换式缓存,由消费面驱动。——
const presenceQueryFn = async (): Promise<Record<string, UserPresence>> => {
  const res = await presenceServiceClient.listPresence({});
  const next: Record<string, UserPresence> = {};
  for (const p of res.presences) {
    next[p.name] = { online: p.online, lastSeenAt: p.lastSeenAt ? new Date(...) : undefined };
  }
  return next; // 整表替换,绝不合并 → 假"在线"不可能存活
};

export function usePresenceMap() {
  return useQuery({
    queryKey: PRESENCE_QUERY_KEY,
    queryFn: presenceQueryFn,
    staleTime: PRESENCE_INTERVAL_MS,
    refetchInterval: PRESENCE_INTERVAL_MS,
    refetchIntervalInBackground: false,
    retry: false,
  });
}

// —— 单用户订阅原语:unknown / offline / online 三态。——
export function useUserPresence(name: string | undefined): UserPresence | undefined {
  const { data, isPending } = usePresenceMap();
  if (isPending) return undefined;            // 首帧加载中 → unknown(不渲染任何徽章)
  return data?.[name] ?? { online: false };   // 全量响应中的缺席 = 从未心跳 = 离线
}

// logout 清理保持不变。
registerCleanup(() => { queryClient.removeQueries({ queryKey: PRESENCE_QUERY_KEY }); });
```

设计要点:

1. **读集合不再依赖 store**:没有 `collectWantedNames`、没有 200 名单上限、没有"名册加载后等下一拍"。刷新后首拍直接返回全量正确数据——刷新 bug 这一类问题被结构性消除;
2. **整表替换**消灭 merge-forever;缺席即离线,旧缓存不可能存活超过一拍;
3. **读循环由消费面驱动**(多个消费面共享同一 query 实例与 interval):没有徽章的界面不发读请求;新界面挂载时若数据已 stale(`staleTime` 30s)立即 refetch——新开 DM/名册即时拿到数据,不用等下一拍;
4. **写循环不依赖任何界面**:dashboard 布局挂载即首拍(`fireOnMount`),纯写 RPC 失败不影响读;
5. `usePolling` 增加 `fireOnMount?: boolean` 选项(挂载即触发一次;可见性恢复仍立即补拍)——这是它第一个使用场景,心跳需要挂载即上报,否则新登录用户要等 30s 才对他人可见。

### last_seen 文案:`frontend/src/lib/presence.ts` 扩展

```ts
// formatLastSeen:离线且有 last_seen 时的相对时间文案(en/zh 双语,走 i18n)。
// < 1min → just-now;< 60min → minutes({{count}});< 24h → hours;< 7d → days;
// 更久 → toLocaleDateString(按当前 locale)。
export function formatLastSeen(lastSeenAt: Date, t: TFunction): string
```

i18n 新键(en-US/zh-CN 同步,复数走 react-i18next `count` 机制,`pnpm --dir frontend sort:i18n` 收尾):

- `chat.presence-last-seen`:`"Last seen {{time}}"` / `"最后在线 {{time}}"`
- `chat.presence-last-just-now`:`"just now"` / `"刚刚"`
- `chat.presence-last-minutes`:`"{{count}} minute(s) ago"` / `"{{count}} 分钟前"`
- `chat.presence-last-hours` / `chat.presence-last-days` 同理
- 既有 `chat.presence-online` / `chat.presence-offline` 保留。

### 接入点(角标 UI 全部保留,数据源切换)

| 界面 | 现状 | 重构后 |
|---|---|---|
| `/members` human 行 | 无数据也渲染 Offline 徽章 | `useUserPresence`:unknown → **暂不渲染徽章**(首帧亚秒级);online → success "Online";offline → secondary "Offline",`title={formatLastSeen(...)}` |
| 会话列表 DM 行 / 频道成员抽屉 / DM 头像 | `onlineUsers[peer] === true` 才有绿点 | `useUserPresence(...)?.online`;unknown 与 offline 均不渲染绿点(行为不变,数据更准) |
| DM 聊天头部 | 在线 title "Online" | 在线不变;**离线时 title 改为 `formatLastSeen(...)`**(无 last_seen 数据则维持"离线") |
| agent 行(全部界面) | ConnectionBadge / ConnectionState | 不变 |
| `lib/presence.ts` 的 `peerPresenceOnline` | user 分支读旧 map | user 分支改走 `useUserPresence`;agent 分支维持 agents 名册判定 |

## 测试计划

**后端**(hermetic,无需真实 PG):

- `store/presence_test.go`:SQL 形状守卫(`ON CONFLICT (handle) DO UPDATE`);`PresenceOnline` TTL 边界;`ListPresence` 行映射。
- `api/v1/presence_service_test.go`:user ctx 心跳落库(依赖注入 fake store);agent ctx no-op 不写;ListPresence 的 name 拼装/online 判定/lastSeenAt 透传;超 2000 截断;无 caller 不报错。

**前端**(Vitest,与源文件同址):

- `hooks/use-presence.test.tsx`:首拍 fireOnMount;心跳失败静默不抛;presence map 整表替换(旧 key 被清除——针对 merge-forever 的回归测试);`useUserPresence` 三态(isPending → undefined;缺席 → offline;存在 → 透传);
- `lib/presence.test.ts`:`formatLastSeen` 各时间档边界;
- `members.test.tsx` / `conversation-list.test.tsx` / `channel-members-panel.test.tsx`:播种新缓存形状;新增 **unknown 不渲染徽章** 的用例(替代旧"空 map = 全员 Offline"语义);
- `use-polling` 测试:`fireOnMount` 挂载即触发。

**迁移守卫**:`migration_test.go` 断言 `user_presence` 表结构(触发迁移门禁时按 AGENTS.md 用 `LAELIA_RUN_MIGRATION_TESTS=1`)。

## 验证清单(按 AGENTS.md)

```bash
# proto
buf format -w proto && buf lint proto && cd proto && buf generate

# backend
gofmt -w <changed files>
golangci-lint run --allow-parallel-runners
go test ./backend/manager/store/... ./backend/manager/api/v1/... ./backend/manager/migration/...
go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go

# frontend
pnpm --dir frontend biome:check
pnpm --dir frontend check
pnpm --dir frontend type-check
pnpm --dir frontend test

# 迁移门禁(改动涉及迁移时)
LAELIA_RUN_MIGRATION_TESTS=1 LAELIA_TEST_PG_URL=postgresql://dev:dev@localhost/laelia \
  go test ./backend/manager/migration -count=1
```

## 改动文件清单

| 层 | 文件 | 动作 |
|---|---|---|
| DB | `backend/manager/migration/migration/LATEST.sql` | +`user_presence` 表(幂等) |
| DB | `backend/manager/migration/migration/1.1/0030##user-presence.sql` | +建表 + lastLoginTime 回填 |
| DB | `backend/manager/migration/migration_test.go` | +表结构守卫 |
| DB | `backend/manager/migration/migrator_test.go` | 修正存量坏断言:FreshInstall/Upgrade 的期望版本改为从迁移树动态推导(硬编码 "1.0.0" 在增量目录出现后就断了门禁),注入的假迁移动态生成比最新版本高一档的版本号 |
| proto | `proto/v1/v1/presence_service.proto` | +PresenceService(SendHeartbeat/ListPresence/Presence) |
| proto | `proto/v1/v1/command.proto` | −SyncPresence RPC 与三个消息 |
| proto | `backend/generated-go/`、`frontend/src/types/proto-es/` | buf generate 产物 |
| 后端 | `backend/manager/store/presence.go` + `presence_test.go` | +TouchPresence/ListPresence/PresenceOnline/TTL |
| 后端 | `backend/manager/api/v1/presence_service.go` + test | +两个 handler |
| 后端 | `backend/manager/api/v1/command_presence.go` + test、`command.go`、`component/presence/`(整包) | −删除 |
| 后端 | `backend/manager/server/grpc_routes.go` | 换 PresenceService 构造 |
| 前端 | `frontend/src/hooks/use-presence.ts` + test | +新 hook(替换 use-presence-heartbeat.ts) |
| 前端 | `frontend/src/hooks/use-polling.ts` | +fireOnMount 选项 |
| 前端 | `frontend/src/lib/presence.ts` + test | +formatLastSeen;peerPresenceOnline user 分支切换 |
| 前端 | `frontend/src/connect/` | +presenceServiceClient |
| 前端 | `members.tsx`、`conversation-list.tsx`、`channel-members-panel.tsx`、`chat-conversation.tsx` | 消费面切换到 useUserPresence;离线 last_seen 提示 |
| 前端 | `src/locales/en-US.json`、`zh-CN.json` | +last_seen 文案键 |
| 前端 | 各 `*.test.tsx` | 缓存形状与新语义用例更新 |

## 已知边界与演进路径

- **传播延迟 ≤30s**:轮询模型的固有属性,presence 语义可接受;
- **后台 tab**:两循环都停(`refetchIntervalInBackground: false` / usePolling 可见性门控),超 90s 即离线;回到前台双循环立即补拍恢复——与聊天软件"挂机变离线"直觉一致,与旧设计相同;
- **写负载**:每在线用户每 30s 一次窄行主键 upsert,自托管规模(百级用户)可忽略;若将来在线用户上万,可在 handler 层做内存聚合批量刷库;
- **ListPresence 全量上限 2000 行**:更大规模需加分页或按名过滤——届时客户端"全量替换"语义退化为"分页合并",需重评前端替换逻辑;
- **推送演进**:数据模型与 API 形状不变,后续可加 `PresenceStreamService`(Postgres LISTEN/NOTIFY 唤醒 + Connect server-streaming)实现亚秒级广播;轮询作为降级路径保留;
- **多实例**:presence 状态在 PG,天然支持;注意 roomhub(消息长轮询唤醒)仍是单进程约束,与本系统无关;
- **PWA/离线页**:service worker 无法代替页面心跳,不在范围内。