# 前端「事件 / 预览 / 工作区」组件模块深度评审

> **⚙ 实施进度标注(批 6 收口后)**
- ✅ 已完成:整文件死代码(command-timeline/CommandTerminal/use-auto-scroll)+ isToolEvent/投机枚举/双映射清除(`b0499db`);iframe 桥 `safeOpenExternal` 白名单(`75d844b`);watch 流断线重连 + 退避 + seqNo 续传(`4b7cf57`)。
- ✅ 批 6 完成:TimelineModel 归一(`b3c2644`:`lib/command-events-model.ts` 唯一 merge/kind/时间与状态语义,行键漂移根治,`command-event-kind.ts` 删除);preview 收敛(`16a10c8`:CommentsPanel 双胞胎合并 + FilePreviewShell + useHtmlPreviewBridge + F-B9/F-S2/F-S3/F-B10);ledger 虚拟化 + 100KB 截断 + 搜索防抖(`38f78af`);workspace 树扁平化虚拟化 + role=tree + 树内搜索(`a4dcabf`);activity 魔数(F-B7)与 workspace-file-panel t 依赖(F-B8)已修;inspector timing 死分支(F-D7)随迁。
- ✅ 批 9 完成:proto `tool_call_id` 契约 + 全链路透传(`68e53a6`/`69396f8`:两个 ToolCall*Payload 增可选 `tool_call_id`;ToolCallSink 接口与 ACP 三帧、acp2 thread executor、pi executor 全部发射点透传 runtime id;interleaved/pi 测试钉死 ID 契约),前端配对改 ID 优先 + FIFO 兜底(`baf4167`:并发交错不再错配,断线缝隙丢 START、legacy 无 ID 仍走事件序兜底,pair 按 started 顺序输出)。
- ✅ 批 10 完成:F-S8 双分页收敛(`29be54e`,产品拍板统一无限滚动)——activity-list 桌面 Prev/Next 分页栈退役,桌面/移动共用一套 token 栈 + IntersectionObserver sentinel;`useActivityPages` 去 `intervalIndex`,5s 轮询统一骑第 0 页(newest-first offset 分页下唯一稳定窗口,修掉轮询可见 offset 页的漂移隐患);`activity.page/prev/next` 三死键删除,测试改写为无限滚动三案。
- ⏳ 未完成(留待后续):overview 真实时间轴与 span 上限(F-P4,依赖 model 后续演进);inspector WARNING 游离 tab 之外(F-B6);SidePanel 壳统一(F-S5);AgentSelect→Combobox(12 项)。

> 范围:`command-events/`(5 文件 1750 行)、`preview/`(6 文件 1470 行)、`agent/`(4 文件 796 行)、`activity/`(2 文件 575 行)、`workspace/`(3 文件 474 行)、`chat-events/`(3 文件 214 行),并对照 `components/chat/message-row.tsx`、`components/command-timeline.tsx`、`components/command-terminal.tsx`、`pages/dashboard/command-detail.tsx`(主消费者)、`stores/{command,preview,image-preview,activity,workspace}.ts`、`lib/{tool-call-events,html-file,command-status}.ts` 与 proto 契约 `proto/v1/v1/command.proto`。全部文件已完整阅读,未抽样;所有"无人引用"结论均经全仓 grep + git 历史验证。

---

## 1. 总体架构判断(执行摘要)

这批模块正处于一次**未完成的中期演进**中:`git log` 显示 command 详情经历了 `efff395`(增强展示)→ `9c6d31f` "trajectory-style command Output & Events redesign"(诞生 `command-events/`)→ `857f0bb`(revamp)的换代,但**旧实现 `command-timeline.tsx` 与 `CommandTerminal` 未删除**,形成旧新两套渲染管线并存;`chat-events/` 是 chat 侧与 command 侧共用的事件原语层,但只有 Diff/Warning 真正被复用,工具调用卡在两侧各写了一份且**状态判定语义已经分叉**;`preview/` 整体质量最高(桥协议有 nonce/epoch/e.source 三重校验、请求竞态有 requestId 防护、blob URL 生命周期正确),但存在一个**真实可利用的 XSS 面(`window.open` 无 scheme 白名单)**与评论面板的 300 行复制粘贴双胞胎;`workspace` 树与 `activity` 列表是无虚拟化的全量渲染,在现有数据规模下尚可、在"大规模重构"目标下是明确的技术债。

最优先的三件事:**删死代码(≈350 行,零风险)**、**堵 XSS 面(10 行)**、**把四份输出合并算法收敛为一个纯函数模块**。这三项完成后,后续的「统一事件渲染管线」重构才有一个干净的落点。

---

## 2. 死代码与历史债务(维度 5)

### F-D1 【高|死代码】`command-timeline.tsx` 整个文件是旧管线的遗留
- **位置**:`frontend/src/components/command-timeline.tsx:1-200`(200 行)
- **证据**:全仓检索 `CommandTimeline` 无任何 import 方(唯一命中是 `lib/use-auto-scroll.ts:5` 的注释 "Reused by CommandTerminal and CommandTimeline");git 历史 `9c6d31f feat: trajectory-style command Output & Events redesign` 之后其职责已被 `command-events/command-event-ledger.tsx` + `command-event-timeline-overview.tsx` 完全取代。
- **连带影响**:它 import 的 `ChatToolCall`(line 2)与 `pairToolCallEvents`(line 3)看似有引用,删除后需复查;`useAutoScroll` 的两个使用方都是死组件(见 F-D3)。
- **建议**:整文件删除;`use-auto-scroll.ts` 一并删除;如需保留自动滚动能供未来管线使用,移入新管线包并补测试。

### F-D2 【中|死导出】`CommandTerminal` 组件无调用方,文件只剩 `FinalSummary` 活着
- **位置**:`frontend/src/components/command-terminal.tsx:8-58`
- **证据**:grep 全仓,`CommandTerminal` 无 import;同文件的 `FinalSummary`(60-72)仍被 `command-list.tsx:6`、`command-detail.tsx:13` 使用。
- **建议**:删除 `CommandTerminal`,把 `FinalSummary` 平移到独立文件;顺带消除该文件旧主题 token(`bg-dark-bg`、`text-matrix-green`)的最后残留。

### F-D3 【中|死代码】`lib/use-auto-scroll.ts` 全链路死亡
- **位置**:`frontend/src/lib/use-auto-scroll.ts:1-27`
- **证据**:唯二使用方 `command-timeline.tsx:4` 与 `command-terminal.tsx:15` 均为死组件。
- **建议**:删除;若统一管线的虚拟化列表需要 auto-scroll,再按需重写。

### F-D4 【低|死导出】`isToolEvent` 零使用
- **位置**:`frontend/src/components/command-events/command-event-kind.ts:181-186`
- **证据**:全仓 grep 仅定义处;调用方都直接比较 `CommandEventType.TOOL_CALL_*`。
- **建议**:删除,或在统一管线中作为唯一判断入口,二选一,不要留两套写法。

### F-D5 【中|投机代码】`commandEventKindExtra` 预留了 contract 中不存在的枚举
- **位置**:`frontend/src/components/command-events/command-event-kind.ts:144-166`
- **证据**:硬编码 16/17/18(steer/retry),注释自述 "Placeholder kinds for event types that may be added later";proto 当前 `CommandEventType` 最大为 `TOKEN_USAGE = 15`(`proto/v1/v1/command.proto:79-96`)。连带 `matchesFilter` 的 `"steer"`/`"retry"` 分支(`command-event-ledger.tsx:84-85`)永远不可达。
- **建议**:删除;等 proto 真正新增枚举时随 PR 一起加,避免翻译键让联调误以为已支持。

### F-D6 【中|平行双映射】事件类型 → i18n 标签存在两份独立维护的真相源
- **位置**:`frontend/src/lib/command-status.ts:33-49,136`(`commandEventTypeToI18nKey`,已导出、零使用)vs `frontend/src/components/command-events/command-event-kind.ts:42-141`(`commandEventKind[].labelKey`)
- **证据**:两者覆盖同一批枚举,前者独有 `TEXT_DELTA → "command.event-text"`,后者独有 icon/tag/phase;全仓 grep `commandEventTypeToI18nKey` 只有定义与导出处。
- **建议**:删除 `commandEventTypeToI18nKey`;kind 注册表(§7 的 `EventKindRegistry`)成为唯一映射,i18n key/icon/phase/lane 全部收进去。

### F-D7 【低|死 UI 分支】Inspector 的 timing 占位分支不可达
- **位置**:`frontend/src/components/command-events/command-event-inspector.tsx:545-549`
- **证据**:`availableTabs`(44-78)只在 TOOL_CALL 分支 push `"timing"`,而此时 `isTool` 恒为 true(413-421),`t("command.event-no-timing")` 分支永不渲染。
- **建议**:简化为直接渲染 dl;或把 timing 合并进 ToolOverview,减少一个 tab。

### F-D8 【正面确认】其余模块全部存活
`preview/`(经 `app/layouts/dashboard-layout.tsx:35-70` lazy 挂载)、`agent/`(team-detail/human-detail/agent-profile/machine-profile)、`activity/`(`activity-layout.tsx:4`)、`workspace/`(`agent-workspace.tsx:4-5`)、`chat-events/`(`message-row.tsx:28-30,682-692` + inspector 复用)均有引用,不存在整目录死模块。

---

## 3. 重复代码(维度 1)

### F-R1 【高】"合并连续同类型输出块"算法存在 4 份拷贝,且语义已互相分叉
- **位置(4 份)**:
  1. `command-timeline.tsx:112-135`(renderItems,死代码)
  2. `command-event-ledger.tsx:356-376`(merge-before-filter 循环,**无条件合并**)
  3. `command-event-timeline-overview.tsx:94-183`(run/flushRun,**要求 `run.end <= ts` 才续接**,line 118)
  4. `command-detail.tsx:228-280`(mergedOutputs,**要求 `current.endTs <= ts`**,line 265)
- **证据片段**(ledger):
  ```ts
  if (last && last.row.kind === "output" && item.row.kind === "output"
      && last.row.output.type === item.row.output.type) {
    last.row.content += item.row.output.content;   // 无时间单调性检查
  ```
  vs overview:`if (run && run.type === item.output.type && run.end <= ts)`。
- **后果**:时间戳非严格单调时(跨流竞态、重连后 `afterSeqNo` 续传回放、机器时钟抖动),三者产出**不同的行集合**;而行键统一取"首块 seqNo"(`out-${seqNo}`),于是 ledger 的一行在 overview 中可能没有对应 span,或反之 → `selectedKey` 高亮落空、inspector 打不开(见 F-B4)。
- **建议**:新建 `lib/command-events-model.ts`:`mergeOutputs(outputs, breaks, opts)` 单一实现(明确时间戳乱序策略:按 seqNo 排序而非按 ts;输出块本质是流内有序的,seqNo 才是全序),四个调用方(删一份死代码后剩三份)全部改调。

### F-R2 【高】`tsToMs` / 时间格式化散落 5 处,且与既有 lib 重复
- **位置**:`command-event-ledger.tsx:97-100`、`command-event-timeline-overview.tsx:42-45`、`command-detail.tsx:229-230`、`command-timeline.tsx:48-51`;`formatTimeMs` 双份:`command-event-inspector.tsx:85-91`、`command-event-ledger.tsx:111-117`;`formatTime`:`inspector:80-83`、`ledger:102-109`;而 `lib/command-status.ts:59-67` 已有 `formatTimestamp/formatTimeOfDay`。
- **建议**:proto Timestamp→ms 与时间格式化系列全部下沉 `lib/command-status.ts`(该文件就是为此存在的),各组件只做展示。

### F-R3 【高】工具调用状态语义三个组件三套判定,且与后端实际枚举脱节
- **位置与证据**:
  - `chat-events/tool-call.tsx:58-73`:`status === "completed" || status === "success"` → success Badge,**`"error"` 会落到灰色 secondary**;
  - `command-event-ledger.tsx:208` 与 `command-event-inspector.tsx:204`:`status === "error" || status === "failed"`;
  - `command-event-timeline-overview.tsx:140-152`:同 error 判定。

  而后端实发只有 `"success" | "error"`(`backend/agent/pi/executor.go:522-525`:`status := "success"; if ev.IsError { status = "error" }`);proto 定义是自由字符串(`proto/v1/v1/command.proto:190-193`)。
- **后果**:chat 侧的错误工具调用显示成灰色"已完成"(误导用户);`"completed"`/`"failed"` 是死分支。
- **建议**:定义单一 `toolCallStatusToVariant/statusLabel` 映射(放在 §7 的注册表),proto 侧建议加注释或 enum 约束;四个渲染点共享。

### F-R4 【高】评论面板是 300 行级的复制粘贴双胞胎
- **位置**:`preview/comments-aside.tsx:29-221,223-279` vs `preview/html-comments-aside.tsx:54-221,223-281`
- **证据**:结构逐行同构——头部(29-35 ≈ 54-76)、评论列表(137-153 ≈ 136-153)、composer(156-218 ≈ 155-217,连 `enterToSend` 处理 189-194 都逐字相同)、`CommentRow` 完整复制(223-279 ≈ 223-281,含 avatar 推导 241-251)。差异只有三点:pendingAnchor 归属(markdown 本地 state vs overlay 升-state)、跳转函数(DOM scrollIntoView vs bridge locate)、focusKey。加载逻辑也重复:`comments-aside.tsx:67-69,86-93` ≈ `html-comments-aside.tsx:34-47(useHtmlComments)`。
- **建议**:抽 `CommentsPanel`(props 注入 `onJump(sectionId, quote)` 与受控 `pendingAnchor`);anchor 策略抽象为 `AnchorLocator` 接口(markdown = DOM 定位,html = bridge locate),正好是 §7 管线中"锚点协议"的雏形。

### F-R5 【中】三个预览 overlay 的壳层三胞胎
- **位置**:`html-preview-overlay.tsx:308-360,484-500`、`markdown-preview-overlay.tsx:82-146,239-255`、`image-preview-overlay.tsx:40-66`
- **证据**:同一套"fixed inset-0 + LAYER_SURFACE_CLASS + h-14 顶栏(文件名/字节/按钮组)+ Body(flex)"三处复制;Esc→关闭的 window keydown effect 三处逐字重复(html:233-240 / md:44-51 / img:28-35);`Placeholder` 组件两份(html:484-500、md:239-255);`usePreserveHigherLayerAccess("overlay")` + `getLayerRoot("overlay")` 三遍。
- **建议**:抽 `FilePreviewShell({ attachment, actions, children })` 统一顶栏/Esc/portal/Placeholder;三个 overlay 各剩 100 行左右真正差异化的内容。

### F-R6 【中】iframe 桥的父端消息处理双实现
- **位置**:`html-preview-overlay.tsx:160-230` vs `workspace/html-file-view.tsx:24-42`(外加 `onLoad` 激活 51-61 vs overlay 的 `handleLoad` 155-158)
- **证据**:同为 `slockAcBridge === 1 && nonce 相等 && documentEpoch 相等` 三段校验 + `link-clicked` 分支,逐行复制。协议一旦加消息类型(例如未来的 zoom/打印),必须记得改两处。
- **建议**:抽 `useHtmlPreviewBridge({ onLinkClick, onEscape?, handlers })` 放 `lib/html-file.ts` 旁;消息类型用 TypeScript 判别联合集中声明。

### F-R7 【中】搜索框绕过共享原语;搜索行为与 chat 侧不一致
- **位置**:`command-event-toolbar.tsx:50-60` 手写 `<input type="search">`;项目已有 `components/ui/search-input.tsx`,且 `chat/channel-files-panel.tsx:142-148`、`chat/channel-search-panel.tsx:169-175` 都在用。行为上 `channel-search-panel.tsx:43-83` 有 250ms debounce + cancelled 守卫,而 ledger 搜索(`command-event-ledger.tsx:92-95,381`)是每键即时、无防抖、无缓存地 `toLowerCase()` 全量内容。
- **建议**:toolbar 换 `SearchInput`;过滤/防抖/匹配收敛到一个 `useFilteredRows` 小模块(§7)。

### F-R8 【低】`formatBytes` 住在 chat 组件里被 4 个模块跨域引用
- **位置**:定义 `chat/file-card.tsx:8-19`;消费方 `preview/markdown-preview-overlay.tsx:13`、`preview/html-preview-overlay.tsx:13`、`workspace/workspace-file-panel.tsx:6`、`chat/channel-files-panel.tsx:14`。
- **建议**:移 `lib/format.ts`(与 F-R2 的时间格式化一并),`file-card` 改 import。

### F-R9 【低】avatar 推导三份
- **位置**:`comments-aside.tsx:241-251` ≡ `html-comments-aside.tsx:241-251`,与 `chat/message-row.tsx` 的推导同型(`principalId/agentId/senderName` 分支)。
- **建议**:抽 `useSenderAvatar(msg, currentPrincipalId)` 进 `lib/avatar-cache.ts` 旁。

### F-R10 【正面】配对逻辑本身已收敛在共享 lib
`lib/tool-call-events.ts:14-27` 是唯一实现,四处调用(`ledger.tsx:278`、`overview.tsx:67`、`command-detail.tsx:215`、`message-row.tsx:441`)——这是本仓库做对的部分,§7 只需把它挪进归一化层。

---

## 4. 设计问题(维度 2)

### F-S1 【高】XSS 滥用面:桥转发的 `link-clicked` 无 scheme 白名单,`window.open("javascript:…")` 可在应用 origin 执行脚本
- **位置**:`preview/html-preview-overlay.tsx:218-222`;`workspace/html-file-view.tsx:35-38`;上游 `lib/html-file.ts:272-287`(只截断 4097 字符,不校验 scheme)。
- **证据**:
  ```ts
  case "link-clicked": {
    const href = String(d.href ?? "");
    if (href) window.open(href, "_blank", "noopener,noreferrer");
  ```
- **分析**:iframe 本身防护优秀——`sandbox="allow-scripts"` 无 `allow-same-origin`(opaque origin)、nonce+epoch、`e.source === iframeRef.current?.contentWindow`、`referrerPolicy="no-referrer"`、桥内 `preventDefault` 了链接与表单(`html-file.ts:272-292`)。但链接的最终打开发生在**父窗口**,新 about:blank 窗口会继承 opener 的 origin(`noopener` 只切断 `window.opener` 引用,不改变 origin 继承)。预览/工作区内容是**不可信附件**(用户上传 + agent 生成),一个 `<a href="javascript:fetch('/api/...',{...})">` 即可以应用身份执行任意脚本。
- **建议(低成本,应立即做)**:父端统一走一个 `safeOpenExternal(href)`,scheme 白名单 `http/https/mailto`(`URL` 解析失败即丢弃);桥端同样白名单化后仅放行 http(s)。两处实现收敛进 F-R6 的 hook。

### F-S2 【中】不受信文档可通过 `state` 消息风暴拖垮宿主页面
- **位置**:桥 `lib/html-file.ts:105-139`(MutationObserver 监听 `documentElement` 的 `attributes/childList/characterData + subtree`,rAF 节流)→ 父端 `html-preview-overlay.tsx:173-183` 每条 message `setScroll(...)` 触发全 overlay 重渲染。
- **分析**:rAF 节流把最坏频率钳在 ~60 msg/s,但一个带 CSS 动画/时钟的页面可以让父组件**每帧 setState × 全量重渲染**(含评论 pins、flash 等所有子树),这是经典的宿主 DoS 面;预览的是不可信内容。
- **建议**:① 父端对 `state` 做值比较(diff 无变化丢弃,scroll 未变不 setState);② 桥端将 state 上报降频(如 100ms 节流 + 仅在真实几何变化时发送:比较 docWidth/docHeight/scroll);③ 检测到异常频率(如 >30/s 持续)时降级为轮询。同时把 `esc` 消息(桥 295-297,恶意文档可编程派发 KeyboardEvent 反复关 overlay)加节流。

### F-S3 【中】"nonce/epoch 是 per-open 秘密"的实现与注释矛盾
- **位置**:`html-preview-overlay.tsx:85-90`(`useMemo(() => randomId(), [attachmentId])`)与注释(86-88)"nonce/epoch are per-open secrets"。
- **分析**:依赖数组是 attachmentId,同一附件**关闭后再次预览**会复用同一 nonce/epoch(组件 `if (!active) return null` 但 hooks 状态保留)。当前无实际攻破路径(校验链含 `e.source` 强校验,opaque origin 不可伪造),但"per-open"承诺已失效,且未来若有逻辑绑定"每次打开唯一 epoch"会静默踩坑(例如 F-B9 的 locate 请求映射)。
- **建议**:用 `useState(() => randomId())` + 在 attachmentId 变化的 reset effect(104-111)里重置;或把生成逻辑移入 `openFilePreview`(store)随 `activePreview` 下发。

### F-S4 【中】command-events 的职责边界:旧管线残留 + 与 chat-events 的复用半途而废
- **证据链**:
  - 旧组件 `command-timeline.tsx` 未删(F-D1),其"输出块合并 + 工具卡 + 时间排序"与 ledger/overview 三个 useMemo 构成 4 份平行实现(F-R1);
  - `chat-events/` 作为共享原语层,被 `message-row.tsx:28-30` 和 `command-event-inspector.tsx:5-6` 共用,但只覆盖 Diff/Warning:**工具调用卡在 chat 侧(ChatToolCall)、ledger 侧(ToolContent 198-230)、inspector 侧(ToolOverview 168-245)是三种独立形态**(F-R3);
  - "事件种类语义"有两份注册表(F-D6),而时序总览的 **lane 归类又是第三套内联逻辑**:`command-event-timeline-overview.tsx:157-171` 把所有非 diff/warning/compaction 事件(含 TOKEN_USAGE/FINAL_SUMMARY/LIFECYCLE/PERMISSION_*)塞进 lane 0 "Output"、染成 stdout 的 `bg-info/70`,与 ledger 的 `phase` 体系(`command-event-kind.ts` 的 usage/system/summary/permission phases)互相矛盾——同一事件在两视图里呈现不同类别。
- **建议**:这就是 §7 统一管线的直接动机:kind 注册表(含 phase 与 lane)唯一化,三种工具卡收敛为一个 `<ToolCallCard variant="row|card|inspector">`。

### F-S5 【中】抽屉/面板壳层四种形态并存,移动端无方案
- **证据**:inspector 是 ledger 上的绝对定位浮层 `command-detail.tsx:489-500`(`absolute inset-y-0 right-0 z-10 w-80`,移动端直接盖死表格);CommentsAside/HtmlCommentsAside 是静态 `w-80` aside(chat-events-aside 双胞胎,见 F-R9/F-R4);chat 侧有成熟 `ChatDrawerSheet`(`chat/chat-drawer-sheet.tsx:52`,Sheet + swipe-to-close + history-sentinel);preview 内 aside 又是第三种。frontend 的 overlay/层策略(AGENTS.md)明确要求 dropdown/panel 走共享原语,但"侧栏面板"这一层原语是缺失的。
- **建议**:抽 `SidePanel`(desktop: 内嵌 aside;mobile: 复用 ChatDrawerSheet 的 swipe/历史语义),inspector/评论 aside/文件面板统一换壳。

### F-S6 【中】workspace 树:无虚拟化、无 a11y、无搜索,patchRow 全树重映射
- **位置**:`workspace/workspace-tree.tsx:175-253`(递归 `TreeRows` 全量渲染)、`:160-173`(`patchRow` O(n) 不可变重映射,每次 toggle 触发)、`:200,228,237`(用内联 `paddingLeft` 表达深度)、搜索缺失——chat 侧文件抽屉有"名字搜索"(`channel-files-panel.tsx:111-117`),工作区树连"树内过滤"都没有,与本报告命题所提"树上搜索"重复问题相反:是**搜索能力不一致**。
- **建议**:扁平化展开状态(rows + expandedSet)后用 `@tanstack/react-virtual` 虚拟化(节点高度固定 28px 极易虚拟);补 `role="tree"/treeitem` 与上下键导航;树内文件名过滤复用 `SearchInput`。

### F-S7 【低】`AgentSelect` 是 ad-hoc 下拉,违反项目 layering/组件规范
- **位置**:`agent/agent-team-form.tsx:292-363`(`z-30` 本地 z-index 于 341、手写外点关闭 307-316、无 ARIA listbox/option、无键盘导航);同文件 232-255 的角色选择用的却是共享 `Select`。
- **建议**:替换为共享 `Combobox`(支持 portal,符合 frontend/AGENTS.md "下拉必须走共享原语"条款);顺带修复键盘可达性。

### F-S8 【低】activity 列表:桌面分页 + 移动无限滚动双方案共存
- **位置**:`activity/activity-list.tsx:69-86(pageTokens 栈/gotoPage)、121-128(轮询)、181-199(IntersectionObserver)` + store 端静默合并逻辑 `stores/activity.ts:88-105`。
- **分析**:竞态防护是认真的(`requestSeq` 81-101,注释清楚),但三条写路径 + 双分页语义让后续改动成本高;桌面"第 N 页"和移动"无限列表"共享同一个 `activities` 数组与 `activitiesNextPageToken`。
- **建议**:收敛为无限滚动(桌面列表区高度足够),分页栈删除;或把分页逻辑整体下沉 store。同时轮询(5s,`:121-128`)建议加 `document.visibilityState` 门控。
- **✅ 批 10 已修**(`29be54e`,产品拍板:统一无限滚动;先经批 8 `53b9cc3` 迁 Query per-(filter,pageToken)):桌面分页栈/翻页脚手架/`lastRowsRef` 翻页保持全部删除,双端共用 token 栈 + sentinel;轮询可见性门控已随批 8 由 `refetchIntervalInBackground` 覆盖,批 10 起 5s 轮询统一骑第 0 页(后端 `created_at DESC` + offset 分页下唯一稳定窗口);`activity.page/prev/next` 死键删除,activity-list 316→235 行。

---

## 5. 潜在 Bug 与脆弱点(维度 3)

### F-B1 【高】工具调用配对是 FIFO 猜测,并发工具调用必然错配
- **位置**:`lib/tool-call-events.ts:9-27`;契约缺口 `proto/v1/v1/command.proto:190-193` —— `ToolCallFinishedPayload` 只有 `status/raw_output`,**没有关联 ID**;注释自认 "payloads carry no correlation id... pair by event order: FIFO"。
- **败坏场景**:agent 并发两工具时事件序为 `START-A, START-B, FIN-B, FIN-A` → FIFO 把 A 配上 B 的结果,状态与输出全部张冠李戴;且 START 丢失一条(断线缝隙)后所有后续配对永久错位一格。
- **建议**:proto 为 STARTED/FINISHED 增加 `tool_call_id`(配合 AIP 字段命名规范),前端配对改 ID 匹配 + FIFO 兜底;这是"大规模重构"里少数必须先动 proto 的项。
- **✅ 批 9 已修**:`68e53a6`(proto 字段 + Go/proto-es/grpc-doc 重新生成)+ `69396f8`(ToolCallSink 接口带 id;ACP DefaultAdapter/OpenCodeAdapter 三帧、acp2 thread_executor、pi executor 全部发射点透传;interleaved-opencode 与 pi 测试钉死 payload ID)+ `baf4167`(`pairToolCallEvents` 先按 `tool_call_id` 匹配,无 ID/ID 无匹配走 FIFO 兜底,兜底关闭时同步退休 map 条目防二次配对;新增并发交错/START 丢失/legacy 混合三案)。

### F-B2 【高】watch 流断线不会重连:网络闪断 = 页面永久停更
- **位置**:`stores/command.ts:74-89,106-122`(for-await 抛错即 `return false`)、消费方 `command-detail.tsx:124-134`(一次性订阅,`.catch(() => {})`)。
- **场景**:长时间运行命令,中途网络闪断 → 流关闭 → 页面既不重订阅也不提示,command 仍在跑而 ledger/timeline 冻结,用户只能手动刷新。
- **建议**:订阅封装进 store(带 exponential backoff 重连,`afterSeqNo` 续传机制已经存在——`command.ts:61-66,93-98`——只差重连循环);或页面层 useEffect 内做重连循环并给 UI 一个"重连中"状态。

### F-B3 【中】`activeOutputs/activeEvents` 无限增长,reset 也不清理
- **位置**:`stores/command.ts:78-83,110-115`(每次 chunk 追加,整段输出文本常驻内存);对照 `stores/index.ts:80-91`:`reset()` 专门 abort `channelWatchers/threadWatchers`,但 command 的两条流与其缓冲既不在 reset 里 abort 也不设上限。
- **后果**:单页会话内访问过的每个命令的完整 stdout/assistant 文本(可能数 MB/命令)永远泄漏,且长会话多命令下 store 选择器(`activeEvents` 整 map 订阅,`command-detail.tsx:42-43`)放大重渲染。
- **建议**:store 内加 LRU/N 条上限(如仅保留最近 3 个命令、每命令输出 5MB 截断标记),`reset()` 补 abort 语义或提供 `releaseCommand(name)`;组件侧订阅改为 `s.activeEvents[name]`(选择器返回稳定 slice)。

### F-B4 【中】三套合并算法语义分叉导致行键漂移(与 F-R1 同根的 bug 表达)
- **位置**:`command-event-ledger.tsx:362-376` vs `command-event-timeline-overview.tsx:118` vs `command-detail.tsx:265`。
- **触发**:乱序/非单调 ts 时,ledger 合并了某两块而 overview 不合并 → `out-${seqNo}` 行键集合不同 → 选中高亮落空、inspector 打不开或打开错误的块。
- **建议**:同 F-R1(单一实现 + 以 seqNo 为序)。

### F-B5 【中】overview 拖选的两套几何语义不一致,且单击路径依赖事件顺序
- **位置**:`command-event-timeline-overview.tsx:229-231`(pointerup 判定用 **overlap**:`left < end && left+width > start`)vs `:324-327`(渲染期 inSelection 用 **containment**)→ 拖拽过程中与松手后的高亮集合不一致;另外 `handlePointerDown`(196-203)不区分命中目标,点在 span 按钮上也启动拖选,松手先触发 pointerup 的 range 选择、再触发 click 的 `onRangeSelect(null)`(332-336),正确性依赖 pointerup→click 的固定次序。
- **建议**:统一 containment;pointerdown 命中 span 时短路为纯点击;为拖选加 4px 移动阈值防误触。

### F-B6 【中】inspector 的 WARNING 条游离于 tab 机制之外,每个 tab 都重复渲染
- **位置**:`command-event-inspector.tsx:553-557`:该块位于 tab 内容 switch 之外,WARNING 事件的 summary 与 raw tab 底部都会再出现一条告警横幅。
- **建议**:作为 summary tab 的固定 section(类似 UsageOverview 的结构),或并入 `availableTabs` 的类型分支。

### F-B7 【中】`activity-row.tsx:61` 魔数状态判断
- **证据**:`const isDone = activity.state === 3; // ActivityState.DONE` —— 同文件开头已经在用 `Number(ActivityCategory.X)`(19-22),枚举就在 import 范围内却写了裸数字。
- **建议**:`activity.state === ActivityState.DONE`;加 lint(或 biome 规则)防再犯。

### F-B8 【低】`workspace-file-panel` 依赖 `t` 导致切语言重新拉取文件
- **位置**:`workspace/workspace-file-panel.tsx:54-81`(effect deps 含 `t`,因 catch 分支构造 `t("workspace.load-error")` 文案)。
- **建议**:错误文案改为存储 sentinel(如 `error: true`),展示层再翻译;effect deps 去掉 `t`。

### F-B9 【低】html overlay 的 locate 超时定时器与回调映射不随卸载清理
- **位置**:`html-preview-overlay.tsx:126-142`(每次 locateQuote 挂 3s `setTimeout`,无 unmount 清理;`locateCbsRef` Map 不清空);`:287-298` `jumpToComment` 的 `.then` 无 cancelled 防护;`:149-153` flash 定时器同样无 unmount cleanup。
- **后果**:卸载后偶发 setState(React 18+ 无害但脆弱);极端情况下跨文件预览的 locate 回调映射到新 document 的 epoch 校验上(由于 F-S3,同附件重开时 nonce/epoch 复用,老定时器解析出的 rect 会写入新会话的 `located`)。
- **建议**:effect 式清理(一个 `useEffect` 管理挂起的 Map,卸载时逐个 resolve(null) 并 clear 所有 timer);jumpToComment 加本地 cancelled。

### F-B10 【低】`buildHtmlPreviewDoc` 注入点可被注释欺骗,桥静默失效
- **位置**:`lib/html-file.ts:308-325`:`/<html[\s>]/i.test(content)` 对 `<!-- <html> -->` 这类内容误判为完整文档;若同时无 `</head>`/`<body>`,script 被注入到第一处匹配(即注释内)→ 注释里的脚本不执行 → 预览静默丢失桥能力(无选中评论/无链接拦截)。
- **建议**:注入后断言 `doc.includes(script)`(或对 regex 注入结果做 `data-ac-bridge` 存在性检查),失败时回退到 fragment 包裹分支;补该 case 的单测。

### F-B11 【低】`tool-call.tsx` 的 `JSON.stringify` 无 BigInt 保护
- **位置**:`chat-events/tool-call.tsx:83,107`;inspector 已为同类问题写了 `safeStringify`(`command-event-inspector.tsx:100-108`,注释明说 protobuf int64 是 BigInt)。
- **建议**:统一走 `safeStringify`(挪进 lib),两处共用。

### F-B12 【低】markdown 大纲单 rAF 采集,依赖渲染完成时机
- **位置**:`markdown-preview-overlay.tsx:57-72`(rAF 后 `buildOutline(contentRef.current)` + `getElementById(...).scrollIntoView`);markstream 是分片渲染(`batchRendering`),超长文档一帧后标题 DOM 可能尚未齐 → 大纲偶发为空 / 跳转落空。
- **建议**:改由 markstream 的渲染完成回调驱动(若有),或 ResizeObserver 到 DOM 稳定后重建;至少在无标题时提示而非静默。

---

## 6. 性能问题(维度 4)

### F-P1 【高】流式 chunk 每条触发一次全链重算,长会话 O(n²) 累计
- **位置**:`stores/command.ts:74-84`(每 chunk 一次 `set`)→ `command-detail.tsx` 三个重组件 `useMemo`(144-150, 214-222, 228-280,每个都是全量遍历/排序)→ ledger `rows`(内含 `pairToolCallEvents` + 全量排序 + 合并,`command-event-ledger.tsx:277-385`)→ overview `spans`(`overview.tsx:65-186`,又一次 pair + 排序 + 合并)。
- **分析**:一次 `pairToolCallEvents` + 排序 + 合并本来无害;但**同一份数据在一次 render 里被做 3-4 遍**,且每来一个 chunk 全部重来。1000+ chunk 的长会话(每 token flush 的 pi 即如此)必然进入可感知卡顿区间。
- **建议**:① store 端合批(rAF/32ms 窗口攒 chunk 再 `set`,一帧最多一次重算);② 把 pair/merge/排序放进步骤 §7 的归一化模块,配合 `useMemo([modelVersion])` 只算一次,ledger/overview/inspector 共享同一 `TimelineModel`;③ 订阅粒度见 F-B3。

### F-P2 【高】ledger 无虚拟化 + 输出行无高度/大小上限
- **位置**:`command-event-ledger.tsx:400-498`(`<table>` 全量 `rows.map`);`OutputContent`(232-238)把合并后的整段输出渲染为单个 `whitespace-pre-wrap` 节点,无任何截断——一次 10MB stdout 就是一个 10MB 文本节点;表格行无 max-height,浏览器 layout 成本爆炸。仓库(`package.json`)无任何虚拟化依赖。
- **建议**:`@tanstack/react-virtual` + 固定行高(标签列/内容列已适配两列布局);输出行截断(如 100KB,带"展开"走 inspector OutputRaw);大文本行加 `content-visibility: auto` 兜底。

### F-P3 【中】搜索无防抖、无小写缓存,内容级 O(全输出)每键触发
- **位置**:`command-event-ledger.tsx:92-95`(`matchesSearch` 对 `searchText` 每行 toLowerCase;输出行的 searchText 是**完整输出内容**,见 290-306 与合并循环 372)+ deps 含 `searchQuery`(385)。
- **建议**:SearchInput 自带 debounce;model 预计算小写 searchIndex(只在内容变化时构建一次);大文本行 searchIndex 只取前 4KB + 尾部 1KB。

### F-P4 【中】overview 每个 span 一个绝对定位 `<button>`,等宽序数布局上限低
- **位置**:`command-event-timeline-overview.tsx:260-262,320-350`:DOM 节点数 = 行数(数千 span 时不可用);`width = max(0.5%, step-gap)`(262/322)在 >200 span 后全部 0.5% 挤在一起,不可点也不可辨。
- **建议**:短期按 span 数截断(>500 时聚类成 "…N more");中期改 SVG/canvas 渲染或**真实时间轴**(以 ts 线性分位而非序数等宽,顺带解决"两个相邻分钟事件被等宽展示"的失真),支持 zoom。

### F-P5 【中】inspector 输出预览全量渲染 Markdown
- **位置**:`command-event-inspector.tsx:357-369`:`MarkdownRender` 无 `batchRendering`/`deferNodesUntilVisible`(对照 `markdown-preview-overlay.tsx:179-186` 两个都开了);merged ASSISTANT 输出可达数百 KB。
- **建议**:与 md-preview 对齐参数;超长内容(>200KB)直接跳到 raw 视图。

### F-P6 【低】杂项
- `command-event-ledger.tsx:385`:`t` 在 memo deps,切语言全量重建行(无意义,搜索文本不含翻译)。
- `command-event-ledger.tsx:462`:`rangeKeys.includes(row.key)` 在行渲染里 O(n×k),改 Set。
- `agent-teams-manager.tsx:45-47`:`pageSize:1000` 无分页 UI,静默截断。
- `activity-list.tsx:126`:5s 轮询无页面可见性门控。
- 预览评论 aside 重开即全量重定位所有评论引用(`html-preview-overlay.tsx:266-284` + 发布新评论后 effect 重跑全量 locate;每次 locate 桥内最多走 5000 节点 × O(n²) contains 匹配,`html-file.ts:186-207`)——建议按 m.id 增量缓存,已定位的跳过。

---

## 7. 重新设计:统一事件渲染管线(维度 6)

现状里"同一份命令事件流"被 chat(message-row)、command 详情(ledger/overview/inspector)、dead timeline 四个表面各自消化,消化逻辑(pair/merge/kind/lane/search)有 3-4 份拷贝。重设计应按"协议 → 模型 → 原语 → 表面"四层收敛:

```
┌ L5 表面层  chat message-row │ command-detail │ activity │ workspace preview
├ L4 原语层  <EventTag> <ToolCallCard> <TimelineRow> <EventInspector> <TimelineOverview> <CommentsPanel>
├ L3 状态层  store: 流订阅(合批 flush + 断线重连 + LRU)→ TimelineModel(版本号)
├ L2 归一化层 lib/command-events-model.ts(纯函数,100% 单测)
│     normalize(events, outputs) → { rows, pairs, spans, searchIndex, usage }
│      · 唯一 pair 实现(ID 优先,FIFO 兜底)
│      · 唯一 merge 实现(seqNo 全序)
│      · 唯一 EventKindRegistry:i18n/icon/tagClass/textClass/phase/lane/isInternal
└ L1 契约层  proto:ToolCall*.tool_call_id;输出统一视为 kind 事件(stdout/stderr/system/assistant)
```

**落点与对应发现**:
1. **kind 注册表**(解 F-D6/F-R3/F-S4-lane):`{ labelKey, icon, tagClass, phase, lane, isInternal }` 单对象;`isInternal` 取代散落的 `CONTEXT_USAGE_UPDATE/RAW_ACP/TEXT_DELTA` 排除(ledger:310-311、overview:83-89、detail:238-246、`isVisibleEvent` 四处各写一遍)。
2. **行键与配对协议**(解 F-B1/F-B4):`tool_call_id` 进 proto;行键 `rowKey = type:anchorSeq` 保持唯一空间注释(ledger:24-25 的警告保留)。
3. **状态层**(解 F-B2/F-B3/F-P1):watch 生命周期、重连、合批、LRU 全部内聚;页面只消费 `useCommandTimeline(cmdName)` 拿稳定 `TimelineModel`。
4. **原语层**:`<EventRow>`(虚拟化列表行)、`<EventInspector>`(tab 逻辑数据驱动)、`<TimelineOverview>`(真实时间轴 + zoom)、`<CommentsPanel>`(F-R4)与 `useHtmlPreviewBridge`(F-R6)、`FilePreviewShell`(F-R5)。chat 内联与 command 全景消费同一 model,只是 `variant` 不同。
5. **bridge 协议类型化**:消息 union(`state|selection|selection-cleared|located|link-clicked|esc`)集中 `lib/html-bridge.ts`,父端 hook 统一校验链 + 请求超时管理(解 F-B9/F-S2 的父端半边)。
6. **测试资产保留**:现有三个模块的测试(ledger 合并/滚动/区间、overview 拖选、inspector tabs)质量不错,重构时先平移到新 model 的纯函数测试上,再重建组件测试。

---

## 8. 本模块重构优先级清单(按性价比排序)

| # | 动作 | 主要位置 | 预估规模 | 风险 | 性价比依据 |
|---|------|----------|----------|------|-----------|
| 1 | **删除死代码**:command-timeline.tsx、CommandTerminal(保 FinalSummary 平移)、use-auto-scroll.ts、isToolEvent、commandEventTypeToI18nKey、commandEventKindExtra、inspector timing 死分支 | §2 全部 | 删 ≈350 行 | 零风险、立即收益 | 纯减法,先清场 |
| 2 | **`window.open` scheme 白名单**(统一 `safeOpenExternal`) | html-preview-overlay.tsx:218-222、html-file-view.tsx:35-38 | ~20 行 + 测试 | 堵住唯一可利用 XSS 面 | 安全问题,最高优先 |
| 3 | **watch 断线重连 + 合批 flush + LRU**(重构 store 层) | stores/command.ts、command-detail.tsx:124-134 | ~120 行 | 解决"页面永久停更"与内存无上限 | 正确性 + 性能双收益 |
| 4 | **建 `lib/command-events-model.ts`**:唯一 merge/pair/tsToMs/kind 注册表/lane 映射 | 替换 §3 F-R1/F-R2/F-D6 四份拷贝 | ~250 行 + 迁移 | 消灭行键漂移这类隐性 bug 的总根源 | 后续一切重构的地基 |
| 5 | ~~**proto 加 `tool_call_id`** + 配对改 ID 优先~~ | proto/v1/v1/command.proto、tool-call-events.ts | proto + ~40 行 | 根治并发错配 | ✅ 批 9 完成(`68e53a6`/`69396f8`/`baf4167`) |
| 6 | **统一工具卡与状态语义**(`<ToolCallCard variant>`;修 chat 侧 error 显示灰) | chat-events/tool-call.tsx、ledger、inspector、overview | ~150 行 | 用户可见的误导性 bug 修复 | 高可见度收益 |
| 7 | **评论面板双胞胎合并 + FilePreviewShell + useHtmlPreviewBridge** | preview/ 三文件 + workspace/html-file-view.tsx | −≈300 行 | 消除最大复制粘贴块,协议演化单点 | 重构期顺手完成 |
| 8 | **ledger 虚拟化 + 输出行截断 + 搜索 debounce/索引** | command-event-ledger.tsx + toolbar | ~200 行 | 长会话可用性关键 | 性价比最高的性能项 |
| 9 | **overview 改真实时间轴 + span 上限**(≥500 截断) | command-event-timeline-overview.tsx | ~120 行 | 修语义失真 + DOM 规模 | 依赖 #4 |
| 10 | **workspace 树虚拟化 + a11y + 树内搜索** | workspace-tree.tsx | ~150 行 | 大工作区可用性 | 中等规模收益 |
| 11 | **杂项修复包**:activity 魔数/可见性/双分页收敛(✅ 批 6 F-B7 + 批 8 迁 Query + 批 10 `29be54e` 收敛)、workspace-file-panel `t` 依赖(✅ 批 6 F-B8)、inspector WARNING 游离块、buildHtmlPreviewDoc 注入断言、locate 定时器清理 | §5 各条 | ~80 行 | 各自独立小修 | 批量清偿 |
| 12 | **AgentSelect → Combobox;toolbar → SearchInput;SidePanel 壳统一** | agent-team-form.tsx、toolbar、inspector/aside | ~200 行 | 规范对齐 + a11y | 视 UI 改版档期 |

**不建议做的**:为 activity 桌面分页补齐更多能力(✅ 已被批 10 收敛取代:统一无限滚动,分页方案不复存在);给 chat 侧 message-row 大改(其 memo 结构已较成熟,属另一章范围)。

---

### 附:本次审查覆盖清单(文件与行数)

| 模块 | 文件 | 行数 |
|---|---|---|
| command-events | command-event-inspector.tsx / ledger / timeline-overview / toolbar / command-event-kind.ts | 563 / 501 / 356 / 80 / 246 |
| preview | html-preview-overlay / markdown-preview-overlay / comments-aside / html-comments-aside / image-preview-overlay / attachment-comment-card | 500 / 255 / 279 / 282 / 91 / 66 |
| agent | agent-team-form / agent-teams-manager / key-value-env-editor / string-list-editor | 416 / 238 / 75 / 67 |
| activity | activity-list / activity-row | 333 / 242 |
| workspace | workspace-file-panel / workspace-tree / html-file-view | 156 / 253 / 65 |
| chat-events | tool-call / diff-view / warning | 115 / 81 / 18 |
| 对照 | command-timeline.tsx(死)/ command-terminal.tsx / command-detail.tsx(主消费)/ message-row.tsx(相关段)/ 各 store 与 lib / proto 契约 | 已读 |