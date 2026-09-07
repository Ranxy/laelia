# Chat 在线状态角标(presence badge)设计与实现方案

> **状态:机制部分已被取代,UI 部分仍然有效。** 本文当年的 human presence 机制(`SyncPresence` RPC、`component/presence` 内存 registry、`use-presence-heartbeat.ts` 心跳查询、`collectWantedNames` 客户端名单收集)已被 [presence-redesign-design.md](./presence-redesign-design.md) 彻底重构:**presence 现在落 Postgres(`user_presence` 表),API 是独立的 `PresenceService`(`SendHeartbeat` + `ListPresence`),前端 hook 是 `hooks/use-presence.ts`**。上述旧文件/RPC 均已删除。凡涉及 presence 机制的细节,一律以新文档为准,不要从本文推断实现。
>
> 本文保留的仍然有效的内容:角标 UI 设计(Avatar 绿点)、agent 在线判定(`ConnectionState`)、各接入点的展示位置与形态、关键决策的历史记录。

## Context

chat 页面此前完全不展示任何参与者的在线状态:用户无法在发起对话前感知 agent/human 是否在线,也无法判断一条 DM 发出去后对方"现在"是否能看到。(实现前的历史快照,供追溯。)

- **Agent 在线状态:后端已有权威信号。** agent 的 liveness 由 machine 连接模型维护——dispatcher 中该 agent 存在活跃 AgentChannel 即 `connected`,叠加 deleted/enabled 生命周期后由 `computeConnectionState`(`backend/manager/api/v1/agent_convert.go`)导出 `AgentStatus.ConnectionState`(ONLINE/OFFLINE/ERROR/KICKED/STOPPED),经 `ListAgents` 暴露给前端,由同频 silent `fetchAgents` 每 30s 刷新。
- **Human 在线状态:当年后端完全没有**(无心跳、无上报,唯一沾边的 `UserProfile.last_login_time` 仅在登录那一刻写入)——这正是后来两版 presence 机制要解决的问题。
- **chat 页头像出现的位置**:左侧会话列表 DM 行(`conversation-list.tsx`)、聊天窗口头部(`chat-conversation.tsx`)、消息行、thread 面板、频道成员面板。

## 关键设计决策(历史记录,标注现行有效性)

1. **角标位置**:左侧会话列表 DM 行头像 + 聊天窗口头部(打开会话的对方头像)。消息行头像不加角标,与微信/Slack 的信息密度一致。**【有效】**
2. **human 在线判定**:~~SyncPresence + 内存 registry~~。**【已被取代】** 现为 PG 落库的心跳 + `PresenceService`,见 [presence-redesign-design.md](./presence-redesign-design.md)。
3. **离线阈值**:90s TTL,心跳间隔 30s → 允许连续丢 2 拍;后台 tab 完全停止心跳。**【有效,数值沿用至新机制】**
4. **agent 在线判定**:仅 `AgentStatus.ConnectionState === ONLINE` 显示绿标(复用 Agents 管理页的判定),OFFLINE/STOPPED/ERROR/KICKED 一律不算在线。**【有效】**
5. **agent presence 不走 presence RPC**:agent 在线状态由 dashboard 布局挂载的 presence hook(现为 `use-presence.ts`)每 30s 一次的 silent `fetchAgents` 刷新(整册返回,复用 `agentsEqual` 跳过逻辑),避免后端在 presence handler 里按 agent 逐个重查 dispatcher liveness。**【有效】**
6. **offline 不显示灰色角标**:只在在线时显示绿点;离线头像保持原样。**【有效】**
7. **频道(type 2)不显示角标**:频道头像位置是 # 图标,成员众多、语义不明。**【有效】**

## 机制现状(2026-02 重构后速览)

详细设计、数据流与边界见 [presence-redesign-design.md](./presence-redesign-design.md)。速记:

- 写:dashboard 布局的 `usePresenceHeartbeat` 每 30s 调 `PresenceService.SendHeartbeat`(纯副作用,身份来自 auth ctx,fireOnMount 挂载即上报);
- 存:`user_presence(handle PK, last_seen_at)`,在线 = 距上次心跳 ≤ 90s TTL,重启不丢、支持多实例;
- 读:消费面通过 `usePresenceMap`/`useUserPresence` 每 30s 拉一次 `ListPresence` 全量 map(整表替换),三态 online / offline / unknown(未知时**不渲染任何徽章**);
- last_seen:离线且有心跳历史时,`formatLastSeen` 渲染"最后在线 x 前"提示。

## Avatar 角标 `frontend/src/components/chat/avatar.tsx`【有效】

`Avatar` 的可选 `online?: boolean`(`undefined` = 不显示角标,所有现有调用点零改动;`true` = 绿点;`false` = 无点),另有 `title?: string` 由调用方传入角标的悬停提示:

```tsx
// 仅 online === true 时才包一层并渲染角标;false/undefined 保持裸头像
return (
  <span className={cn("relative inline-flex shrink-0", sizeClass)} title={title}>
    {core}
    <span
      data-testid="presence-badge"
      className={cn(
        "absolute right-0 bottom-0 rounded-full bg-success ring-2 ring-background",
        size <= 6 ? "size-2" : "size-2.5"   // 点径随头像缩放
      )}
    />
  </span>
);
```

- 颜色用语义 token `bg-success`(`--color-success` #16a34a,tailwind.css 已定义),禁止裸色值;`ring-2 ring-background` 提供标准聊天软件的"描边留白"效果。
- 点径:size-8(32px)头像用 size-2.5(10px)≈ 31%,即常见比例;size-6 及以下头像用 size-2。
- 组件内部 absolute 定位,不涉及 overlay 分层策略(仅组件内部组合)。
- i18n:`chat.presence-online` / `chat.presence-offline`;重构后新增 last_seen 文案键(`chat.presence-last-*`,见新文档)。聊天窗口头部的 `title` 在在线时传 `t("chat.presence-online")`、离线且有 last_seen 时传 last_seen 文案。

## 接入点(位置与形态仍然有效;数据源已切换到 `use-presence.ts`)

### 接入点 1:会话列表 `conversation-list.tsx`

`ConversationRow` 仍是 memo + 纯 primitive props,`peerOnline?: boolean`:

- agent DM(type 1,peer 为 `agents/<id>`):agents 名册 `onlineAgentNames` Set(`isAgentOnline` 仅 ONLINE 为 true);
- user DM(type 4):`usePresenceMap()` 的 `presences[peer]?.online`(unknown 与 offline 均不渲染绿点);
- 频道行与 type-3 agent-DM 行:`undefined`(不渲染)。

### 接入点 2:聊天窗口头部 `chat-conversation.tsx`

头部头像区(isDm/isAgentDm/isUserDm 且 `channel.peer` 存在时)渲染 **peer 真实头像 + 角标**:

- user DM 走 `useUserPresence(peer)`;agent DM 走 `agentPeerOnline(peer, agents)`(`lib/presence.ts`;旧混合 helper `peerPresenceOnline` 已删除);
- `title` 在线时为 "Online",离线且有 `lastSeenAt` 时为 `formatLastSeen(...)`("最后在线 x 前");
- 频道头部保持 # 图标不变;agent↔agent DM(type 3)peer 为 agent,同样显示真实头像 + 在线角标。

### 接入点 3:`/members` 目录页 `pages/dashboard/members.tsx`

与 agents 页一致的**字符串**式展示(非绿点):

- online → `Badge variant="success"` "Online";offline → `Badge variant="secondary"` "Offline",其 `title` 为 last_seen 文案(有心跳历史时);
- **数据尚未加载(unknown)时渲染空**,不渲染 Offline 徽章——"无数据" ≠ "离线"(旧实现把缺失数据渲染成 Offline,是当年刷新后全员离线观感的放大器);
- agent 行保持原 ConnectionBadge 不变;
- 数据:human 行 `useUserPresence(member.name)`。

### 接入点 4:channel 成员抽屉 `components/chat/channel-members-panel.tsx`

与 chat 列表一致的**绿点**式展示:

- agent 成员(`memberType === 2`):agents 名册 `onlineAgentNames` 查 `agents/<memberId>`;human 成员查 `presences["users/<memberId>"]?.online === true`(经 `usePresenceMap` 读取);
- 面板同时被 chat 成员 Sheet 与 channel 详情页复用,两处自动生效。

## 已知边界(现行)

- **深度后台 tab**:心跳与读循环都停止(可见性门控),超过 90s TTL 即显示离线;回到前台立即补拍恢复——与聊天软件"挂机变离线"的直觉一致。
- **service account / system bot**:从不心跳,恒为离线,符合语义。
- **agent 未运行机器时**:STOPPED/OFFLINE 不显示角标,与 Agents 管理页判定一致。
- ~~单进程约束 / manager 重启全员离线~~:已由 presence 落 Postgres 解决,见新文档。

## 历史沿革

- 2026-09-06:初版落地——`CommandService.SyncPresence` + `component/presence` 内存 registry + `use-presence-heartbeat.ts`(客户端收集已加载名单、合并式缓存)。
- 2026-02:机制部分由 [presence-redesign-design.md](./presence-redesign-design.md) 重构取代(`SyncPresence`/内存 registry/`collectWantedNames`/合并缓存全部删除);角标 UI 与接入点沿用。当年机制的完整设计(Proto/后端/前端细节、测试与文件清单)已从本文移除,可在 git 历史中查看本文旧版。