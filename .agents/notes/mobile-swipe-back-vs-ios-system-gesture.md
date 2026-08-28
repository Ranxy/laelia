# 移动端滑动返回与 iOS 系统边缘手势的三层冲突:分析、方案与实现

- 日期:2026-02(本轮会话)
- 影响面:聊天模块全部移动端浮层 + 路由级返回手势
- 状态:已实现并通过模拟器/单元测试验证;真机验证由使用者完成(路由级返回、抽屉关闭已确认正常)

## 一、问题

应用实现了一套 iOS 风格的合成滑动返回手势(`use-swipe-back.ts` / `use-swipe-to-close-sheet.ts`):

- 路由级:聊天页 → 首页,当前页跟随手指平移,底部渲染 backTo 目标路由的预览;
- 浮层级:thread 面板、任务看板(全屏面板,`--swipe-offset` CSS 变量驱动)、成员/搜索/文件抽屉与 mention 详情(Sheet + 遮罩淡出)。

桌面浏览器的移动端模拟里一切正常:上层是当前页/浮层,下方是返回目标。**但在真机(iPhone 的 Safari 和 Edge)上出现"三层"结构**:被拖动的当前页(顶)、返回目标首页(中)、又一层返回目标首页(底)。

演进过程(三次报告):

1. 路由级返回(聊天 → 首页)出现三层;
2. 第一版"贴边守卫"(仅让渡 clientX ≤ 3px 的触摸)后,**时好时坏**——证明系统识别器的识别区深入视口内部,远超 3px;
3. 路由级让渡修复后,抽屉关闭又出现同样的三层——覆盖层需要不同机制。

## 二、根本原因

### 2.1 关键事实(按排查顺序)

1. **DOM 里只渲染了一个目标页**。滑动期间应用 DOM 中返回目标只存在一份(路由预览 / 遮罩下露出的页面)。因此"多出来的一层目标页"不可能来自应用 DOM,只能来自浏览器进程自己合成的图层。

2. **真机 iOS 存在系统级边缘滑动识别器**。iOS Safari 及所有 App Store 浏览器(均为 WKWebView)在浏览器 UI 进程运行 `UIScreenEdgePanGestureRecognizer` 驱动的返回/前进手势(`WKSwipeTransitionController`,见 WebKit 源码 `ViewGestureControllerIOS.mm`)。其识别区从屏幕边框(bezel)延伸到视口内约 20–27pt,**与应用手势的整个起手区(≤24px)重叠**。

3. **网页内容无法阻止它**:
   - WebKit 官方确认 preventDefault / CSS 均无效(bug 240892,「Neither preventDefault on GestureEnd nor CSS overscroll stop the Safari page navigation」);
   - 系统手势认领触摸后,页面**收不到可靠的 touchcancel**(bug 136531,自 iOS 7 起已知且未修复;注释 2 提到计划「gesture 失败前不下发事件」但未落地)。

4. **系统手势的转场自带一层"上一页快照"**。手势启用时,WebKit 以交互转场的方式合成:当前页(实时视图,含我们已做的 DOM 位移与已挂载的预览)作为滑出的卡片,**上一条历史记录的快照**作为下方图层。上一条历史 = 首页 = 与我们预览的相同目标 → 屏幕上出现:被拖动的页 + 我们的预览(被夹在"中间")+ 系统的上一页快照(底部)= 三层。

5. **模拟器里没有系统识别器**——桌面 DevTools 设备模拟只伪造 UA/触点数,不实现 UI 进程手势,因此只有应用的两层,一切正常。这也解释了"模拟器正常、真机异常"。

### 2.2 佐证(业界同类问题)

- Flutter #114324:「从屏幕外缘起滑时转场中出现多余的页」;#184284:「iOS 浏览器上返回转场动画播放两次」。
- Framework7 论坛:「swipe back animates twice on iOS Safari」。
- 结论:任何"页面内合成边缘手势"都会与 WebKit 系统手势竞争,无法从网页侧消除系统手势本身,只能避免与之同触。

## 三、方案

核心原则:**应用合成手势与系统手势绝不在同一个触摸上同时运行**。按"系统手势是否存在"与"浮层/路由"两类语义分流。

### 3.1 平台检测 —— `frontend/src/lib/platform-edge-swipe.ts`

```ts
platformOwnsEdgeSwipe():
  navigator.vendor === "Apple Computer, Inc."   // WebKit 引擎;Blink 恒为 "Google Inc."
  && navigator.maxTouchPoints >= 2              // 排除桌面 Mac Safari(无触摸屏)
  && !(navigator.standalone || display-mode: standalone)  // PWA standalone 无系统手势
```

- 真实 iOS/iPadOS 浏览器(Safari + WKWebView 系 Edge/Chrome 等)→ true;
- **DevTools 模拟无法伪造**:引擎不变,`vendor` 保持 "Google Inc." → 模拟器保留应用手势作为开发/测试台;
- jsdom 也报告 Apple vendor(WebKit 规范实现),但 `maxTouchPoints` 为 undefined → `?? 0` 守卫使其回落到 inert(此坑曾让测试环境误判,见 3.6)。

### 3.2 路由级返回:整段让渡(`use-swipe-back.ts`)

真实 iOS 浏览器上,路由级合成手势**完全禁用**(起手区内任何触摸都让渡),系统原生返回转场接管:单层干净、目标即上一条历史记录(标准导航流里 == backTo)。深链直入(无历史,`history.state.idx === 0`)时系统手势无事可做,应用手势保留完整边缘区。Android 上贴边 3px(bezel)内的触摸让渡(系统手势导航在浏览器之下认领);`touchcancel` 改为瞬时复位(避免在系统转场下面再播回弹动画)。

### 3.3 浮层关闭:历史哨兵 —— `frontend/src/lib/use-history-sentinel.ts`

浮层(抽屉/thread/任务看板/mention 详情)不能简单让渡:系统手势的提交目标是**上一条历史记录(首页)**,会让用户"关个抽屉却被送回首页"。解法是移动端 Sheet 的标准模式——**哨兵条目**:

- 浮层打开时压入一条**同 URL 的重复历史记录**(带每层唯一的 token 标记 `laelia.historySentinel`);
- 系统滑动的转场目标于是变为该条目——其快照是**浮层不存在时的页面**(入栈于浮层打开之前)→ 原生转场呈现"浮层滑出、露出页面",无多余图层;
- 提交弹出哨兵 → `popstate` → **关闭浮层而非离开页面**;提前松手原生回弹,浮层保持打开;
- 浮层经自身 UI 关闭时,清理逻辑延迟一拍(0ms)弹出哨兵平衡栈;StrictMode 重挂载/快速开关会**取消**这笔延迟 pop 并复用哨兵(无竞态);
- 堆叠浮层(抽屉内再开成员详情)各持有独立 token,一次 pop 只关闭"哨兵被消费的那一层"(LIFO)。

### 3.4 全屏面板:合成手势(非 iOS)+ 哨兵(iOS)

thread 面板与任务看板是全屏面板(非 Sheet)。`use-swipe-back.ts` 的全屏面板模式(thread 模式,`--swipe-offset` CSS 变量驱动)同时覆盖两者;真机 iOS 上该模式让渡,由哨兵 + 系统转场接管。

### 3.5 各表面行为矩阵

| 表面 | 真实 iOS 浏览器 | 模拟器 / Android(视口内)/ PWA standalone |
| --- | --- | --- |
| 路由级返回(聊天 → 首页) | 系统原生返回(让渡) | 合成手势(平移 + 目标预览) |
| 抽屉 ×3(`ChatDrawerSheet`) | 系统滑动 + 哨兵关闭 | 合成手势(平移 + 遮罩淡出)+ 哨兵(返回键也可关闭) |
| thread 面板 | 同上 | 合成手势(`--swipe-offset`) |
| 任务看板(`TasksPanel`) | 同上 | 同上 |
| mention 详情浮层 | 同上 | 合成手势(平移 + 遮罩淡出) |
| 堆叠(抽屉 + mention) | pop 关闭最上层,token 区分归属 | 同左 |

### 3.5.1 附带修复:TasksPanel 移动端全屏化

任务看板原来是 420px 右侧 dock,手机上被视口裁掉右侧 45px——**关闭按钮不可见**。现与 ThreadPanel 一致:移动端 `fixed inset-0 z-panel` 全屏覆盖(保留安全区内边距),桌面 `lg:` 变体恢复 420px dock;面板挂载时经 `useSwipeBack` 的全屏面板模式接入合成手势,提交调用 `closeTasksPanel(会话)`。

### 3.6 排查中踩过的坑(记录给后来者)

1. **jsdom 伪造了 Apple 身份**:`navigator.vendor === "Apple Computer, Inc."` 在 jsdom 中为真,且 `maxTouchPoints` 为 `undefined` —— `undefined < 2` 为 false,守卫失效,导致测试环境误判"真机"。修复:`(navigator.maxTouchPoints ?? 0) < 2`。
2. **`useSwipeBack` 的 mock store 缺字段**:hook 新增 `tasksPanelOpen`/`closeTasksPanel` 读取后,测试的 store stub 必须同步补齐,否则 `Object.entries(undefined)` 抛错。
3. **fake timers 与 `findBy*` 死锁**:`vi.useFakeTimers()` 要放在 `await screen.findByText(...)` 之后,否则 waitFor 的轮询定时器永不推进。
4. **jsdom 的 history.back()**:真实遍历异步且不可靠,测试统一 spy `history.back` + `vi.advanceTimersByTime` 断言,不做真实遍历。

## 四、涉及文件

| 文件 | 职责 |
| --- | --- |
| `frontend/src/lib/platform-edge-swipe.ts` | 平台检测(新增) |
| `frontend/src/lib/use-history-sentinel.ts` | 哨兵 hook:token 化、堆叠归属、StrictMode 安全(新增) |
| `frontend/src/lib/use-swipe-back.ts` | 路由让渡 + bezel 守卫 + thread/tasks 面板模式 + touchcancel 瞬时复位 |
| `frontend/src/lib/use-swipe-to-close-sheet.ts` | Sheet 合成手势增加 `enabled` 选项 |
| `frontend/src/components/chat/chat-drawer-sheet.tsx` | 抽屉:哨兵 + 让渡 |
| `frontend/src/components/chat/thread-panel.tsx` | thread:哨兵(覆盖聊天/活动/提醒三处嵌入) |
| `frontend/src/components/chat/mention-detail-sheet.tsx` | mention 详情:哨兵 + 让渡 |
| `frontend/src/components/chat/tasks-panel.tsx` | 任务看板:移动端全屏化 + 哨兵 |
| `frontend/src/pages/dashboard/chat-conversation.tsx` | 三个抽屉换用 `ChatDrawerSheet` |
| 测试 | `use-history-sentinel.test.tsx`、`chat-drawer-sheet.test.tsx`、`use-swipe-back.test.tsx`、`tasks-panel.test.tsx`(jsdom 需 IntersectionObserver stub) |

## 五、已知的取舍与遗留

1. **真机上路由级返回的自定义预览被原生转场替代**:视觉等价(当前页滑出、下方露出目标),但自定义的预览渲染/阈值手感不再生效——这是消除竞争的唯一无伪影方案。
2. **看板 ↔ thread 互切会遗留一条孤儿哨兵**:看板打开 → 点进任务 thread,两个哨兵一上一下;关 thread 后栈底仍有一条哨兵条目,导致**一次"空滑"/多按一次返回**(同路径 pop,页面无变化)。自愈、无害,但可感知。彻底消除需要跨浮层的哨兵移交(当前未做)。
3. **任务看板此前无滑动关闭手势**,本轮一并补上;若不需要移动端全屏化,可回退 aside 类名为原 420px dock(哨兵机制不受影响)。
4. **图片/Markdown 预览浮层**(store 驱动)没有滑动关闭手势,真机上边缘滑动会原生返回离开页面——行为干净(无叠加)但语义是"离开页面",暂未处理。

## 六、真机验证清单

- [ ] 聊天页 → 边缘滑回首页:单一原生转场,无中间层;
- [ ] 抽屉(thread/任务看板同理)打开 → 边缘滑动:浮层滑出露出聊天页,松手关闭且**留在聊天页**;
- [ ] 提前松手:浮层/页面回弹,不导航;
- [ ] 抽屉内点开成员详情 → 一次滑动关详情、再一次关抽屉;
- [ ] 看板 → 点进任务 thread → 滑动关 thread(注意:其后可能有一次"空滑",见遗留 2);
- [ ] 模拟器(DevTools):所有合成手势行为与之前一致(测试台不受影响);
- [ ] 深链直入聊天页(无历史):边缘滑动仍可用应用手势返回。

## 七、参考

- WebKit bug 240892:preventDefault/CSS 无法阻止 Safari 导航手势
- WebKit bug 136531:bezel 返回导航期间 touchend/cancel 不派发
- WebKit 源码:`Source/WebKit/UIProcess/ios/ViewGestureControllerIOS.mm`(WKSwipeTransitionController / UIScreenEdgePanGestureRecognizer / 快照转场)
- Flutter #114324(边缘起滑出现多余页)、#184284(转场播放两次)
- React Router data router:`history.state.idx` 用于判断是否存在可返回的应用内历史