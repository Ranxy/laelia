# 前端数据层深度审查报告:`src/stores` + `src/composables`

> **⚙ 实施进度标注(批 3 收口后)**
- ✅ 已完成:ADR-1 五 slice 纵切(user/agent/machine/api-provider/mcp,`b95c530`/`9c9c353`/`e7aca3a`,新增 17+3 测试);equal-bailout 六列表(`c9fe388`);chat shim 删除(`b95c530`);无界缓存 LRU + releaseCommand(`5efb461`);全局冻结拆除(`d4774c3`);cleanup 注册表(`e440ba0`,reset/logout 手工清单归零)。
- ⏳ 未完成:ChatGateway(channel/thread 双 watcher 合一 + badge 同节拍);乐观发送编排与 10 处组件 setState;presence/activity/reminder 收编 Query;types.ts 拆分与写入面收敛;`conversations` map 私有化;polling.ts 更名。

> **决策状态更新**:本报告 §7.1 的 TanStack Query 建议已由总报告 **ADR-1 拍板:引入**(`@tanstack/react-query@5`;`useResourceList` 作为列表特化薄壳;迁移按 §7.3 七步执行)。§4 B3 的全局 set 冻结由总报告 **ADR-2 拍板:旧预览体系整体退役**——`use-preview-routes.tsx` 删除、`setSuppressLoadingFlags` 删除、swipe-back 改 CSS 转场(保留手势识别与 sentinel)。其余内容不变。
> 审查对象:React 19.2 + Zustand 5.0.14 + ConnectRPC(proto-es) + Tailwind 4 单页应用,自定义 Zustand 单 store 承担全部数据获取与轮询。
> 审查方式:`src/stores` 全部 24 个源文件(**4739 行**,含 12 个测试文件已通读)+ `src/composables` 全部 4 个源文件(287 行,含 useMentionTargets.test.ts)+ 交叉 grep `src/components`、`src/pages`、`src/app`、`src/lib`、`src/router`、`src/connect`(513 处 store 引用)逐符号验证。未抽样,未使用任何抽样统计。
> 本报告为总报告的一章,编号 05。

---

## 0. 总体快照

| 维度 | 现状 |
|---|---|
| 文件/行数 | stores 24 文件 4739 行;composables 4 文件 287 行 |
| store 形态 | **单 store、19 个 slice**(`index.ts:43-92`),`AppStoreState` 为 19 slice 全交集 + `reset`(`types.ts:878-901`) |
| 行数集中度 | `types.ts`(903)+ `channel.ts`(811)+ `chat.ts`(419)+ `thread.ts`(319)= 2452 行,**51.7%** 集中在聊天域 |
| 轮询实现 | 无 WebSocket;通道/线程 = 25s 长轮询自调度 loop + 5s badge interval(store 内);其余 = 组件内 **8 个独立 `setInterval`**(5s/2s/3s/10s 不等)+ 30s presence 心跳 |
| 错误处理 | 非测试代码 49 个 `catch`,约 35 个静默吞掉(仅复位 loading / return undefined),无统一错误模型 |
| 死代码 | 意外地少:仅 `TaskSlice.closeTask`(task.ts:200-212)一个完整 action 全库零调用;债务集中在**模式复制**而非闲置实体 |
| 测试 | stores 内 12 个测试文件 + composables 1 个,质量高(watcher 竞态、分页窗口竞态、send/echo 竞态均有覆盖),是重构最值钱的安全网 |

**总体判断**:这不是一个混乱的数据层——领域切分清晰、竞态防护用心(channel watcher 的 aborted guard、clearJump 世代令牌、same-ref bailout 都做得相当到位)、测试覆盖真实行为。它的问题是**模式性债务**:同一套「列表加载+分页+silent 刷新+equal-bailout+吞错误」模式被手工复制 9+ 遍,聊天域的状态外科手术泄漏进两个约 2000 行的页面组件,三个缓存无界增长,以及一个会静默丢弃写入的全局 freeze 开关。重构的性价比排序(§8)基于此展开。

---

## 1. 架构评估

### 1.1 `types.ts` 是什么:不是运行时上帝文件,但是「类型契约上帝文件」

**位置**:`stores/types.ts:1-903` | **严重度:中**

903 行**全部是类型与注释,零运行时代码**。四类内容混在一处:

1. **共享 UI 模型**:`ChatMessageUI`(types.ts:51-107,字段+大段注释的「聊天 UI 事实标准」,含 threadPreview/threadNewReplyCount/uploadProgress/roomVersion 等字段约定)、`TaskInfoUI`(111-119)、`MemberSummary`(359-366)、`TaskCountsUI`(678-683);
2. **19 个 slice 接口**(AuthSlice 121-141、UserSlice 148-187、AgentSlice 227-317、MachineSlice 323-354、MembersSlice 370-384、CommandSlice 386-407、ChatSlice 409-467、ChannelSlice 473-573、ThreadSlice 580-607、PreviewSlice 614-645、ImagePreviewSlice 651-660、PresenceSlice 666-671、TaskSlice 685-735、ReminderSlice 737-761、ActivitySlice 763-784、ApiProviderSlice 790-798、McpServerSlice 803-811、SettingSlice 818-859、WorkspaceSlice 865-876),每个都带设计注释——这是本仓库最好的文档;
3. **组合类型** `AppStoreState`(878-901)与 `AppSliceCreator`(903)。

**交集的后果**:`AppSliceCreator<Slice>` 把 `set/get` 的类型定为完整 `AppStoreState`(types.ts:903),因此**每个 slice 都能读写其他所有 slice 的字段**,且跨 slice 写入真实发生、成为常态:

- `channel.ts:491-503` 写 chat slice 的 `chatMessages`、`chatCurrentVersion`、`chatHasNewerByConv`;
- `thread.ts:316-318` 写 chat slice 的 `chatMessages`(syncRootReplyCount 回写根消息回复数);
- `task.ts:150-162` 写 thread slice 的 `threadByRoot`(`patchTaskThreadAndRefresh`);
- `members.ts:35-41` **回写** user/agent slice(`drainRoster` 的 `writeSlice`);
- `index.ts:85-90` 的 `reset` 手写枚举 channel/thread watcher 内部结构。

**问题**:模块边界只靠文件名约定,类型系统不设防。新增字段或 slice 时,TS 报不出「channel 不该写 chatMessages」这类越权错误;`types.ts` 成为唯一耦合枢纽(每个 store 实现 import 它,`ChatMessageUI` 还被 6 个组件经 `@/stores/types` 深度导入,证据:message-context-menu.tsx:12、message-row.tsx:47、tasks-panel.tsx:12、thread-panel.tsx:61、comments-aside.tsx:20、html-comments-aside.tsx:16、agent-profile.tsx:69、chat-conversation.tsx:69、members.tsx:18)。

**重构建议**:① 按域拆类型——`ChatMessageUI/TaskInfoUI` 等收敛到 `stores/ui-models.ts`,各 slice 接口与实现同文件/同目录;② 中期给「写入面」加约束(泛型 `SetState<Slice>` 交叉)或把聊天域的三份纠缠 map(§1.3)合并为单一消息域文件,让所有权显式化。

### 1.2 store 间依赖图:无环,但有两处「接缝」化石

**位置**:`stores/index.ts:1-92`、`channel.ts:28`、`chat.ts:11-15`、`permissions.ts:1-7`

```
index ──► 19 × slice            # 纯组合,index.ts:43-79,无环
permissions ──> index           # permissions.ts:4-7 注释明说:为打断 index↔auth 循环而把 hooks 拆出
channel ──> chat                # 唯一的 slice→slice 模块导入(channel.ts:28):复用 appendNewMessages/fetchConversationDelta/toUiMessage
chat ──> chat-helpers           # chat.ts:11;且 chat.ts:14-15 re-export 兼容层(见 §6 K2)
all ──> types / chat-helpers / polling
task ──> chat-helpers; thread ──> chat-helpers, polling
```

结构上**无循环依赖**,这是好消息。两个接缝:

- `channel.ts:28` 从 `./chat`(chat slice 文件)而非 `chat-helpers.ts` 导入消息 helpers,叠加 `chat.ts:14-15` 的 `export { appendNewMessages, toUiMessage } from "./chat-helpers"`(注释:*"Re-export so existing `./chat` imports of these helpers keep working"*)——一条**过渡期兼容通道**,证明 channel↔chat 切分处于半途状态;
- `permissions.ts:4-7` 的注释本身是一次早前环依赖事故的考古记录(`index` 组合全部 slice,auth 若在模块顶层 import store 会落在 TDZ)。

**建议**:把 `fetchConversationDelta`、`appendNewMessages`、`toUiMessage` 归位 helper 层(或新建 `messaging.ts`),删除 `chat.ts:15` re-export、channel.ts:28 改从 helper 导入。机械小改动。

### 1.3 「每域一个 store」:名义上是,实质是一个扁平 store

**位置**:`stores/index.ts:43-92` | **严重度:高(架构根因)**

19 个 slice 按领域拆分本身是对的(auth/agent/machine/members/chat/channel/thread/task/reminder/activity/setting/user/workspace/preview/image-preview/presence/command/api-provider/mcp)。但它们组合进一个 store 后共享扁平 state、同一个 `set`,使「领域」只存在于文件名。发酵出三个跨域纠缠:

1. **消息域三分**:`chatMessages`(Channel/Chat 共用,types.ts:471-472 注释自证)、`threadByRoot`(ThreadSlice)、`tasksByConv`(TaskSlice)是同一批消息的三份**冗余表示**——"Tasks live in the same chatMessages flow as regular messages (a task IS a message with metadata); this slice is only the panel's separate view onto the task subset"(task.ts:19-26 注释)。为此维护着三组双向同步函数:`applyChannelThreadSummaries`(channel.ts:656-684)、`refreshChannelTaskInfo`(channel.ts:760-811)、`syncRootReplyCount/bumpRootReplyCount/updateRootReplyCount`(thread.ts:269-319)。**数据重复是这 ~300 行同步代码存在的唯一理由**。
2. **花名册三层互写**:`users`(UserSlice)、`agents`(AgentSlice)、`members`(派生合并)——members.ts:11-15 注释明说它 "writes the full rosters back into the source slices so the Machines / Agents / Settings pages stay consistent",即一个「派生视图」拥有对上游的写权(竞态见 §4 B4)。
3. **presence 一次读三域**:`use-presence-heartbeat.ts:30-47` 单次 tick 同时聚合 `channels`(c.peer)、`users`、`channelMembersByConv` 再调 `syncPresence` + silent `fetchAgents`。

### 1.4 reset / 清理策略:模式正确,名单散落

**位置**:`index.ts:80-91`、`reset.test.ts:9-62`、`auth.ts:65-81`、`image-preview.ts:14-46` | **严重度:中**

设计是**先停后清**:`for (const w of Object.values(get().channelWatchers)) { w.ctrl.abort(); clearInterval(w.badgeTimer); }`,thread watcher 逐一 `w.ctrl.abort()`,然后 `set(useAppStore.getInitialState())` 恢复创建时的原始 state(getInitialState 返回的 action 闭包仍绑定活的 set/get)。logout 路径调用 `get().reset()`(auth.ts:71)并手工清理两个模块级缓存(`invalidateAvatar()`、`invalidateImageBlobs()`,auth.ts:76-79)。

四个缺口:

1. **blob URL 不在 reset 覆盖内**:image-preview.ts:42-46 只在 `closeImagePreview` 撤销 objectURL;若登出时 lightbox 开着,`reset()` 直接整体换 state(不经过 closeImagePreview),blob URL 泄漏到下一用户(低危,但同类「忘记 revoke」点会随新增模块增多);
2. **模块级缓存靠手工名单**:auth.ts:76-79 必须记得调用两个 invalidate;「新增有副作用的模块要同时改 store reset + auth.logout + 自己的 close 逻辑」三处默契,团队最容易漏;
3. `reset.test.ts:27-35` 只对 9 个字段断言(currentUser/isLoggedIn/sessionLoaded/agents/chatMessages/unreadByConv/reminders/activities/channelMembersByConv),38-58 验证 watcher 清理——新增 slice 不会自动进入断言面。建议改表驱动:遍历 `getInitialState()` 每个 key 断言恢复,测试随 slice 自动补全;
4. reset 与 `setSuppressLoadingFlags` 冻结交互有漏洞,单独成条(§4 B3)。

### 1.5 `polling.ts` 的设计:名不副实,21 行只是 `sleep`

**位置**:`stores/polling.ts:1-21` | **严重度:低**

仅含一个可被 AbortSignal 提前唤醒的 `sleep`(watcher 退避用),被 channel.ts:29、thread.ts:8 导入。真正的「轮询子系统」是分散的三轨(§3.1)——`polling.ts` 这个名字许诺了一个不存在的架构。建议更名 `delay.ts` 并入 helper 层,真正的轮询收敛(§7)。

---

## 2. 重复代码(量化)

合计可消解约 **700–900 行(≈19% 的 stores 行数)**。每条给出复制点位。

### D1. watcher loop 双写(最重)[高]

**位置**:`channel.ts:412-537` vs `thread.ts:183-244` | 重复约 **120–150 行**

两个「自调度长轮询 loop」逐行同构:建 AbortController → `get()` 读 cursor(`chatCurrentVersion[conversationName] ?? 0n` / `threadByRoot[root]?.currentVersion`)→ 带 waitMs 请求(`channel.ts:435-439`/`thread.ts:196-206`)→ `appendNewMessages` 合并 → `merged !== prev || version !== prevVersion` 同引用 bailout → **双重 aborted 守卫**(set 前一次性检查 + 循环尾检查,channel.ts:490/511-512,thread.ts:215/236-237)→ catch 里 `sleep(1000, signal)`(`channel.ts:507-509`/`thread.ts:231-234`)→ `void poll()`。常量定义连注释都复制(`WATCHER_LONG_POLL_MS = 25000` channel.ts:36-39 ↔ `THREAD_LONG_POLL_MS = 25000` thread.ts:11-14,同样解释 server cap 30000 与 headroom)。外加各自的 watcher registry 增删样板(channel.ts:531-549 / thread.ts:241-260,含 `delete` + 重建对象)与手写类型化签名 `Parameters<AppSliceCreator<...>>[0/1]`(channel.ts:698-699、749-751?、thread.ts:184-185、248-249)。channel 侧还多一路 5s badge interval(channel.ts:522-526)。**任何一处修复(如 §4 B1 乱序防护)都要人肉双份。**

### D2. 「silent 刷新不翻 loading」样板 × 9 [中]

**位置(证据行)**:`agent.ts:37-40`、`machine.ts:34-37`、`api-provider.ts:15-18`、`mcp.ts:15-16`、`user.ts:23-27`(双 flag 路径)、`activity.ts:70-72`、`reminder.ts:44-46`、`members.ts:25-28`、`channel.ts:135-136`。同一个 `if (!silent) set({...Loading: true})` 4-line 模式 9 份,注释各自把「否则表格闪 Loading」又解释了一遍。

### D3. 手写等价比较函数 × 6 [高]

**位置**:`agentsEqual`(agent.ts:276-283,name+protobuf `equals`)、`machinesEqual`(machine.ts:175-185,同款)——这两个用了 protobuf `equals`;而 **`activitiesEqual + timestampEqual`(activity.ts:16-54,38 行手写字段比较)**、`remindersEqual`(reminder.ts:15-33)、`agentActivitiesEqual`(channel.ts:49-61)、`sameThreadPreview`(channel.ts:735-749)全手写字段比对。活动注释自证复制链:activity.ts:29 *"mirroring remindersEqual"*。**风险**:新字段参与渲染但漏写进 equal → 语义性渲染 bug(重渲染或漏更新)。方案:统一为基于 proto schema 的通用比较(可导出一个 `fieldsEqual` 工具),4 个手写实现可删。

### D4. `Record<conv, X>` spread 样板 × 20+ 与 `conversations/${…}` 拼接 × 21 [中]

`set((s) => ({ chatLoading: { ...s.chatLoading, [k]: false }}))` 形态出现在每个 per-conversation flag 上(证据:channel.ts:554-557、566-570,chat.ts:111-113、319-321、364-366,task.ts:59-61、92-94,members? channel.ts:237-243 等)——2 行逻辑膨胀为 5 行 × 20+ 处。字符串前缀 `` `conversations/${…}` `` 在 stores 内出现 **21 处**(grep 计数),chat.ts:210 又有 `` `${conversation}/messages/${messageId}` ``;task.ts:173 甚至要 `rootMessageId.split("/").pop()` 反拆(threadByRoot 的 key 形态不统一:整段资源名 vs 裸 UUID,task.ts:169-172 注释自述)。

### D5. 组件层乐观发送/上传编排双写,含 store 封装破洞 [高]

**位置**:`pages/dashboard/chat-conversation.tsx:1089, 1181, 1221, 1240, 1268` 与 `components/chat/thread-panel.tsx:409, 508, 555, 581, 615` —— **共 10 处组件代码里的 `useAppStore.setState`**(grep 免测试全库仅此两文件)。

两处各自实现同一套流程:「构建 optimisticMsg(已完成附件 + `pending-*` 占位附件 + uploadProgress)→ `setState` 追加 → 上传 progress 回调里再 `setState` 把进度镜像进气泡(`chat-conversation.tsx:1087-1105` vs `thread-panel.tsx:407-430` 几乎逐行对应) → seek 成功后按 `optimisticId` 移除占位、失败标记」。每份 ~150 行,合计 ~300 行近似复制(证据:`chat-conversation.tsx:1165-1268` vs `thread-panel.tsx:492-620`)。store 侧明明有占位替换逻辑(`sendChannelMessage` 的 `optimisticId` 参数与 `withoutOptimistic` 过滤,channel.ts:319-354;`sendThreadMessage` 同款,thread.ts:132-169),但**乐观占位的创建与进度回写留在组件**——同一把状态手术刀一半在 store、一半在页面,封装被打破。

### D6. 分页 drain 循环 × 3 [中]

`fetchConversationDelta`(chat.ts:38-68,首页带 waitMs 长轮询变体)、`listMachineAgents`(machine.ts:156-170,50 页上限)、`drainRoster`(members.ts:95-117,50 页上限)。三份「for page < cap → 取页 → nextPageToken 终止判定 → 累积」循环,仅语义微差。

### D7. `setting.ts` 六连模板 × 2 = 240 行 [中]

`fetchXxx`(setting.ts:92-150)6 个方法与 `updateXxx`(152-282)6 个方法是**同一模板 × 6 × 2**:仅资源名、schema、`v.case` 字符串不同。配套 6 组 update-mask 常量(23-73)。表驱动可压缩到 ~40 行:

```ts
const SETTINGS = [
  ["workspace_profile", WorkspaceProfileSettingSchema, "workspaceProfile", workspaceProfilePaths],
  ["smtp_config", SMTPSettingSchema, "smtpConfig", smtpConfigPaths],
  // ...
] as const;
```

### D8. 吞错误 catch × ~35 [中]

49 个非测试 `catch` 中约 35 个为「空 catch / 仅复位 loading / return undefined / console.error」(证据:channel.ts:130-132, 149-151, 171-173, 196-198, 225-228, 263-267, 286-289, 314-316, 407-409;chat.ts:157-161;agent.ts:56-61;machine.ts:49-52;user.ts:49-55;presence.ts:41-43;members.ts:78-83)。**缺失的不是注释**(写得好),是**语义**:调用方无从区分「空列表」与「加载失败」——`membersError`(members.ts:22, 47-51)是全 store 唯一被 UI 展示的加载失败字段,而完全同构的 users/agents/machines/apiProviders/mcpServers 失败时 UI 只显示空表(members.ts:45-46 注释甚至自证了这个不一致)。

---

## 3. 数据流问题

### 3.1 刷新机制三轨并行,合计 11 个独立定时器/循环 [高]

| 数据域 | 机制 | 位置/证据 | 节奏 |
|---|---|---|---|
| 通道消息 | 长轮询 loop(room_version 增量 cursor) | channel.ts:412-537 | 25s hold + 1s 退避 |
| 通道徽标/线程摘要/任务徽标 | watcher 内 `setInterval` 三连发 | channel.ts:519-526 | 5s × 3 fetch |
| 左栏列表 | 组件 setInterval | chat-layout.tsx:11,18-26 | 5s |
| 线程消息 | 长轮询 loop | thread.ts:183-244 | 25s + 1s 退避 |
| 在线状态(agent 部分借道) | 30s 心跳 + silent fetchAgents | use-presence-heartbeat.ts:9,50-51 | 30s |
| Activity | 组件 setInterval | activity-list.tsx:20,126 | 5s,**无 visibility gating** |
| Reminders | 组件 setInterval ×2 | reminder-list.tsx:106、reminder-detail.tsx:116 | **2s**(全 app 最密),无 gating |
| Machines | 条件式 setInterval | machines.tsx:99-105 | 10s(offline 时才跑) |
| Machine upgrade | 组件 setInterval | machine-profile.tsx:366-376 | 3s |
| 命令输出/事件 | server stream watch | command.ts:60-122 + command-detail.tsx:124-133 | push式,唯一真流 |
| 设备码/安装轮询 | 组件 setInterval | device-login.tsx:71(3s)、machine-new.tsx:70(5s) | 一次性交互页 |

**三套并行范式**:① store 内自调度长轮询 loop(channel/thread);
② 组件 setInterval 静默 fetch;
③ stream watch。对「停止条件、错误退避、同引用 bailout、页面可见性暂停」各有各的做法:watcher loop 有 aborted 守卫;activity/reminder 的 setInterval 无 visibility gating,后台 tab 仍 2–5s 打点;只有 presence 有 `visibilitychange` 立即同步(use-presence-heartbeat.ts:52-55)。review 维护成本最高的正是这张表的每一行。

### 3.2 presence / channel / chat 刷新方式不一致的一致性代价 [中]

- presence 用「30s 心跳推自己 + 每次拉回全部已加载人类」(use-presence-heartbeat.ts:29-45,注释解释了 90s 服务窗口与浏览器节流的换算);
- channel 用「25s 长轮询 + 5s badge interval + 5s 左栏 poll」三路;
- chat(命令执行回显)走 gRPC server stream(command.ts:60-122)。
三者对同一语义(「新事件到来」)有三种延迟与三种失败策略。**open question**:服务器已在 delta 上实现 `wait_ms` hold(官方注释 channel.ts:36-39:server caps wait_ms at 30000),即协议层已支持「准推送」,但 activities徽标/threads/tasks 仍走 5s 短轮询。channel watcher 的 badge interval 中的三个 fetch(refreshChannelThreadCounts/refreshChannelTaskInfo/fetchConversationActivity,channel.ts:522-526)本可与长轮询同节拍(藏进同一响应或顺势触发),请求量可省 **~40–60%**(每会话稳态从 6 路/5s 降到 1 路/25s + 按需)。

### 3.3 乐观更新质量分层(从好到差)

1. **好**:会话列表四个 flip 操作都是「本地乐观 + 失败 refetch 回滚」——`setConversationPinned`(channel.ts:201-229,含 `reorderChannels` 三段排序)、`setConversationClosed`(channel.ts:231-268,含清理 unread + reopen refetch)、`setConversationMuted`(channel.ts:270-290)、`setChannelArchived`(channel.ts:292-317);
2. **中**:发送链乐观完整但**住在组件里**(§2 D5)——回滚语义(发送失败时占位残留在 chat-conversation.tsx:1221-1268 / thread-panel.tsx:570-620 只做部分清理)分散在 4 个 setState 中难审计;
3. **差**:`toggleReaction`(chat.ts:209-233)**完全非乐观**:await RPC 后才写;两次快速点击还会分支翻转(§4 B5);
4. **矛盾**:`fetchChannels` 每 5s 响应**无 equal bailout 并整体替换**(channel.ts:126-129),会碾压轮询间隔内的一切乐观 patch(pinned 顺序、markConversationRead 清零的徽标闪回)。`markConversationRead` 本身做了本地清零(channel.ts:187-199)但撑不过下一个 5s poll 的服务器覆盖(表现:亮/灭闪炼)。

### 3.4 缓存失效策略 = 「永远轮询 + 登出核爆」

唯一全局失效点是 logout 的 `reset()`(auth.ts:71 → index.ts:80-91)。无 TTL、无协商失效;A→B→A 会话切换有 during-visit 光标(`chatCurrentVersion` 增量,chat.ts:115-128 设计得当);前台重新聚焦仅 presence 有 `visibilitychange` 处理(use-presence-heartbeat.ts:52-55),activity/reminder/machines 忽略。失效成本以恒定网络流量支付。

---

## 4. 潜在 Bug 与脆弱点

### B1. 批量 list 接口全量替换、无乱序防护 [高]

**位置**:`channel.ts:120-133`(fetchChannels)、`agent.ts:36-62`、`machine.ts:33-53`、`user.ts:22-56`、`api-provider.ts:14-32`、`mcp.ts:14-44` | **严重度:高**

全部是「await → 整体 set 新引用」,无请求序号 / AbortController / 最新请求优胜。而「同表双路并发」真实存在:fetchChannels 被 chat-layout 5s poll + `mention-detail-sheet.tsx:134`、`human-detail.tsx:130`、`agent-detail-layout.tsx:90` 手动调用;fetchAgents 被 8+ 组件调用 + presence 每 30s silent 调用(use-presence-heartbeat.ts:47)。**慢响应晚到会把旧服务器数据写在更新本地状态之上**——轻则左栏 pinned/未读闪回(channel),重则 rosters 显示已删除条目直到下一轮询。与 D2 九份 silent 样板互为因果:样板不提供请求生命周期管理。

### B2. 内存无界:`activeOutputs` / `activeEvents` / `threadByRoot` / chatMessages 每会话永不驱逐 [高]

**位置**:写入 `command.ts:74-91, 106-121`(只 append);全库 grep 无任何 delete/裁剪,唯一清除是 logout reset。消费在 `command-detail.tsx:42-43,134-137`。`threadByRoot` 只增不删(`openThread`/`loadThreadMessages` 写,`closeThread()` 只切 active 指针不清缓存,thread.ts:83-87);`chatMessages` 每访问过的会话永久驻留(仅 `clearJump` 暂清,chat.ts:400-418)。

命令输出是完整终端流(数 MB/条),一次会话开 N 个 command-detail 页 = N 组输出常驻;每个刷过的 200 条线程快照同理;这不只是「脏数据」,还会放大 P2 的订阅成本。**建议**:① stream 结束(收到流尾或 abort)后对 `activeOutputs[name]` 设 TTL/LRU、仅保留尾部 scrollback(当前页面仍持有 it 可由页面 state 接管);② `closeThread` 后对已关闭 root 的快照设驱逐策略;③ 消息列表定义「窗口 + 段落化分页缓存的统一上界」。

### B3. 全局 set 冻结(suppressLoadingFlags)会静默丢弃所有写,含 reset / 登录 / 发送 echo [高]

**位置**:`index.ts:37-58`(冻结实现,`if (suppressLoadingFlags) return;`)、`lib/use-swipe-back.ts:135, 238-268`(500ms 解冻窗口) | **严重度:高**

swipe-back 预览期间,包装的 `set` 变为**无条件 no-op**——包括 `login` 两次 set、`logout` 内部的 `get().reset()`(其本身就是 `set(useAppStore.getInitialState())` → **在冻结窗口内 logout 的数据清空被静默丢掉**)、`sendChannelMessage` 的 echo 写入、markConversationRead、presence 同步等。被丢弃的写入**不会重放**,且这一熔断对所有业务语义不透明(类型系统/测试均无法察觉)。预览真正想防的是「预览实例 mount → fetch 触发 store 写 → 真实页面闪屏」(index.ts:23-36 注释),正解是预览实例不产生 store 写(props 直通/预览实例冻结自身 effect),而不是熔断现实页面也在用的唯一写通道。**重构时必拆点。**

### B4. `drainRoster` 依赖共享 slice 逐页中转,与心跳/list 流量互踩 [中]

**位置**:`members.ts:95-117` + `use-presence-heartbeat.ts:47` + `user-list.tsx:216-224` | **严重度:中**

`drainRoster` 把「第 N 页」写进**共享的** `users/agents` slice 再 `readSlice()` 读回(以上游 slice 为页面间暂存区,members.ts:11-15 注释、104-115)。期间任何他人写同一 slice——presence 每 30s `fetchAgents({ silent: true })`(use-presence-heartbeat.ts:47,它是全量第一页,会把 drain 中途的 slice 覆回第 1 页)或 settings 页的 `fetchUsers({ filter })`——都会让 `readSlice()` 拿到**错误页**,造成重复行或漏行。且 `fetchMembers` 无 in-flight 互斥:members.tsx:58 (`silent: hasCached`) 与 :157(手动 retry)并发时互写。
**修复**:drain 改**本地累积、一次回写**(让 fetch 返回 rows 而不是副作用回读),加 in-flight 令牌;或直接由 Query 分页接管(见 §7)。

### B5. `toggleReaction` 双击竞态导致 add/remove 分支取反 [中]

**位置**:`chat.ts:209-233` | 严重度:中(频次低但 UX 显眼)

`reacted = msg?.reactions?.find(...)?.reacted` 读取于点击时;两个快速点击在同一请求未落地时读到同样旧值 → 两个「add」或两个「remove」并发,本地与服务器不一致,直到该消息被下次 delta 重写。修法:本地先行(占位 pending)+ mutation 串行化 —— 又一个统一 mutation 层免费送的修复。

### B6. `sendChatMessage` 依赖 `conversations` map 的「隐式预热」 [中]

**位置**:`chat.ts:91-101, 164-206` + 唯一非聊天页调用方 `command-list.tsx:110-125` | **严重度:中**

`ChatSlice.conversations` map **只被 `getOrCreateConversation` 内部写**(chat.ts:91-99),组件从不读它(grep `s.conversations` 除权限字符串外 0 命中)——它是函数级误放在 store state 里的缓存。`command-list.tsx:113` 直接 `sendChatMessage(agent, instruction.trim())` 且从不调 `getOrCreateConversation`:map 冷时 `conversation = ""`(chat.ts:166),请求仍发出(`conversation: conversation || ""`,chat.ts:187),但 echo/reconcile 双分支被 `if (conversation)` 短路(chat.ts:173-205)→ 消息在服务器存在、在 UI 消失,直到下次该会话 delta 补上。正确性依赖「某页面恰好先调过 getOrCreateConversation」这一**跨文件隐式契约**。
建议:`sendChatMessage` 内部先确保 conversation(getOrCreate),或要求显式 conversationId;`conversations` 降为模块内私有缓存。

### B7. 重置不完整的「名单式维护」风险 [中]

**位置**:`index.ts:80-91` + `auth.ts:76-79` + `image-preview.ts:14-46` | **严重度:中**

`getInitialState()` 全量恢复 state ✓;但 (a) watcher 清理在 reset 内**手写枚举两个 slice 的 handle 结构**(index.ts:85-89,应由各 slice 注册 cleanup);(b) 模块级缓存在 logout 里 **额外**失效(auth.ts:78-79);(c) blob URL 不 revoke;(d) reset.test.ts 只锁 9 个字段。四点合并 = 新增「有副作用/模块缓存」的 slice 时需改三处分散代码,建议 `cleanupRegistry: (() => void)[]` 收敛(§1.4 已述)。

### B8. 已覆盖的竞态防护(正面确认,重构时须保留等价行为)

- channel watcher 在途响应三个分界的 abort 守卫(channel.ts:448, 472, 490)与「reset 后防写回」注释(channel.ts:486-490);clearJump 清 cursor 与在途 delta 的交叉处理(channel.ts:467-478);
- thread watcher 同款(thread.ts:213-215);
- 分页窗口世代令牌:`jumpAnchor` 身份比较丢弃在途页(chat.ts:317-336, 362-381),且 await 后重读 current(chat.ts:339-341)——`chat-history.test.ts:146-184` 专门锁这个行为;
- send/echo 乱序:`sendChannelMessage` 按 server id 去重(channel.ts:338-354),`chat-stream.test.ts:227-257` 复现「watcher echo 先于 send 响应」;
- 这些是测试资产,§7 迁移第 4 步直接以之为基准。

### B9. 小型脆弱点(合并)

| 位置 | 问题 |
|---|---|
| chat.ts:50 + 22-24 | 请求 `pageSize: 200` 而自家注释承认后端 clamp 100;`LATEST_PAGE_SIZE=100` 阈值恰巧与 clamp 对上——后端一旦放宽 clamp 而「chatHasOlderByConv」逻辑未同步即错。请求与阈值应同用一常量并注明来源 |
| chat.ts:313 | `loadOlderMessages` 的 `beforeVersion = msgs[0].roomVersion ?? 0n`——首个消息缺 roomVersion(乐观消息房vi无、toUiMessage 的 `roomVersion: msg.roomVersion || undefined`)时把 0n 传给 before 语义,行为未定义(mergeMessages 排序可兜底,风险低) |
| chat.ts:310, 358 | `chatJumpLoading` 单 flag 被向上/向下翻页共用,双向同时滚互锁(有界) |
| channel.test.ts:23、setting.test.ts:40-50 等 | 测试 beforeEach 用手选字段 `setState` 复位——slice 新增字段测试会静默漂移(与 §1.4 表驱动 reset 同根) |

---

## 5. 性能问题

### P1. `fetchChannels` 无 equal-bailout + 整列表订阅 → 稳态 5s 全列表重渲染 [高]

**位置**:`channel.ts:126-129`(`set({ channels: list, unreadByConv })` 无条件新引用)+ `components/chat/conversation-list.tsx:90-92`(订阅整个 `s.channels`/`s.unreadByConv`)+ `chat-layout.tsx:18-26`(5s 驱动)。

对照:agent/machine 列表已有正确做法(agent.ts:48-55 `agentsEqual` bail、machine.ts:44-46,presence.ts:30-40 也做了 map bailout),但聊天左栏没有——5s 轮询每 tick 新引用,ConversationList(以及另外三个直接订阅 `s.channels` 的页面:`chat-conversation.tsx:224`、`reminder-detail.tsx:87`、`activity-detail.tsx:35`)必然重渲染。**~20 行修复,收益最大。**

### P2. `command-detail` 订阅整个 `activeOutputs` map + map 无界增长 [中]

**位置**:`command-detail.tsx:42-43`、写入 command.ts:74-91。

流式输出每条 chunk set 一次新 map 引用 → 整页(含 ledger/timeline overview 等重组件)每 chunk 重渲染,而订阅成本随会话打开过的 command 数(B2 无界)递增。**修法**:`useAppStore((s) => s.activeOutputs[cmdName])` key 化 + B2 驱逐。

### P3. derived state 存 store:`members`、`tasksByConv` 双份拷贝 [中]

`MembersSlice.members` 是 users+agents 的排序合并(members.ts:54-77),作为**派生态**常驻 store 并负有回写上游的义务;`tasksByConv` 是 `chatMessages` 的子集复制,靠 `refreshChannelTaskInfo`(channel.ts:760-811)+ 面板 `loadTasks`(task.ts:57-85)双向同步——同一任务在两处需要被同时更新(channel.ts:753-756 注释明说 "This mirrors refreshChannelThreadCounts",还是「第三份」逻辑)。members 改 selector(`useShallow`)、tasks 改从单一 `messages` 派生(§7)。

### P4. 对象/数组字面量 selector:**0 处**(正面结论)

`useAppStore((s) => ({…}))` 全库 0 命中;不存在「每 tick 新对象永不 bail」的经典陷阱。key 化 selector 与稳定空引用用得很好:`tasks-panel.tsx:51-52`(`s.tasksByConv[convName] ?? EMPTY_TASKS`)、`channel-members-panel.tsx:87`、`thread-panel.tsx:242, 1165`、`chat-conversation.tsx:265-304`(key 化 + `?? EMPTY_MESSAGES`)、`useMentionTargets.ts:21-32`(EMPTY_MEMBERS + per-conversation 订阅)。重构须保住该纪律。

### P5. same-ref bailout 体系已成(正面,须保住)

`appendNewMessages` 空增量返回原引用(chat-helpers.ts:82-91)、`agentActivitiesEqual`(channel.ts:49-61)、presence 无变化不写(presence.ts:30-40)、activities 三重比较(activity.ts:86-112)。这些手写 bailout 可在迁移后由框架 `structuralSharing` 免费替代(§2 D3 同源动机)。

---

## 6. 死代码 / 历史债务

| # | 位置 | 证据 | 定性与建议 |
|---|---|---|---|
| K1 | **`TaskSlice.closeTask`**:task.ts:200-212、接口 types.ts:724-726(注释自述 *"Kept for the agent tool path; the UI now uses updateTaskStatus(DONE) instead"*)、测试 task.test.ts:78-137 | 全 src 非测试代码 **0 调用**(grep `\.closeTask\(` 仅命令到 store 自身与 proto d.ts;components/pages 里 11 处命中全是 `closeTasksPanel` 子串) | **死代码**。删除 action + 接口 + 对应测试段,UI 全走 `updateTaskStatus(DONE)`(task.ts:168-184) |
| K2 | `chat.ts:14-15` re-export shim(*"keep working"*)+ channel.ts:28 仍从 `./chat` 导 helpers | 过渡期兼容通道 | 删 shim、改 import(0.5h,搭车项) |
| K3 | `ChatSlice.conversations` map(chat.ts:82) | 组件零读取,仅内部用(chat.ts:91,166) | 非死但应私有化并修 B6 |
| K4 | 其余逐一核销:`deletedUsers`(user-list 4 处)、`agentChannelsByAgent`/`fetchChannelsForAgent`(agent-chat.tsx:19-21)、`undeleteUser`/`resetPassword`/`upgradeMachine`/`forceDisconnectMachine` 各 1–3 处真实调用、`activeOutputs/activeEvents` 有 command-detail 消费、`membersError`(members.tsx:157 附近) | — | **未发现整段弃用的 slice;死代码面比预期小,债务在模式不在实体** |
| K5 | `polling.ts` 命名(仅 sleep,§1.5);`chat-stream.test.ts:151-158`、channel.ts:378-380 中 "old pollChannelMessages"/"old 2s interval" 等历史注释为考古化石 | — | 别名的良性记载,顺手清 |

---

## 7. 重新设计视角

### 7.1 判断:值得引入 TanStack Query(v5),不值得自研 mini Query

本 store 手写的一切「数据层机制」与 Query 的开箱能力一一对应:

| 本仓库手写的机制 | 本报告证据 | TanStack Query 对应 |
|---|---|---|
| silent 刷新样板 ×9 | §2 D2 | 后台 refetch 不触 loading(`isPending` / `isFetching` 分列) |
| 手写 equal ×6 | §2 D3 | `structuralSharing`(默认开) |
| 请求乱序/去重/竞态 | §4 B1 | 同 key 去重 + 最新查询优胜 |
| 条件轮询(5s/2s/30s/10s)| §3.1 | `refetchInterval` + `refetchIntervalInBackground: false` |
| 后台 tab 仍轮询 | activity-list.tsx:126 等 | `refetchOnWindowFocus` 或精确控制 |
| 失败静默无重试 | §2 D8 | 计数退避重试、`isError` 一等状态 |
| 乐观回滚 4 套手写 | channel.ts:201-317 | `useMutation.onMutate/onError` 标准化 |
| 无界内存 | §4 B2 | `gcTime` 自动驱逐未订阅缓存 |
| logout 核爆 reset | index.ts:80-91 | `queryClient.clear()` + 组件树卸载语义 |

自研 mini Query 的理由不成立:这 9 个机制点每个都需要真实测试才能达到现有心智成本,总耗时 ≈ 引入 Query;且 Query 对 React 19 一等公民。**Zustand 保留**,但职责反转:只存「真 UI/瞬时状态」(面板开合、activeThread、jump 窗口、overlay 状态、watcher 句柄),服务器数据全部交给 Query。

### 7.2 目标分层(与现有文件映射)

```
├─ Layer 1 传输:connect/(ConnectRPC proto-es 客户端)——保持不动
├─ Layer 2 服务器缓存:TanStack Query
│    queryKeys:['users'|'agents'|'machines'|'apiProviders'|'mcpServers'|'settings'|
│              'activities'|'channels'|'myChannels'|'presences'|'tasks'|'threads', scope…]
│    CRUD → useMutation;删除 slice 里的 loading/silent/equal 样板(§2 D2/D3/D7)
├─ Layer 3 实时层:ChatGateway(新单例,替代 channel/thread 双 watcher)
│    - 每会话 1 个 AbortController;25s 长轮询 delta + badge 聚合同节拍
│    - 统一管理:重连退避、页面可见性、会话切换交接、世代令牌、every-channel 并发上限
│    - 命令输出 stream(watchCommand/watchCommandEvents)保留在原 slice/移动到 gateway 均可(它天然 push)
├─ Layer 3' Presence:30s 心跳 hook 保留,数据入 Query(['presences']),staleTime≈25s
└─ Layer 4 Zustand(瘦身后 ~600–1000 行):
     tasksPanelOpen、activeThreadRoot/Conversation、activePreview/activeImage、
     chatJumpByConv/hasOlder/hasNewer(浏览窗口状态)、swipe-back 预览(改为非全局冻结)
```

消息列表本体是关键决策点:**建议保留独立 message store(或放进 Query 的 `queryClient.setQueryData(['messages', conv])`)**——由于 25s 长轮询+乐观发送+窗口语义的行为都被现有测试锁死,放在显式消息 store(或固定 queryKey)里最保守;看板/预览/回复数由 selector 派生,消灭 `tasksByConv`/三向同步(§1.3)。

### 7.3 迁移路径(7 步,每步独立可发布)

1. **铺路(0.5d)**:`lib/query-client.ts` + Provider;删 `chat.ts:15` shim、channel.ts:28 改 import(K2 顺手)。
2. **横向试点——低风险 rosters(1–2d)**:api-provider/mcp/setting/user/agent/machine 六组「列表+分页+silent」全换 `useQuery/useMutation`(同构、无实时性),删 setting.ts 240 行模板(D7)+ 9 处 D2 样板的大多数;**三个 slice 文件在本步即可消失**。
3. **members/合并视图(0.5d)**:`members` 改纯 `useQueries` + select 合并(本步顺带完成 §4 B4 的根治与 D6 一份消解)。
4. **聊天域收拢(3–5d,最大件)**:channel/chat/thread/task 的**状态类**(watcher 竞态、分页窗口语义)迁 `ChatGateway` + 消息 store,乐观发送编排从两个组件(§2 D5)下沉为 `useSendChannelMessage`/`useSendThreadMessage`(onMutate 乐观 + onError 回滚),预计折叠 ~300 行组件代码;**直接以 `chat-stream.test.ts`、`chat-history.test.ts`、`chat.test.ts`、`chat-delta.test.ts` 为行为基准**(它们已覆盖 watcher 生命周期/send-echo 竞态/窗口世代/分页上界)。
5. **presence/activity/reminder 收编(1d)**:三个组件 interval 换 `useQuery({ refetchInterval })`,visibility 行为统一;心跳 hook 保留。
6. **拆除全局 freeze(0.5d)**:swipe-back 改「预览实例不写 store」(预览容器隔离),删 index.ts:37-58 与 `setSuppressLoadingFlags`(消 B3)。
7. **清尾(0.5d)**:reset 简化为 `queryClient.clear() + cleanupRegistry` + Zustand 核心重置;types.ts 拆分+写入面(§1.1);`polling.ts` 更名;补一条 lint/自检:**禁止组件直接 `useAppStore.setState`**(只允许进入 lib/composable 层的专门 action),防 D5 复发。

**风险与保障**:第 4 步是唯一深水区,靠 §4 B8 列出的竞态测试(约 623 行聊天域测试)钉行为;其余步骤都是「新增消费点 → 旧点再删」的双轨式,回归面可控。全量完成后 stores 目录预计 **4739 → ≈2600–3000 行**;11 个组件级定时器收敛到 1 个 gateway + 若干 Query 级 `refetchInterval`。

### 7.4 WebSocket 的定位

若未来上 WS,统一后的 ChatGateway 是唯一受影响模块(长轮询 loop 换 socket 订阅,增量 merge 语义不变)。**不建议**现在为 WS 而 WS:服务端已把 `wait_ms` 长轮询作为官方通道(channel.ts:36-42 自证),且 presence/背景轮询合流后升 WS 的客户端改动被完全隔离在 gateway 内。

---

## 8. 本模块重构优先级清单(按性价比排序)

| # | 动作 | 主要位置 | 工作量 | 收益 | 优先级依据 |
|---|---|---|---|---|---|
| 1 | **fetchChannels equal-bailout**(channels/unreadByConv 内容未变返回同引用;顺带 user/mcp/apiProviders) | channel.ts:120-133 | ~20 行 | 消除聊天页最大稳态重渲染(每 5s 全列表) | 改动极小、零风险、立刻见效 |
| 2 | **删死代码**:closeTask(API+types+tests)、chat.ts:15 shim、channel.ts:28 改 import | §6 K1/K2 | 0.2d | 净删 ~50 行 + 断兼容通道 | 零风险卫生包 |
| 3 | **重做 swipe-back 冻结**(去 set 熔断) | index.ts:37-58、use-swipe-back.ts | 1d | 消除「500ms 窗口内所有写(含 logout reset)被丢弃」类不可复现事故 | 唯一含跨用户数据风险的一致性缺陷(B3) |
| 4 | **三处无界缓存有界化**(activeOutputs/activeEvents 驱逐、threadByRoot 关面板处置、chatMessages 可视面上界) | command.ts、thread.ts、chat.ts | 1–1.5d | 长会话内存上界收敛,叠加修复 P2 | 泄漏型故障的根治 |
| 5 | **toggleReaction 乐观化 + mutation 串行化**(统一 mutation 层试金石) | chat.ts:209-233 | 0.5d | 修 B5 竞态 + 弱网互动体验 | Query mutation 模式的最小切入口 |
| 6 | **接入 TanStack Query 并迁移 rosters/setting 横切片**(§7.3 步 1–3) | 9 个 slice + 消费点 | 2–3d | 净删 ~500–600 行样板,消 D2/D3/D6,修 B1,得错误状态,内存自动 TTL | 最大结构性收益的入场券,低风险先行 |
| 7 | **ChatGateway:双 watcher 合并 + 11 个定时器收敛** | §7.3 步 4 | 3–5d | 消解 D1 ~150 行、UX/网络双向优化、B1/B6 根治、§3.1 表整体缩为 1 行 | 必须在 #6 地基上做;聊天行为测试护航 |
| 8 | **乐观发送/上传编排下沉**(10 处组件 setState 收敛为 2 个 hook) | §2 D5 | 2d | 组件 -300 行,回滚可审计,封装恢复 | 依赖 #7 消息模型定型 |
| 9 | **drainRoster 本地累积化 + members 派生化** | members.ts:95-117 | 0.5d | 修 B4 串页风险,members.ts 大幅瘦身 | 小而准 |
| 10 | **types.ts 拆分 + 写入面收敛 + 表驱动 reset 测试** | §1.1、§1.4 | 1d | slice 边界可见、reset 覆盖自动化 | 与 #6/#7 落地并行顺手完成 |
| 11 | setting.ts 表驱动压缩(240→40 行)、`conversations` 私有化 + sendChatMessage 冷缓存修复(B6) | setting.ts、chat.ts | 0.5d | 净删 + 修隐式契约 | 机械重构 |
| 12 | reminders/activity 轮询降频(2s→5s)+ 全组件 visibility gating | reminder-list.tsx:106 等 | 0.3d | 后台页网络负载 -60%+ | 独立小优化 |
| 13 | WS 升级面预留(长轮询→socket,仅 gateway 内) | ChatGateway | 视后端 | 单点切换 | 依赖 #7 |

**不建议做**:① 全量自研 mini Query(把 D2–D8 的手写模式再自证一遍);② 把 19 个 slice 拆成 19 个独立 store(会加剧 channel→chat 跨域写问题;方向应是**域内融合消息模型**,不是更碎的 store);③ 为「性能」提前上 React Compiler/immer 等工具——本模块渲染问题都有明确局部成因(P1/P2),先修数据层。

---

## 附:审查覆盖与方法备注

- 通读文件清单(非测试):types.ts(903)、channel.ts(811)、chat.ts(419)、thread.ts(319)、setting.ts(283)、agent.ts(283)、task.ts(231)、machine.ts(185)、activity.ts(167)、auth.ts(137)、command.ts(123)、reminder.ts(120)、members.ts(117)、user.ts(106)、index.ts(93)、chat-helpers.ts(91)、preview.ts(84)、workspace.ts(49)、image-preview.ts(47)、presence.ts(45)、mcp.ts(45)、api-provider.ts(33)、permissions.ts(27)、polling.ts(21)= 4739 行;composables:useAvatarEditor.ts(87)、useMentionDetect.ts(54)、useMentionTargets.ts(85)、use-presence-heartbeat.ts(61)= 287 行。
- 测试通读:reset/chat/channel/chat-delta/chat-history/chat-stream/chat.test/command/members/task/setting/activity/presence.test.ts + useMentionTargets.test.ts。
- 交叉验证:513 处 `useAppStore`/`stores/` 引用逐符号 grep(components/pages/app/router/lib/connect,含排除测试),所有「死代码候选」均先 grep 后判断。
- 依赖版本核对:package.json + node_modules/zustand@5.0.14。