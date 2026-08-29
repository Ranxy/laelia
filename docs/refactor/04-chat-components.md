# 聊天模块深度代码审查报告(frontend/src/components/chat/)

> **⚙ 实施进度标注(批 3 收口后)**
- ✅ 已完成:ThreadReplies mentionLabel 透传(含回归测试,`5bc9f90`)。
- ⏳ 未完成:useChatComposer 双份管线收编与 9 处组件内联 setState(批 4,连同三个真实 bug:上传跨会话串台/mentionMap 残留/发送失败不恢复);streaming 管线拆除(待产品确认);thread-panel 拆四件;ConversationRow 方向锁;Avatar sizeClass 显式映射;messages 轻窗口化(ADR-3)。

> **决策状态更新**:本报告 §6.2 的虚拟化建议已由总报告 ADR-3 拍板——按"分片订阅 → 行级 memo → 轻窗口化"三步执行,消息列表不引入 react-virtuoso,原 P2-1(列表窗口化)按此顺序在拆分完成后执行。流式管线去留(P0-2)仍需产品确认后执行。

**审查范围**:23 个源码文件,共 6698 行(thread-panel 1405 / message-row 945 / conversation-list 812 / channel-members-panel 625 / 其余 19 个文件)。已全量逐行阅读,并与 `stores/chat.ts`、`stores/chat-helpers.ts`、`stores/types.ts`、`composables/useMentionTargets.ts`、`composables/useMentionDetect.ts`、`lib/use-auto-scroll.ts`、懒加载协议方 `chat-conversation.tsx`、`lib/image-blob-cache.ts`、`lib/avatar-cache.ts`、`lib/use-swipe-to-close-sheet.ts`、`tailwind.config.js` 做了交叉验证(所有引用/死代码结论均以 grep 实证)。
**技术栈实测**:React 19.2 / Zustand 5.0 / ConnectRPC(proto-es)/ markstream-react 0.0.55 / Tailwind 4.3(带兼容 config)/ Biome + Vitest。

**总体判断**:这是一个被"渐进式打补丁"撑大的模块。局部抽象质量相当高——MessageRow 的 memo 层级、LazyMarkdown 的懒加载门、zustand selector 的 EMPTY 常量纪律、markstream 的 MemoMarkdown 隔离,都做得很对且注释诚实;但三大结构性问题让它处于"每个新特性都比上一个贵"的拐点:① 流式渲染管线在产品层面已放弃却未拆除;② 发送/上传/乐观更新管线在 thread 与 channel 两处完整拷贝(约 600 行);③ 面板外壳与手势方案有三种并行实现。重构收益最大的不是"拆 thread-panel"本身,而是拆掉 thread-panel 与 chat-conversation 之间的发送管线重复,并一次性决策流式管线的去留。

---

## 一、重复代码

### 1.1 [高] 发送/上传/乐观更新管线在 thread-panel 与 chat-conversation 双份完整拷贝(约 600 行)
**位置**:`thread-panel.tsx:70-85, 266-303, 357-648, 855-1113` ↔ `pages/dashboard/chat-conversation.tsx:311-347, 1030-1039, 1061-1140, 1141-1296, 1764-2050`
**问题**:channel 主面板与 thread 面板各有一套完整 composer:上传状态机、乐观消息、mention 插入、自动增高、键盘发送、附件 chip 渲染。**代码证据**(两边逐行同构,差异只有 store 键和一个 `asTask` 开关):

```ts
// thread-panel.tsx:75-85 与 chat-conversation.tsx:320-330 相同的 5 件套
const inFlightUploadsRef = useRef<Promise<Attachment | null>[]>([]);
const adoptedUploadIdsRef = useRef<Set<string>>(new Set());
const activeOptimisticIdRef = useRef<string | null>(null);
const sendingRef = useRef(false);   // "sending 状态异步更新,双击会双发"同段注释两处复制
```

`handleFiles`(thread-panel:384-466 / chat-conversation:1061-1140)、`handleSend` 的 dedupe(`pending-${u.id}` 临时附件、`new Map(...).values()` 去重、失败回滚、`setTimeout(() => textareaRef.current?.focus(), 0)`)、mention 插入后的光标恢复(`mentionState.startIndex + target.handle.length + 2`,thread-panel:664-671 / chat-conversation:1334-1341)均为复制体。
**建议**:抽 `useChatComposer({ mode: "channel" | "thread" })`,store 差异用注入的 `appendOptimistic / patchOptimistic / removeOptimistic` 回调表达。预计净删 500+ 行,并阻止两处独立演进继续漂移(channel 有 `asTask`、thread 没有)。

### 1.2 [高] 乐观更新绕过 store action,`useAppStore.setState` 内联手术 9 处,空 ThreadState 字面量复制 5 次
**位置**:`thread-panel.tsx:409-433, 508-523, 555-576, 581-595, 615-629`;`chat-conversation.tsx:1181-1189, 1221-1235, 1240-1247, 1268-1275`
**证据**(thread-panel:508-523,`?? {messages: [], currentVersion: 0n, loading: false}` 在 412、559、584、618 共 5 处重复):

```ts
useAppStore.setState((state) => ({
  threadByRoot: {
    ...state.threadByRoot,
    [rootMessageId]: {
      ...(state.threadByRoot[rootMessageId] ?? {
        messages: [], currentVersion: 0n, loading: false,
      }),
      messages: [...(...), optimisticMsg],
    },
  },
}));
```

**问题**:组件直写 store 是 Zustand 反模式——切片不变式(去重、与 watcher 长轮询 echo 的 reconcile、排序)只在 `stores/chat.ts` 维护;`chat-helpers.ts:82-91` 的 `appendNewMessages` id 去重保护在 thread 乐观路径完全未复用(watcher echo 与乐观行 id 不同)。真实 `ThreadState` 定义见 `stores/types.ts:581-585`,新增字段时这 5 处会静默漂移。
**建议**:ThreadSlice 增加 `appendOptimisticThreadMessage / patchThreadMessage / removeThreadMessage` 三个 action,内部复用 appendNewMessages。

### 1.3 [中] 面板外壳(移动全屏 aside + swipe transform + 标题栏)三处复制,swipe 关闭却有三套姿势
**位置**:`thread-panel.tsx:238-240, 741-747, 770-778` ↔ `tasks-panel.tsx:44-49, 87-104`;标题栏 `thread-panel.tsx:1245-1401` ↔ `tasks-panel.tsx:105-142` ↔ `channel-files-panel.tsx:121-139`
**证据**:

```
// thread-panel.tsx:240 与 tasks-panel.tsx:91-92 逐字相同
"fixed inset-0 z-panel ... pt-[var(--mobile-header-height)]
 pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))]
 lg:static lg:inset-auto lg:w-[420px] lg:shrink-0 lg:border-l ..."
// 两处 style={{ transform: "translateX(var(--swipe-offset, 0px))",
//              transition: "var(--swipe-transition, none)" }}
```

swipe-close 三种实现并存:`ChatDrawerSheet`(chat-drawer-sheet.tsx:34-43,已正确封装 `useSwipeToCloseSheet`+`useHistorySentinel`);`MentionDetailSheet`(mention-detail-sheet.tsx:71-86,**没有复用 ChatDrawerSheet**,手写 popup/overlay 两个 ref-state + 相同两个 hook);Thread/Tasks 面板(use-swipe-back 的 CSS 变量方案)。
**建议**:抽 `<ChatPanelShell>`(标题 + 关闭 + 移动全屏 + 手势策略),四个右栏面板(thread/tasks/search/files)统一;MentionDetailSheet 改用 ChatDrawerSheet,删 71-86 手写接线。

### 1.4 [中] "desktop 才包 ContextMenu" 模式重复 3 处
**位置**:`conversation-list.tsx:774-811`、`message-context-menu.tsx:70-77`、`channel-files-panel.tsx:344-376`
三者都是 `if (!isDesktop) return row; return <ContextMenu><ContextMenuTrigger …>`,动机注释也几乎相同("long-press 会与滑动手势打架")。建议抽 `DesktopContextMenu` 包装组件统一决策点。

### 1.5 [中] badge 组件家族:三个文件同一层抽象,"紧凑徽标"样式合同手抄 5 处
**位置**:`agent-badge.tsx:11-13`、`task-status-badge.tsx:28-31`、`mention-badge.tsx:12-25`、`message-row.tsx:656-667`
- `task-status-badge.tsx:30` 的 `cn("text-[10px] px-1.5 py-0", className)`;`message-row.tsx:658/666` 为 `CommandStatusBadge`/`TaskStatusBadge` 调用点再写两遍同一 class;`chat-events/tool-call.tsx:65/70` 再来两遍——合计 5 处。
- `mention-badge.tsx` 是裸 `span role="button"`,不经 `ui/badge.tsx`,与前两者两套实现(它需要 inline 语义,应做成 Badge 的 inline variant)。
- `Avatar` 的 `label`/`accent` 自述为"向后兼容参数"(avatar.tsx:14-15),`label` 仅测试引用。
**建议**:`ui/badge.tsx` 增加 `size="xs"` 变体收敛 5 处;MentionBadge 并入 Badge 家族;删 Avatar.label。

### 1.6 [中] 时间格式化 4 套并存,proto Timestamp→Date 换算两种写法
**位置**:`avatar.tsx:95-114`、`lib/command-status.ts:59-65、90-100+`、`search-result-list.tsx:157-159/201-203`、`channel-files-panel.tsx:245-248`
**证据**(同一 proto Timestamp 的两种换算):

```ts
// channel-files-panel.tsx:247
formatTime(new Date(Number(timeSource.seconds) * 1000), i18n.language)
// search-result-list.tsx:158
formatTime(timestampDate(msg.createdAt), i18n.language)
```

`formatTime` 与 `formatConversationListTime` 语义高度重合而实现分叉(一个 locale-aware、一个刻意 locale-stable,见 command-status.ts:91-94 注释)。另 `search-result-list.tsx:101-107` 的 `senderLabel` if 两分支体完全相同(死分支)。
**建议**:建 `lib/time-format.ts` 统一入口,proto 换算统一 `timestampDate`。

### 1.7 [低] stick-to-bottom 逻辑三份,阈值不一致
**位置**:`thread-panel.tsx:329-333 + 351-355(阈值 100)`、`chat-conversation.tsx:962-977(阈值 100)`、`lib/use-auto-scroll.ts:13-24(阈值 40,仅 command-terminal/command-timeline 使用)`
thread-panel 完全重写了 `useAutoScroll` 已提供的能力(ref + onScroll)却未使用。建议 thread 改用该 hook(参数化阈值)。

### 1.8 [低] 紧凑"行"构件三处各自实现
`conversation-list.tsx:504+`(ConversationRow)、`channel-members-panel.tsx:540-625`(ChannelMemberRow)、`channel-files-panel.tsx:178-342`(FileRow)——三个"头像+名称+徽标+时间"行控件各自实现拆卸/悬停/触摸策略。可提供 `RowShell` 统一。

---

## 二、设计问题

### 2.1 [高] thread-panel.tsx 1405 行内部结构区块图谱
| 区块 | 位置 | 行数 | 应拆出 |
|---|---|---|---|
| ThreadReplies(列表,memo) | 83-171 | 89 | 独立文件(还带 bug,见 §3.1) |
| 面板壳 + store 订阅 + 滚动管理 | 215-366 | 152 | ThreadPanel 容器 |
| **上传状态机 + handleSend 乐观管线** | 368-648 | **280** | `useChatComposer`(与 channel 共享,§1.1) |
| mention 检测/插入 + composer UI | 650-674, 855-1113 | ~230 | `<Composer>` 展示件 |
| 任务操作(状态/指派 Select、teams 拉取) | 1166-1361 | ~200 | `TaskHeaderControls` |
| ThreadHeader 其余展示 | 1245-1401 | — | header 展示件 |

**职责混杂证据**:`ThreadHeader`(1127-1403)一边是纯展示(标题/关闭/展开),一边订阅 6 个 store(1158-1165)、组件内直连 RPC `agentTeamServiceClient.listAgentTeams({pageSize:1000})`(1180-1185)、两个表单级 handler(1189-1238)和导航动作(1240-1244)。
**建议**:目标形态 = `ThreadPanel`(容器 ~250 行)+ ThreadMessages + ThreadComposer + ThreadHeader + TaskHeaderControls 五文件;teams 数据进 store。

### 2.2 [中] 组件↔store 耦合:selector 纪律优秀,但"组件直写 store"破坏边界
**正面证据**(应保留的模式):thread-panel:242-249、chat-conversation:264-309 均为 per-key selector + `EMPTY_*` 常量兜底(thread-panel:252 `EMPTY_THREAD`、channel-members-panel:33、tasks-panel:24-25、channel-search-panel:23),避免了"per-key selector 返回 undefined 时新建数组字面量打爆 Object.is"的经典坑——`channel-members-panel.tsx:30-33` 为此写了专门注释。
**问题点**:① 乐观写入直改 store(§1.2 九处);② ThreadHeader 直连 RPC 拉全量 teams(pageSize:1000,每次任务 header 挂载);③ mention resolver(`useMentionTargets.ts:53-68`)是纯展示函数却被当 prop 在 4 层间传递,且 thread 侧漏传(§3.1),更适合 context 化。
**建议**:① 乐观消息走 slice action;② teams 进 store;③ mention resolver 走 `MentionProvider`。

### 2.3 [中] mentions.ts 与 store 的边界:三层表示 + 双重扫描
**位置**:`mentions.ts:5-9(MentionRef)`、`useMentionTargets.ts:10-17(MentionTarget)`、`message-row.tsx:491-510`
mention 有三个表示(proto `Mention` ↔ `MentionTarget` ↔ `MentionRef`),转换分散在 `targetToMention`(useMentionTargets:79-85)与 `mentions.ts`。更实质的问题——**同一 useMemo 周期里对 content 做两次同构全文扫描**:

```ts
// message-row.tsx:491-497 —— segments 仅在 734 行作布尔门,分段结果从未被渲染
const segments = useMemo(() => MentionBadge ? splitByMentions(displayContent ?? "", msg.mentions ?? []) : null, …);
// message-row.tsx:504-510 —— contentWithMentionTags 内部(mentions.ts:191)再跑一遍 splitByMentions
const mentionContent = useMemo(() => … contentWithMentionTags(displayContent ?? "", msg.mentions ?? []) …, …);
```

`splitByMentions` 的"分段渲染"用途已被 `contentWithMentionTags`(单 markdown 通道 + `<mention>` 自定义节点 + 委托点击,设计正确,见 message-row:734-752 注释)取代。
**建议**:`splitByMentions` 降为内部函数,导出 `hasMentions` + `contentWithMentionTags`;`MentionRef` 直接用 proto `Mention`。

### 2.4 [中] 懒加载边界:总体合理,三个缺口
- ✅ `LazyMarkdown`(lazy-markdown.tsx:98-172)设计优秀:600px rootMargin、显式 scrollRoot 免 100 行 `getComputedStyle` 布局抖动(30-42)、延迟一帧建 observer 让位 stick-to-bottom(93-97 注释)、overflow-anchor 排除协议、永不卸载已渲染行。
- ⚠️ 缺口 1:图片无可见性门。`RemoteImage` 在行 mount 即拉 blob(remote-image.tsx:36-57),进频道即挂载至多 100 行;`MAX_CACHED_IMAGES=100`(image-blob-cache.ts:15)被一次会话塞满后 FIFO 驱逐——懒 markdown 省下的解析被图片全量拉取吃回。
- ⚠️ 缺口 2:`eager={messages.length <= 40}`(chat-conversation:208)与 `eager={replies.length <= 40}`(thread-panel:164)双处硬编码同一魔数。
- ⚠️ 缺口 3:懒门只覆盖 markdown;ThreadPreviewBlock、附件 FileCard、Reaction 栏无条件渲染,虚拟化时需一并考虑。

### 2.5 [低] MessageRow 22 个 props 的接口面
**位置**:`message-row.tsx:136-220`。`MentionBadge` 注入的初衷(DM bundle 不引弹层机制,154-156)已失效——channel 与 thread 都恒传,不存在第三种调用方。建议 props 分组(`MessageCallbacks` 对象 + 视觉值),MentionBadge 直连。

---

## 三、潜在 bug 与脆弱点

### 3.1 [高] ThreadReplies 的 `mentionLabel` 声明了但从未透传——thread 回复行永不解析显示名
**位置**:`thread-panel.tsx:111(类型声明)、84-97(解构遗漏)、140-165(<MessageRow> 未传)`;对照根行 812 有传
**证据**:调用侧传了 `mentionLabel={mentionLabel}`(thread-panel:844),但组件内部既没解构也没下传。共享频道中同名成员时,回复行徽标显示裸 handle,而根消息显示"名字(handle)"。**建议**:补透传 + 防回归测试(现 thread-panel.test 无此断言)。

### 3.2 [中] ConversationRow 滑动手势无方向锁,与列表纵向滚动冲突
**位置**:`conversation-list.tsx:574-599、659-668`

```ts
// 582-592:delta 只取 X;无起点 Y 记录、无方向判定、无 touch-action 约束
const delta = startXRef.current - clientX;
setOffset(clampOffset(startOffsetRef.current + delta));  // 每个 touchmove setState → 60fps 重渲染整行
```

对比同仓库正确实现 `use-swipe-to-close-sheet.ts:73-90`:同时记录 startX/startY + `decided` 方向锁 + `SWIPE_BACK_*` 阈值。当前"想纵向滚动但带 1-2px 横向抖动"即拖动行;且 `handleTouchStart` 依赖闭包 `offset`(574-580),拖动期间 handler 每 setState 重建。**建议**:照 use-swipe-back 模式抽 `useSwipeAction`(方向锁 + 直接写 style 不进 state),与 §1.3 的外壳统一复用。

### 3.3 [中] RemoteImage 无可见性检测;objectURL 每 mount create/revoke 的抖动
**位置**:`remote-image.tsx:36-57`
- 挂载即拉取:100 行历史 × 若干图,进频道即发起全部下载(blob 缓存去重并发,但无任何可见性判断;`message-row.image-visibility.test.tsx` 测的是"接收方气泡 hidden"语义,不是 IO 可见性)。
- 每次卸载 revoke:LazyMarkdown 保证 markdown 行不被卸载,但切换 channel 时列表以 `key=msg.id` 全量重挂 → 大量 createObjectURL/revokeObjectURL churn。
**建议**:RemoteImage 内嵌 IntersectionObserver(rootMargin 300px,复用 LazyMarkdown 思路),inline 变体可见才拉取;加 `loading="lazy" decoding="async"` 兜底。

### 3.4 [中] 滚动位置管理:两面板两套协议,LazyMarkdown 的 overflow-anchor 契约是隐式的
**位置**:`lazy-markdown.tsx:116-146` ↔ `chat-conversation.tsx:341-364, 629-689(手动锚恢复事务 + SuppressToken/代际 ref 机制)` ↔ `thread-panel.tsx:329-355(朴素 stick)`
LazyMarkdown 的 fallback→markdown 高度跳变依赖"scroll 容器在非事务期保持原生 overflow-anchor";chat-conversation 为 prepend 分页临时置 `scroller.style.overflowAnchor="none"`(673)并手工恢复(689),靠 5 个 ref 防代际竞态——正确但契约只存在于注释,thread 面板未来若加分页就会踩同一坑而无护栏。另 `scrollToMessage`(thread-panel:339-349)在 rAF 中 scrollIntoView,不等 LazyMarkdown 完成,目标行还是 fallback 高度时定位会再顶偏。**建议**:抽 `lib/chat-scroll.ts`(锚恢复 + anchor 抑制两个函数),thread/channel 共用;LazyMarkdown 头注释指为契约文档。

### 3.5 [低] 其它已验证脆弱点
1. **他人消息缺 senderName 时标成 "You"**:`message-row.tsx:426-430` `msg.senderName || t("chat.you")`,在 `isOwnUser=false` 分支也落到 chat.you;`ThreadPreviewRow` 同型(254-258)。依赖后端 senderName 恒有值。
2. **`avatar.tsx:41` 动态 Tailwind 类**:`const sizeClass = \`size-${size}\`` 无法被扫描器提取;`size-6/7/8/10/12/14/16` 之所以有 CSS 全靠**其他文件的字面量**碰巧存在(实测全 src:size-12 仅 1 处、size-16 仅 2 处;tailwind.config.js safelist:11-25 无 size-*)。改显式 `SIZE_CLASS` 映射表。
3. **失败上传 + 空文本发送**:error 上传 chip 被刻意保留(thread-panel:450-459),此时 `handleSend` 把它放进 tempAttachments;文本为空且全部失败时静默移除乐观行、无任何提示(579-600)。
4. **mention-popup 在 render 期读 `window.innerHeight`**(mention-popup.tsx:76-80):移动端键盘弹出/视口变化期间浮层可能跳位,建议监听 visualViewport。
5. `handleSend` deps 含未使用的 `sending`(thread-panel:641),删除防误导。

---

## 四、性能问题

### 4.1 [高] 无虚拟化——"全量挂载 + IntersectionObserver 内容门"的折中
**位置**:chat-conversation.tsx:172-215、thread-panel.tsx:133-168、lazy-markdown.tsx
现状:每行 MessageRow 确实 `memo`(message-row:346),`rowStreamingProps` 保证非流式行拿到稳定 props(70-82),上下分页 + LazyMarkdown 把 markstream 解析限于可视 ±600px;100 条上限下可用。但 `loadOlderMessages`(chat.ts:309-355)无限累积、无窗口淘汰:每次 store 提交 O(n) diff、DOM 节点线性增长。**缺的是窗口化而不是行级 memo**(方案见 §6.2)。

### 4.2 [中] 流式 markdown 重渲染代价已归零(优点,须固化为纪律)
**位置**:message-row.tsx:96-134(MemoMarkdown)、61(`MENTION_HTML_TAGS` 常量化防止新引用击穿 memo)、734-763
代码注释明确记录了"inline 数组字面量会击穿 React.memo"的教训(57-60)。这是列表性能的核心资产,重构时必须保持:MemoMarkdown 的 props 全为值类型/稳定引用。
**风险**:§2.3 的双扫描让提及行每次内容更新跑两遍全文匹配;`formatTime` 每个带 header 的行每次渲染实例化 2 个 `Intl.DateTimeFormat`(avatar.tsx:97-112)。建议模块级 formatter 缓存。

### 4.3 [中] 上传进度 tick 的写入放大
**位置**:thread-panel.tsx:408-433(chat-conversation:1221-1235 同型)
每 progress 回调整体重建 `threadByRoot` map + 该 thread messages 数组 → 订阅者重渲染、`replies` useMemo 全量重算,再靠 MessageRow memo 逐行 bail(受限于行数,但传大文件时以 chunk 粒度做 O(消息数) diff)。**建议**:`uploadProgress` 移出 ChatMessageUI,放独立 `uploadProgressByAttachment` 分片,气泡按 key 单独订阅——写放大从"整个 thread 数组"缩为"一个 number 槽位"。

### 4.4 [低] conversation-list 是本模块的正面范式
**位置**:conversation-list.tsx:196-242(稳定 id-threaded handlers)+ 504(memo)+ 全 primitive props(含 421-423 预转 ms)。unread/active 变化只重渲染受影响行。channel-files-panel 的 FileRow(178-242)没有 memo,且每次 render `create(AttachmentSchema,…)` 新 proto 对象(195-200),过滤击键时全列表重渲染——参照本条修。

### 4.5 [低] Zustand selector 审计结论:无失效点,一处整 map 订阅(合理)
逐个核对 thread-panel:242-249、channel-members-panel:86-91、tasks-panel:51-61、conversation-list:90-104——均返回稳定引用或原始值。唯一近似问题 conversation-list:92 订阅整张 `unreadByConv`,但 `filtered`(169-179)确实需要;虚拟化后可改 subscribe 手动比较。

---

## 五、死代码 / 历史债务(grep 全部实证)

### 5.1 [高] 整条"流式渲染管线"运行时不可达——`ChatMessageUI.streaming` 全库无 producer
**位置**:`stores/types.ts:60(声明)`;`message-row.tsx:70-82(rowStreamingProps true 分支)、432-475(streamingContent、typing-dots 764-771、wasStreamingRef/fade、prevStreamingRef 自动折叠 456-463)、126-129(smoothStreaming/typewriter/maxLiveNodes)`;`thread-panel.tsx:137`、`chat-conversation.tsx:178`
**证据**:

```bash
grep -rn "streaming: true" src --include="*.ts*"     # 仅 .test. 命中
grep -rn "rowStreamingProps(" src                    # 生产调用全部传 (msg, false, "", EMPTY_EVENTS)
# chat-conversation.tsx:1346-1348 自我承认:
// "Channel rows are never in DM-style streaming mode (channel messages are
//  polled, not streamed token-by-token) … The shared MessageRow still accepts them."
```

channel 已改为 afterVersion 长轮询增量(chat.ts:26-68),token 级流式是旧 DM 架构遗产。**死代码 ≠ markstream 无用**(final 消息仍走 MarkdownRender);死的是 7 个 streaming props、typing-dots、fade 重放机制、`rowStreamingProps` 的整个 true 分支(仅 chat.test.tsx 引用)。**重构决策点**:确认不再有 token 流式 → 一次性拆除;若保留(未来 SSE),收敛为 `msg.streaming` 单布尔 + 内部事件源,而非 4 个 prop。

### 5.2 [中] `splitByMentions` 的分段渲染路径已死,只剩布尔门
**位置**:mentions.ts:75-151、message-row.tsx:491-497 + 734。模块头注释(1-4)描述的"per-segment 徽标渲染"已被 contentWithMentionTags 单通道取代。

### 5.3 [低] 已验证的未引用导出(清运清单)
| 导出 | 位置 | 验证结果 |
|---|---|---|
| `useMentionDetect` hook | useMentionDetect.ts:45-54 | 全库零调用(仅 `detectMention` 直用) |
| `pairToolCallEvents` re-export | message-row.tsx:84-88 | 仅测试消费(chat.pair-tool-call.test.ts),改测试 import 后可删 |
| `EmptyState.action` prop | states.tsx:40 | 全库无传参 |
| `Avatar.label` prop | avatar.tsx:24/63 | 仅 message-row.test.tsx 使用 |
| ThreadReplies.mentionLabel | thread-panel.tsx:111 | 声明未用(实为 bug,§3.1) |
| senderLabel 死分支 | search-result-list.tsx:101-107 | if/return 两体相同 |

文件级:23 个文件全部有消费者,无整文件死代码;无注释掉的代码块。"被新交互替代的旧 UI" = §5.1(streaming)+ §5.2(分段 mention 渲染)两宗。

---

## 六、重新设计视角(方案)

### 6.1 分层:容器/展示彻底分离
```
pages/dashboard/chat-conversation.tsx   (路由容器:watcher、jump、preview overlays)
  └─ chat/
       ChatPanelShell                     ← 统一 aside/Sheet/移动全屏 + 手势策略(§1.3)
       message-list/                      ← MessageList + 轻窗口化 + LazyMarkdown 门
         MessageRow.tsx                   ← 只读展示,props 值类型化(接口面 22→~12)
         message-bits/                    ← ThreadPreviewBlock/AttachmentList/ReactionBar/EventBlock
       composer/useChatComposer + <Composer>   ← channel/thread 共享(§1.1)
       thread/                            ← ThreadPanel(容器)+ ThreadReplies + TaskHeaderControls
       panels/                            ← members/files/search/tasks 四 drawer,共享 PanelHeader
       lib-of-chat: time-format / upload-machine / swipe
```
现状 23 文件中 4 个巨型文件占 57% 行数,其余 19 个都在 100-625 行的健康区间;重构主体是"拆 thread-panel 与 message-row + 抽 composer",不是推倒。

### 6.2 虚拟化方案:建议"轻窗口化",不引重库
1. 现有 100 条上限 + 双向分页窗口(hasOlder/hasNewer/jumpWindow)天然契合;markstream 高度不可预估是重虚拟化库的难点。
2. 方案:保留 LazyMarkdown 的"未渲染→fallback"机制充当**高度未知行**,在 `[data-msg-id]` 外层记录已渲染行高度;距视口 >2 屏的行降级为"高度记忆占位行",滚动接近时恢复。DOM 稳定在 ~60 行内,无滚动跳跃。
3. 若用成熟库:react-virtuoso 的 `followOutput` + `initialTopMostItemIndex` 与现有 stick-to-bottom 语义最接近,但要整体下线 overflow-anchor 协议(其内部高度管理接管)。**建议分两步**:先做 §6.1 拆分与 §5.1 死代码拆除(否则迁移面双倍),再做窗口化。

### 6.3 消息渲染管线目标形态
```
ChatMessage(proto)                     ← store 唯一可信源(action-only 写入)
  → toUiMessage(chat-helpers)          ← 保留,映射唯一一次
  → MessageVM(isOwn/showAvatar/…)      ← 每消息只派生一次
  → <MessageRow vm callbacks/>         ← 展示,memo(vm)
  → MemoMarkdown(content)              ← 保留现状(本模块最佳资产)
```
关键增补:把 `replyCount/taskBadge/reactions` 级别的元数据从 msg 对象拆出独立分片,使"徽标 tick"不再触碰正文行;`uploadProgress` 独立分片(§4.3)。

---

## 七、本模块重构优先级清单(按性价比排序)

| # | 动作 | 位置 | 预期收益 | 风险/工作量 |
|---|---|---|---|---|
| **P0-1** | 抽 `useChatComposer` 统一 channel/thread 发送管线;乐观写入改 slice action(append/patch/remove) | thread-panel:368-674+855-1113;chat-conversation:1061-1296;ThreadSlice 新增 3 action | 消 ~600 行重复、9 处内联 setState,竞态面单点化 | 中;已有 thread-panel/chat 两套测试护栏 |
| **P0-2** | 决策流式管线去留并执行(删除或收敛 streaming prop 链) | message-row:70-82/432-475/764-771;stores/types.ts:60 | MessageRow 接口 -7 props,为虚拟化清障 | 低;需产品确认 |
| **P0-3** | 修 ThreadReplies mentionLabel 透传缺失 + 防回归测试 | thread-panel:111/844 | 共享频道徽标消歧恢复正确 | 极低 |
| **P0-4** | ConversationRow 滑动加方向锁 | conversation-list:574-599 | 消除移动端滚动误拖 | 低 |
| **P0-5** | Avatar sizeClass 改显式映射表 | avatar.tsx:41 | 防样式静默回归 | 极低 |
| **P1-1** | thread-panel 拆四件(Replies/Composer/Header/TaskControls),主文件目标 <250 行 | thread-panel 全文 | 1405→5 文件,行为不变 | 中;机械拆分 |
| **P1-2** | RemoteImage 加 IO 可见性门 + lazy 属性 | remote-image:36-57 | 大历史首屏省 ~90% 图片带宽 | 低 |
| **P1-3** | mentions 单次扫描 + resolver context 化 + 删 useMentionDetect 死 hook | mentions.ts、message-row:491-510 | 每行每次更新省一次 O(n·m) 扫描 | 低 |
| **P1-4** | `lib/time-format.ts` 统一 4 套时间格式化 + Intl formatter 缓存;清 senderLabel 死分支 | avatar.tsx:95-114、command-status.ts:59-100、search-result-list:101-107、channel-files-panel:245-248 | 行为一致 + 每 header 少 2 个 Intl 实例 | 低 |
| **P1-5** | 面板外壳统一:MentionDetailSheet 改用 ChatDrawerSheet;新增 ChatPanelShell | mention-detail-sheet:71-86;tasks-panel:87-142;thread-panel:238-240 | 手势三套语义对齐 | 中;手势测试需全量 |
| **P2-1** | 消息列表轻窗口化(§6.2);eager=40 阈值常量化 | chat-conversation:208、thread-panel:164 | 大频道内存/滚动性能 | 高;须在 P1-1 后做 |
| **P2-2** | badge 家族收敛(ui/badge 加 xs variant;MentionBadge 并入) | agent/mention/task-status-badge、message-row:656-667 | 视觉合同单点维护 | 低 |
| **P2-3** | 死代码清运(§5.3 清单) | 见表 | 表面积收缩,零业务引用 | 极低 |
| **P2-4** | FileRow 补 memo + AttachmentSchema create 上提 | channel-files-panel:163-200 | 过滤输入卡顿消除 | 低 |

**不建议动的部分(应作为不动点保留)**:MemoMarkdown 的 props 冻结纪律、LazyMarkdown 的 fallback→swap + overflow-anchor 方案、conversation-list 的 memo+primitive props 组合、`EMPTY_*` selector 模式、`useHistorySentinel`/`platformOwnsEdgeSwipe` 的平台分派——这些是本模块的正确资产;重构应以它们为基准,把外围的复制与越权 store 写入剪掉。