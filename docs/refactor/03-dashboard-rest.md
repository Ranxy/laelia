# 03 — Dashboard 模块(pages/dashboard)深度代码审查报告

> 审查范围:`frontend/src/pages/dashboard/` 下除 `settings-*.tsx`、`agent-profile.tsx`、`machine-profile.tsx`、`chat-conversation.tsx` 之外的全部文件。
> 技术栈:React 19 + Zustand 5(单 store 多 slice)+ ConnectRPC(proto-es)+ Tailwind 4 + Biome;无 react-query,数据获取 = 页面 useEffect + 各页面自定义轮询。
> 注:全文 file:line 均相对 `frontend/src/`;`app/layouts/*`、`components/activity/*` 为直接承载这些页面职责的紧邻代码,一并纳入证据。

---

## 0. 审查范围与事实核对

- **实际范围**:排除 4 个文件后,目录内 **20 个源文件(6,343 行)+ 19 个测试文件(约 4,000 行)+ README,共 40 个文件、约 10,400 行**,全部逐一读完(含全部 `*.test.tsx`);另核对 store 层(user/members/command/reminder/agent/machine/thread/presence/types)、router、`components/activity`、`lib/command-status`、`sheet.tsx` 宽度档等依赖共 ~1,900 行。
- **两处与委托描述不符的事实**(影响后续评估口径):
  1. **`chat-main.tsx` 不存在**(全仓库 `find` 无结果)。聊天页职责由 `chat-layout.tsx`(53 行壳)+ `chat-conversation.tsx`(2166 行,已排除)承担。
  2. **`agents.tsx` 不是页面**,是一个被误放在 `pages/` 下的 helper 模块(见 §5.2)。另外 `chat.test.tsx` 测的是 `components/chat/message-row`(见 §5.3)。"首页" 即 `/`(chat-layout 的 index 路由),不存在独立 home 页;"任务页" 即 `command-list.tsx`(i18n 命名空间 `tasks.*`)。

---

## 一、重复代码

### R1.(高)列表页三胞胎:分页 + 状态过滤 + 表格 + 分页条,三份实现且已发生行为漂移

**位置**:`pages/dashboard/command-list.tsx:65-107, 138-165, 170-294` ↔ `pages/dashboard/reminder-list.tsx:72-131, 135-242, 245-269` ↔ `components/activity/activity-list.tsx:78-135`(由 `activity-layout` 页直接承载)。

**证据**(三处同构片段):

```ts
// command-list.tsx:66-73  ==  reminder-list.tsx:73-79  ==  activity-list.tsx(pageTokens 栈)
const [pageTokens, setPageTokens] = useState<string[]>([""]);
const [pageIndex, setPageIndex] = useState(0);
const [nextPageToken, setNextPageToken] = useState("");
const pageToken = pageTokens[pageIndex] ?? "";
const canPrev = pageIndex > 0;
const canNext = nextPageToken !== "";
```

过滤 chips 逐字重复(`command-list.tsx:141-156` 与 `reminder-list.tsx:137-160` 的选中/未选中两个三元 class 完全一致),分页条(上一页/下一页按钮 + `{t("tasks.page", { n: pageIndex + 1 })}` / `{t("reminders.page", ...)}`)同样一致。

**漂移证据(同一起源、各自演化)**:

| | command-list | reminder-list | activity-list |
|---|---|---|---|
| `onNext` 后是否清 `nextPageToken` | 清(`command-list.tsx:96-101`) | **不清**(`reminder-list.tsx:117-121`) | store 的 token 栈收编(`activity-list.tsx:143-150`) |
| `onPrev` 是否回填 token | 回填 `setNextPageToken(pageToken)`(`command-list.tsx:103-107`) | **不回填**(`reminder-list.tsx:123-126`) | store 管理 |
| 竞态防护(requestSeq) | **无** | **无** | **有**(`activity-list.tsx:99`:`if (seq !== requestSeq.current) return;`) |
| `initialLoadDone` | 无 | 只写不读(死 ref,见 §5.4) | 写并用于 spinner 门控(`activity-list.tsx:263`) |

**量化**:同一套 ~125 行状态机重复 3 份,占 command-list 的 33%、reminder-list 的 46%。`reminder-list` 缺 token 管理是真实 bug 温床(见 §3 B5);竞态防护只修了 activity 一份(见 §3 B1)。

**建议**:抽 `pages/` 同级的 `useListPage<T>` hook:入参 `{ fetch(pageToken, silent) → { rows, nextPageToken } }`,内部持有 pageTokens/pageIndex/canNext/loading/filter + `requestSeq`,暴露 `rows, load, nextPage, prevPage, reset`。三个列表页各剩 ~100 行真正的列渲染。这是本次重构**杠杆最高**的单点。

### R2.(中)rail+pane 两栏详情壳 ×4,tab 详情骨架 ×2

**位置**:
- `pages/dashboard/chat-layout.tsx:28-52` ↔ `pages/dashboard/activity-layout.tsx:16-48`(aside/main 三元 class 结构逐行同构,仅 `w-72` vs `w-80`);
- `pages/dashboard/machines.tsx:122-263` ↔ `pages/dashboard/members.tsx:121-283`(aside 折叠 + `border-l-2 border-l-accent bg-control-bg` 选中行 + 右侧 `Outlet`);
- `app/layouts/agent-detail-layout.tsx:97-205` ↔ `app/layouts/machine-detail-layout.tsx:66-111`(Tabs + `resolvePath` + GetAgent 门禁 + Outlet)。

**证据**(`machines.tsx:256-263` 与 `members.tsx:274-282` 的复制):

```tsx
<div className={cn("min-w-0 flex-1 overflow-hidden", !detailOpen && "hidden lg:block")}>
  <Outlet />
</div>
```

`components/selection-empty-state.tsx:3-5` 的注释("the members / machines rails used to each carry their own byte-identical copy of this")说明这层曾经去重过一次,但外壳层没动。

**建议**:`TwoPaneShell({ rail, detailOpen, width })` + `DetailTabsLayout({ tabs, gate })`,把「mobile 时 rail 隐藏/pane 占满」与「按路由解析 activeTab」收敛到一处;canEdit 门禁进一步下沉为 route loader(见 §6)。

### R3.(中)`global-search.tsx` 内部自造两个 Combobox(单页输入量最大,~460 行是轮子)

**位置**:`pages/dashboard/global-search.tsx:207-408(FromSenderPicker)、454-622(ConversationPicker)`

**证据**:
- 外点关闭逻辑复制两份:`253-262` 与 `486-495` 完全相同的 `document.addEventListener("mousedown", ...)` + `containerRef.contains`;
- 下拉行 JSX(头像/徽标/无结果文案/`onMouseDown preventDefault`)在 `UserPickerOption`(154-176)/`AgentPickerOption`(179-201)/`ConversationPicker`(589-614)三处几乎一致;
- **项目里已有现成原语**:`components/ui/combobox.tsx` 与 `components/member-picker.tsx`(`global-search.tsx:264` 的注释自己写着 "matching the existing MemberPicker pattern");
- 附带违规:两个下拉都是 `absolute ... z-30` 原生浮层(`377`、`566`),违反 `frontend/AGENTS.md` 的「menus/popovers 应使用共享 DropdownMenu/Popover/Combobox 原语,而非 ad hoc absolute z-* markup」;且**完全没有键盘导航**(无 Arrow/Enter/Escape),也无 `role="listbox"` 等可达性语义。

**建议**:用 `Combobox` 重写两个 picker,预计 ~460 行 → ~120 行,兼得键盘导航与 portal 层级合规。

### R4.(中)同一页面内桌面/移动两套完整筛选栏

**位置**:`pages/dashboard/global-search.tsx:856-918(桌面) ↔ 920-1005(移动)`。

**证据**:isDesktop 分支 else 里,scope Select + ConversationPicker + timeRange Select + FromSenderPicker 四个控件**整组复制**,仅 `size="sm"` vs `"md"` 与容器不同(约 150 行重复)。新增筛选项必须改两处。

**建议**:`<FilterToolbar orientation={isDesktop ? "row" : "column"} />`,控件定义提为数组(`[{key, el}]`)。

### R5.(低)移动端 FAB:4 份实现,其中 3 份 className 逐字节相同

**位置**:`pages/dashboard/human-detail.tsx:524-536`、`pages/dashboard/channel-detail.tsx:176-187`、`app/layouts/agent-detail-layout.tsx:188-199`(三者为一模一样的 ~250 字符 class 串)、`pages/dashboard/machines.tsx:230-252`(带滚动收缩动画的变体,grep 验证)。

**建议**:抽 `components/mobile-fab.tsx`(icon/label/width 变体)。

### R6.(低)资源 id 提取散落 29 处、会话类型魔法数字 4 份

- `split("/").pop()` ×11(`activity-detail.tsx:79,81`、`agent-chat.tsx:32`、`command-list.tsx:132`、`human-detail.tsx:131`、`reminder-detail.tsx:69,74`、`reminder-list.tsx:130`、`team-detail.tsx:56`、`chat-conversation.tsx:453,1406`)+ `replace(/^agents\/|^users\/|^conversations\//, "")` ×18(`members.tsx:194,220,262,352,421`、`machines.tsx:157` 等,grep 验证)。无统一的 `idOf(resource)`。
- `conversation.type` 魔法数字:`global-search.tsx:412-415`、`chat-conversation.tsx:104-106`、`agent-chat.tsx:12-13`、`components/chat/conversation-list.tsx:44-46` 四处各自 `const CONVERSATION_TYPE_DM = 1;`。

**建议**:`lib/resource-name.ts` 提供 `resourceId(name)`/`resourceKind(name)`;ConversationType 收进唯一枚举模块。

### R7.(低)杂项重复

- `getChannel` 兜底「roster 没有则单发 GetChannel」在 3 处重复:`channel-detail.tsx:49-68`、`activity-detail.tsx:56-74`、`chat-conversation.tsx:~470`;
- `Field` 组件两套 API:`components/profile-common.tsx:11-26`(children 型)vs `reminder-detail.tsx:477-484`(label/value 型);`team-detail.tsx:381-392` 又第三次手写 label+Input;`user-list.tsx` 用 `FieldRow`(ui/field-row);
- `reminder-list.tsx:53-58 scheduleSummary` 在 `reminder-detail.tsx:279-285` 内联重写;
- `user-list.tsx:771-798 StateBadge/userTypeLabel` 是唯一的状态/类型标签实现,未被 human-detail 复用(后者干脆不显示用户状态);
- 手写 confirm 删除 AlertDialog 四份结构相同(machines/user-list/team-detail/channel-detail),仅文案不同——可抽 `ConfirmDialog`;
- `AlertDialogClose` 用法漂移:machines 包 `<Button>`(`machines.tsx:285-289`),team-detail/client-detail 直接放文本(`team-detail.tsx:437`、`channel-detail.tsx:252`)。

---

## 二、设计问题

### D1.(高)数据层三种访问方式并存,无约定

1. **store slice**:`command-list.tsx:55-58`、`reminder-list.tsx:66-68`、`members.tsx:31-37`、`machines.tsx:40-42`、`user-list.tsx:155-158`;
2. **直连 `*ServiceClient`**:`team-detail.tsx:87-93/213/228/269`(全部数据直连)、`human-detail.tsx:176-231`(groups/IAM/roles)、`machines.tsx:72-73`(getWorkspaceInfo)、`channel-detail.tsx:54`、`activity-detail.tsx:64`、`global-search.tsx:651/699/753`——store 里明明有同义能力(`stores/agent.ts:36-62 fetchAgents`)却在 `team-detail.tsx:87` 再拉 `listAgents({pageSize:1000})`;
3. **`useAppStore.getState()` 逃生舱**:`user-list.tsx`(4 处:213/221/274/337)、`members.tsx`(2 处:57/64)、`global-search.tsx`(3 处)、`agent-mcp.tsx:96`、`machines.tsx:108`(grep 计数见下)。动机是躲闭包依赖,但说明共享 mutation hook 缺位。

`useAppStore.getState()` 分布(范围内非测试):user-list 4、members 2、global-search 3、agent-mcp 1、machines 1(排除文件中 agent-profile 12、machine-profile 8)。

**影响**:测试必须同时 mock 两套(user-list.test 只 mock store、human-detail.test 只 mock clients);mutation 后的级联刷新(谁负责 roster refetch、是否 silent)无统一契约,`{ silent: true }` 由各页自行决定。

**建议**:约定「页面只读 store 声明的 slice action;写操作一律走 store action」;`getState()` 仅限 store 内部或非 React 上下文;跨域动作(如 DM 创建)统一进 store。

### D2.(高)刷新/轮询策略 6 套并存、与选项矛盾、无视页面可见性

| 页面 | 策略 | 位置 |
|---|---|---|
| reminder-list | **2s 无条件** setInterval | `reminder-list.tsx:106` |
| reminder-detail | **2s 无条件,终态也不停** | `reminder-detail.tsx:114-118` |
| chat-layout 左栏 | 5s fetchChannels | `chat-layout.tsx:20`(常量 `LIST_POLL_INTERVAL_MS = 5000`,`:11`,注释解释了 5s) |
| activity 左栏 | 5s | `components/activity/activity-list.tsx:20,126` |
| machines | 10s,条件轮询(`anyNonOnline` 才启) | `machines.tsx:90-104` |
| machine-new | 5s 找新机器 | `machine-new.tsx:63-72` |
| presence 心跳 | 30s + 顺带 `fetchAgents` | `composables/use-presence-heartbeat.ts`(30s,visibilitychange 立即补拍) |
| 会话/线程消息 | 25s 长轮询(store watcher) | `stores/thread.ts:11-17`、`stores/channel.ts:412+`;watcher 内另有 5s badgeTimer(`stores/types.ts:488-496`) |

store 的等值-短路也有三套:`agent.ts:48 agentsEqual`、`machine.ts:44 machinesEqual`、`reminder.ts:15-33 remindersEqual` 手写,`command.ts:47` 无任何短路。**没有任何一个页面级轮询检查 `document.hidden`**(仅 presence 有 visibilitychange)。

**建议**:统一 `useXxxPolling(intervalMs, { while })` / store 内 `pollWhile`;约定「silent + equals-check + visibility-aware」三件套;引用型列表降到 5-10s 或复用 watchers。

### D3.(中)路由与页面职责的错位

- **`agentLifecycle` 等纯逻辑住在 `pages/dashboard/agents.tsx`**——不被路由,唯一 import 是 `agent-profile.tsx:77`(grep 验证:`from "./agents"`);`/agents` 路由是纯重定向(`router/routes/dashboard.tsx:285-301`),与此文件无关。文件名与目录语义双重误导。
- **canEdit/canManage 门禁在 4 处重复**:layout(`agent-detail-layout.tsx:56-67`)、workspace tab(`agent-workspace.tsx:27-43`)、mcp tab(`agent-mcp.tsx:65-76`)+ profile(排除文件)。每次切 tab 重复全量 GetAgent;这是刻意为之(`stores/agent.ts:64-70` 注释:canEdit 不可缓存),但实现层完全没抽象。
- **跨页深链协议靠裸字符串**:聊天深链参数 `?thread=&message=&version=` 由 5 个页面手工拼(`activity-detail.tsx:124-129`、`reminder-detail.tsx:348-353`、`global-search.tsx:734-746`、`channel-detail.tsx:112`、`human-detail.tsx:131`),消费方仅 `chat-conversation.tsx:745-747`。无 `chatHref(conversationId, opts)` helper。
- **动态 tab 路由解析靠手拆 pathname**:`agent-detail-layout.tsx:71-81`、`machine-detail-layout.tsx:58-64` 用 `segments.indexOf(id)+1` 猜 tab,而非 route handles/matching。
- **`machines.tsx:37-39` 用 `matchPath("/machines/new")` 特判 "new"**,因为 `machine-new` 建模成了兄弟路由——`?create=1` 或 Dialog 化可删特判(见 §6.3)。

### D4.(中)错误处理五风格并存

| 风格 | 例子 |
|---|---|
| inline error + Alert | `machines.tsx:281-283`、`reminder-detail.tsx:427` |
| toast(catch→toast) | `team-detail.tsx:99-107/254-263`、`user-list.tsx`(全部 mutation)、`agent-mcp.tsx:103-109` |
| **try/finally 无 catch → 静默失败** | `command-list.tsx:109-127`、`command-detail.tsx:322-331,333-342`、`reminder-detail.tsx:149-177,179-194`、`channel-detail.tsx:80-89,102-116`、`human-detail.tsx:125-135` + `agent-detail-layout.tsx:85-95` |
| 失败伪装成"无结果" | `global-search.tsx:717-723`(`.catch` 仍 `setSearched(true)` → 用户看到"无结果"而非错误) |
| store 层吞错清空 | `stores/command.ts:49-52`、`stores/user.ts:49-55` |
| store 层吞错保留旧值 | `stores/reminder.ts:65-73`、`stores/agent.ts:56-61`(silent 时) |

`lib/connect-errors.ts` 的 `describeError` 只有一半页面在用。**建议**:统一 mutation 反馈三种形态(内联/toast/对话框),由一个 `useMutationAction` 包装兜底;列表失败态学 `members.tsx:151-161`(Alert + retry)。

---

## 三、潜在 bug 与脆弱点

### B1.(高)列表 store 为单一全局 slice,旧响应覆盖新过滤/新页 —— 竞态

**位置**:`stores/command.ts:38-53`、`stores/reminder.ts:43-74`;调用方 `command-list.tsx:75-87`、`reminder-list.tsx:81-108`

**证据**:

```ts
// stores/command.ts:47 — 谁后返回谁赢
set({ commands: res.commands, commandsLoading: false });
```

无请求序号、无 AbortController、无按 agent/tab 分桶。触发路径:reminder-list 的 2s 轮询(`reminder-list.tsx:106`)叠加用户快速点击状态 tab / 翻页;以及用户从 agent A 的列表快速切到 agent B(同一 slice 装不同 agent 数据)。交错时表格显示的是**上一个 tab/上一页的数据**,且 silent 轮询不置 loading 无保护。**`components/activity/activity-list.tsx:96-100` 已有正确修复**(`requestSeq.current`),证明修复模式已知,却未落库为公共 hook。

**建议**:R1 的 `useListPage` 内统一 `requestSeq`(或 store action 接收 seq/分桶 key)。稳定性最高优先级。

### B2.(高)「不是我」按钮立即复活:machine-new 候选机器无法拒绝

**位置**:`pages/dashboard/machine-new.tsx:233-241` 与 `76-98`

**证据**:候选检测 effect 依赖 `[candidate, currentUser, machines, getMachine]`(`98`);拒绝按钮只清 state:

```tsx
// machine-new.tsx:234-241
<Button variant="outline" onClick={() => {
  setCandidate(undefined);
  setCandidateInfo(null);   // ← effect 因 candidate 变化立即重跑,同一台机器继续命中
}}>
```

`setCandidate(undefined)` → effect 重跑 → `machines.find(...)` 仍命中同一台 `createdAt > pageOpenTime` 的机器 → 候选恢复。用户无法真正拒绝一台机器(无排除列表)。

**建议**:加 `dismissedNames: Set<string>`,检测条件过滤;或拒绝后记录 token。

### B3.(高)`activeOutputs/activeEvents` 全应用无清理路径 —— 内存随命令数只增不减

**位置**:`stores/command.ts:73-90, 105-121`(只 append);全仓库 grep 确认**没有任何 action 清空**这两张 map(仅 `stores/types.ts:389-390` 类型定义 + command-detail/chat-conversation 消费)。command-detail 卸载只 abort 流(`command-detail.tsx:131-133`),不清 store;用户浏览过 N 个命令后每个命令的完整输出/事件流常驻内存。

**建议**:command-detail unmount 时 `clearCommandRuntime(cmdName)`;长输出可设上界(如保留最后 2 万条)。

### B4.(中)reminder-detail 对终态 reminder 仍 2s 无限轮询

**位置**:`pages/dashboard/reminder-detail.tsx:114-118`

```ts
useEffect(() => {
  load();
  const handle = setInterval(load, 2000);   // 不区分终态
  return () => clearInterval(handle);
}, [load]);
```

COMPLETED/CANCELLED/FAILED 详情页每 2 秒 GetReminder + re-render,直到离开。`isTerminal`(`134-137`)已算出但未参与轮询。**建议**:terminal 时清 interval。

### B5.(中)reminder-list 分页 token 语义漂移

**位置**:`reminder-list.tsx:117-126` vs `command-list.tsx:96-107`。`onNext` 不清 `nextPageToken`、`onPrev` 不回填 token:回到第 1 页后 `canNext` 仍为 true 并持有陈旧 cursor(通常仍可用,但存在假真窗口,且数据变更后行为未定义)。差异本身就是复制漂移的证据,由 R1 hook 收敛。

### B6.(中)8 处 mutation 无 catch → 失败零反馈(布局层另 1 处)

**位置**(pages 范围内 8 处):`command-list.tsx:109-127`(发送失败 sheet 停在打开态、无提示)、`command-detail.tsx:322-342`(cancel/steer 失败无反馈)、`reminder-detail.tsx:149-177,179-194`(`updateReminder` 抛错只有 `updated === undefined` 分支被容错,网络异常直接泄漏)、`channel-detail.tsx:80-89,102-116`(归档/进入无反馈)、`human-detail.tsx:125-135`;加 `app/layouts/agent-detail-layout.tsx:85-95` 共 9 处。对比正确示范:`machines.tsx:106-120`(actionError)、`team-detail.tsx`(toast)、`agent-mcp.tsx:92-112`(toast)。

**建议**:统一 `try { await; success toast } catch { describeError → toast } finally {}`。

### B7.(中)command-detail 加载失败呈现为空白页

**位置**:`pages/dashboard/command-detail.tsx:76-93` — `getCommand` 失败(store 层吞错返 undefined,`stores/command.ts:55-58`)→ `if (!c) return;` → `displayCmd === null` → 页面只剩返回按钮,无 not-found/错误态。与 reminder-detail 的 not-found(`reminder-detail.tsx:204-220`)不一致。

### B8.(中)human-detail 深链无效 id → 永久 "Loading"

**位置**:`pages/dashboard/human-detail.tsx:63-66, 245-251` — `users.find(...)` 找不到(无效 id / 已删除用户)时永远渲染 `common.loading`,无 not-found、无重试,也未查 `deletedUsers`(store 已有该 slice,`stores/user.ts:19`)判定「用户已删除」。

### B9.(低)机器入库检测依赖客户端时钟对齐

**位置**:`pages/dashboard/machine-new.tsx:80-84` — `Number(m.createdAt.seconds)*1000 > pageOpenTime.current`(`Date.now()`);客户端时钟慢几秒会漏检新机器。建议改用纯服务端事实(`createdBy` + 分页状态)。

### B10.(低)user-list 特殊账号启发式 `id < 100`

**位置**:`user-list.tsx:812-817` — 内建号判定自造 `id<100`(system bot id=1)。建议以服务端字段(`userType === SYSTEM_BOT` 已有,`user-list.tsx:793` 在用)为主,启发式兜底并注明。

### B11.(低)eslint-disable 掩盖的依赖问题(范围内 3 处,grep 验证)

- `agent-mcp.tsx:75`:deps `[agentId, agentName, getAgent]`,但每渲染新建的 `loadAgent` 被刻意排除——建议 useCallback 后去掉 disable;
- `agent-mcp.tsx:89`:mount 时 `mcpServers.length === 0` 才 fetch——首拉失败/空表后不再重试(除整页刷新);
- `human-detail.tsx:242`:deps `[canGetPolicy, user?.name]` 但闭包读 `user.groups`——注释声明故意收窄(避免 roster 刷新重拉 policy),代价是 groups 迟到/变化时徽标不刷新;建议依赖 `user?.name` + `user?.groups` 或入 store。

### B12.(低)细节脆弱点

- `global-search.tsx:846-852`:ESC 徽标是**可点击按钮**(没有真实 `Escape` keydown 监听),与 `836-845` 的 X 按钮功能重复;
- `machine-new.tsx:104,114`:`setTimeout(...,2000)` 无清理(卸载后 setState 幂等 no-op,安全但脏);
- `human-detail.tsx:174-191`:`listGroups({pageSize:1000})` 非管理员也全量拉取,`.catch(() => {})` 静默;
- `reminder-list.tsx:99-108`:`initialLoadDone.current = true` 在**任何**完成路径(包括 silent 轮询后的 then)置位,语义未消费(见 §5.4)。

---

## 四、性能问题

### P1.(高)大列表零虚拟化,容量上限=服务端第一页(或 5000 上限)

**位置**:`user-list.tsx:113-135`(最多 ~100 行 Table,`pageSize:100` 于 `213-225`,`nextPageToken` 弃用);`members.tsx:188-227, 256-267`(roster 由 `stores/members.ts:95-117 drainRoster` **全量 drain**,上限 50 页 × 100 = **5000 行**,无提示);`global-search.tsx:797-818 + components/chat/search-result-list.tsx`(loadMore 累加,无窗口化;grep 确认 package.json 无 react-window/virtua);`machines.tsx:155-224`(100 行)。

- presence 心跳每 30s 替换 `onlineUsers` 对象(`stores/presence.ts:30-40`)→ 每个已挂载 `MemberRow` 重渲,`MemberRow` 订阅到整张 map(`members.tsx:360`:`useAppStore((s) => s.onlineUsers)`),订阅粒度过粗,应改为 `s.onlineUsers[member.name]`;
- 排除文件外的 `command-list/reminder-list` 为 50/页,安全。

**建议**:先做行级 selector + `memo`(成本一行),规模继续上升再虚拟化;roster drain 到上限时给 UI 提示。

### P2.(中)订阅粒度:整表对象订阅 + 输出流无界增长

**位置**:`command-detail.tsx:42-48` 订阅 `s.activeOutputs` / `s.activeEvents` 整个 map——任一 watch 中命令的新 chunk 都会触发本页 re-render;`mergedOutputs`(`228-280`)在每个 chunk 后全量重算(累计 O(n²)),ledger 全列表重渲。结合 B3 的无清理,长会话内存与渲染成本同时膨胀。**建议**:selector `s.activeOutputs[cmdName] ?? EMPTY`;ledger 截断。

### P3.(中)bundle 交叉:activity-detail 拖进整个主聊天页模块

**位置**:`pages/dashboard/activity-detail.tsx:9` — `import { ChannelConversationView } from "@/pages/dashboard/chat-conversation"`(2166 行的主聊天页,含流式/mention/附件管线)。Activity 详情的懒加载 chunk 因此并入手聊页面的全部依赖(markstream 等)。这是本模块唯一一处**页面导出组件给另一个页面**的反向耦合。

**建议**:`ChannelConversationView` 提升到 `components/chat/`,chat-conversation 回归纯页面;activity chunk 随即减重。

### P4.(低)重复渲染小账

- `activity-detail.tsx:56-74`:getChannel 兜底 effect 依赖 `channels` 数组,chat-layout 5s 刷新即重跑(有 cancelled 保护,纯噪音);
- `members.tsx:105-112`:每次渲染 `members.filter×2 + myChannels.filter` 未 memo,搜索键入时 5000 规模全量过滤;
- `command-list.tsx:119-123`:send 成功后手工重组请求,与 effect 自动触发重复一次。

---

## 五、死代码 / 历史债务

### 5.1(中)确认死 i18n key

**方法**:全量提取 `t(...)` 字面量 + 动态模板前后缀,与 1324(en)/1324(zh) 交叉;en↔zh 完全对称(0 缺失)。确认 3 个死 key:

- `sidebar.settings-agent-teams` — 全仓库 0 引用(Teams 已迁入 Members/HumanDetail,侧边栏入口是遗留);
- `settings.agentTeams.create-description`、`settings.agentTeams.create-failed` — 0 引用(team-detail 实际用 `settings.agentTeams.create` / `save-failed`)。

其余「疑似未用」清单(`command.status-*`、`reminders.status-*`、`tasks.filter-*`、`reminders.filter-*`、`activity.filter-*`、`agent.lifecycle.*`、`machine.no-selection`、`members.no-selection` 等)经动态模板(`agents.tsx:47`、`command-list.tsx:154`、`reminder-list.tsx:158`、`activity-list.tsx`、`route-info.ts`)与状态映射(`lib/command-status.ts`、`lib/reminder-status.ts`、`lib/task-status.ts`)核销为**误报,不需动**。

### 5.2(中)`pages/dashboard/agents.tsx` 是文件名与位置双重误导的 helper

**位置**:`agents.tsx:1-48`(grep 证实仅 `agent-profile.tsx:77` 导入)。`agentLifecycle/lifecycleLabel` 的纯逻辑住在 pages/ 下顶着「agents 页面」的名字;`/agents` 路由(`router/routes/dashboard.tsx:285-301`)是纯 redirect。**建议**:移至 `lib/` 或 `components/agent/`,`agents.test.tsx` 一并迁移。

### 5.3(低)`chat.test.tsx` 测试对象不在本文件

`pages/dashboard/chat.test.tsx:4` 仅 import `MessageRow`/`rowStreamingProps`(`@/components/chat/message-row`)测 memo 行为,与「chat 页面」无关(历史页面已拆分)。**建议**:迁至 `components/chat/message-row.test.tsx`。

### 5.4(低)`reminder-list.tsx:97` `initialLoadDone` 只写不读

`97` 声明、`102` 赋值,从未消费——复制自 activity-list(其母本中它参与 spinner 门控 `activity-list.tsx:263`),复制时留下赋值漏掉用途。另 `fireAtDate`(`reminder-list.tsx:47-49`)为单行转发冗余。

### 5.5(低)分页 API 返回 `nextPageToken`,三个调用方均弃用 —— 「静默截断 100 行」

`stores/user.ts:48`、`stores/machine.ts:48` 返回 nextPageToken,但 `user-list.tsx:213-225`、`machines.tsx:59-61`、`machine-new.tsx:63-72` 均只取第一页。与 members(全量 drain)、command/reminder(服务端分页)构成第三种语义:**>100 行静默截断且无提示**。与之对照 `stores/machine.ts:153-170 listMachineAgents` 已实现带 50 页上限的全量 drain——.len 两种实现并存。

### 5.6(信息)Sheet 宽度档位漂移

`user-list.tsx:522,611` 用 `width="medium"`(w-[40rem]),而 command/reminder-detail 用 `standard`(w-[44rem],`components/ui/sheet.tsx:63-79`);`frontend/AGENTS.md` 文档只记载 narrow/standard/wide 三档,实际 palette 有 10 档——文档与 palette 漂移,同类 3-6 字段表单宽度不一致。

---

## 六、重新设计视角(重构目标态)

### 6.1 统一脚手架:三层拆分

```
pages/dashboard/
  useListPage.ts        ← R1/B1/B5:token 栈 + requestSeq + filter 重置 + silent/equals 轮询
  useDetailView.ts      ← 参数实体或 not-found、canXxx 门禁(下沉 route loader,替代 4 处手写 gate)
  useResourcePolling.ts ← D2:silent + equals-check + visibilitychange 的统一策略机
```

- **列表页模板** `<ListPageShell filterBar table pager>`:command-list / reminder-list / activity 左栏 / user-list(active+trash tabs)四页收敛,页内只剩列定义 + row renderer。预估净删 ~400 行。
- **详情页模板** `<DetailPage subject errorState actions>`:team-detail / reminder-detail / channel-detail / human-detail 骨架同构(header + back + 动作区 + 状态网格),统一 not-found(修 B7/B8)与错误反馈(修 B6)。
- **编辑抽屉** `useEditSheet`:`frontend/AGENTS.md` 的 openEntityRef + key + isDirty 模式落为 hook;user-list 162-208 的 ~25 个 useState 编组降为 4 个独立 sheet 组件。

### 6.2 布局页与内容页边界(建议规则)

- **布局页只做**:响应式两栏切换、rail 数据装载与轮询、url → activeTab/高亮。rail 里的列表一律来自 `components/`,禁止页面 A import 页面 B 的导出(消除 P3)。
- `chat-layout`(53 行)/`activity-layout`(49 行)合并为 `TwoPaneRouteShell`(rail 组件 + 落空组件作参数);machines/members 同模板 + `canCreate FAB` 槽位。
- tab 详情骨架:agent-detail-layout / machine-detail-layout 合并为 `DetailTabsLayout({ tabs: [{key, routeHandle, icon, gate}] })`,activeTab 用 route handles 解析替代 `segments.indexOf`。

### 6.3 可参数化合并的页面

- `machine-new.tsx` 改为 `?create=1` 或独立 Sheet(`machines.tsx:37-39` 的 matchPath 特判即可删除);
- `command-list` 与 `reminder-list` 合并为参数化 `AgentTaskListPage({ resource: "commands" | "reminders" })`(共用后端 command 服务、共用 `agentResourceName`);
- `channel-detail` / `human-detail` 的「Back + 标题 + Message 动作 + FAB + 面板」可由同一个 `MemberDetailPane` 呈现;
- `team-detail` 实为人视角的团队编辑器,孤悬在 `users/:userId/teams/:teamId`(仅此处入口),建议并入 agent 域 or 成员详情的编辑 Sheet,消除「路由等级≠编辑器规模」的错配。

---

## 七、本模块重构优先级清单(按性价比排序)

| # | 动作 | 位置 | 解决 | 成本 | 优先级 |
|---|---|---|---|---|---|
| 1 | 抽 `useListPage`(token 栈 + **requestSeq** + filter/reset),command/reminder 先迁,activity 左栏跟随 | R1 / B1 / B5 | 竞态 + ~125 行×3 重复 + 行为漂移 | 中 | **P0** |
| 2 | 修 machine-new「不是我」复活(dismissed 过滤);>100 台截断提示 | B2 / 5.5 | 功能性 bug | 小 | **P0** |
| 3 | 轮询治理:`isTerminal` 停 detail 轮询;列表轮询统一 silent+equals+visibility;统一节奏常量 | D2 / B4 / P4 | 请求量约 -60%(按 2s×2+5s×2+30s 心跳当前组合估算) | 小 | **P0** |
| 4 | `activeOutputs/activeEvents` 生命周期清理 action | B3 / P2 | 内存泄漏 | 小 | P1 |
| 5 | 8 处 try/finally 补 `catch` + `describeError` toast;global-search 区分「无结果/错误」;command-detail / human-detail 补 not-found | B6 / B7 / B8 | 错误反馈一致性 | 小 | P1 |
| 6 | global-search 两 Picker → `Combobox`;删除桌面/移动筛选栏重复 | R3 / R4 | -460 行 + 层级合规 + 键盘可达 | 中 | P1 |
| 7 | `TwoPaneShell` + `DetailTabsLayout` 合并 4 布局;门禁下沉 route loader | R2 / D3 | 布局一致性、去重 | 中 | P2 |
| 8 | 数据层约定:页面只走 store action / selector,消灭 `getState()` 逃生舱与页面直连 client;mutation 级联刷新契约成文 | D1 | 测试 mock 面减半 | 中 | P2 |
| 9 | 死代码清理:3 个死 i18n key、`initialLoadDone`、`agents.tsx` 迁移、`chat.test.tsx` 改名迁移 | §5 | 认知负担 | 极小 | P2(顺手) |
| 10 | `ChannelConversationView` 出迁 components/ | P3 | Activity chunk 减重 | 小 | P2 |
| 11 | 5000 行级 roster 虚拟化 + `MemberRow` 订阅改行级 selector | P1 | 极端规模性能 | 中 | P3(先做 selector 一行) |
| 12 | `lib/resource-name.ts`(`idOf/kindOf`)收编 29 处提取 + ConversationType 唯一化 | R6 | 减少复制 | 小 | P3 |

**总体量化**:P0/P1 全部落地后,`pages/dashboard` 非排除文件预计从 ~6,300 行降至 ~4,300 行(≈-30%),核心列表/详情获得统一的竞态、轮询与错误语义,并为 settings-* / 其他模块的同类重构提供模板。