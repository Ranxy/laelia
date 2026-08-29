# 前端组件层深度审查报告:`components/ui/` + `components/` 根目录

> **⚙ 实施进度标注(批 3 收口后)**
- ✅ 已完成:死代码清单全部删除(tooltip/separator/command-timeline/CommandTerminal/MobileSidebar/variants 导出/Sheet 六档,`b0499db`);ModelCombobox portal 化(`75d844b`);text-danger ×13 + 裸色语义化(`4c2d37c`);TableHead 排序/resize 竞态与 Checkbox 冗余分支(`5bc9f90`)。
- ⏳ 未完成:StatusBadge 五胞胎与 modal 壳/弹层收敛;destructive→error 与 size 词典 codemod;Avatar.sizeClass 显式映射;语义 token 收尾(dark-bg/matrix-green、copyable 之外残余);tabbar RouterLink;Separator 去留决策。

> 审查范围:`frontend/src/components/ui/` 全部 29 个源文件 + 10 个测试文件(ui 源码实计 2349 行),以及 `frontend/src/components/` 根目录全部 22 个 .tsx(2078 行,含 sidebar.tsx 514 行)。所有引用结论均在全仓库 grep 验证(含 pages/、app/、lib/、components/ 子目录)。技术栈:React 19.2 / Zustand 5 / @base-ui/react 1.3 / Tailwind 4 / Biome。

---

## 0. 总体结论(TL;DR)

这不是一份烂代码报告:ui 层的主体(Base UI 包装族、cva 变体、layer 分层体系)质量高于平均 shadcn 项目,且有成文规范(frontend/AGENTS.md)和守卫脚本(scripts/check-react-layering.mjs)。**真正的问题是三类**:

1. **至少 300 行已验证的死代码**隐藏在核心目录里(tooltip、separator、command-timeline、CommandTerminal、MobileSidebar),且与成文规范自相矛盾;
2. **API 约定靠惯例而非机制**:size/variant 命名三套并存、cn 拼法三种并存、ref 策略两代并存(ref-as-prop vs forwardRef);
3. **一个真实的功能 bug**:ModelCombobox 的 `absolute z-50` 弹层在 SheetBody(`overflow-y-auto`)内被裁剪,违反自家 layering 政策。

---

## 1. 组件质量(components/ui/)

### 1.1 基因判定:shadcn 风格的"两级混合体"

ui 目录是标准 shadcn 演化路径(Base UI 原语 + cva + cn),但内部分成清晰的两级:

- **原语包装族(健康,shadcn 式 compound API)**:Dialog、AlertDialog、Sheet、Select、Popover、DropdownMenu、ContextMenu、Tabs、Checkbox、Switch、RadioGroup、Toast/Toaster。共同模式:re-export Base UI 复合组件 + `ComponentProps<typeof Base.*>` 类型透传 + `ref` 解构转发 + layer portal。
- **自研件(风格各异,shadcn 三件套缺失)**:Tooltip(17 行裸 div)、Separator(27 行裸 div)、ModelCombobox(166 行手写 listbox)、FieldRow、SearchInput、ColumnResizeHandle、NumberInput、SecretInput、Alert、toast 卡片。

自研件里只有 Alert/Badge/Button/Input/Textarea/SheetContent/NumberInput 用了 cva;FieldRow、Separator、Tooltip、ColumnResizeHandle、SecretInput 完全无变体系,风格与原语族断裂。

### 1.2 未使用组件(grep 全仓库验证,0 引用)

| 文件 | 行数 | 外部引用 | 证据 | 建议 |
|---|---|---|---|---|
| `ui/tooltip.tsx` | 17 | **0** | 全仓库仅 `components/ui/tooltip.tsx` 自身出现 `Tooltip` 字样(其余命中均为 i18n key 或注释) | 删除。悬浮提示现状由原生 `title=` 属性承担(message-row.tsx:848 等) |
| `ui/separator.tsx` | 27 | **0** | 全仓库无 `from "@/components/ui/separator"`;仅 ContextMenu/Dropdown 菜单内部分隔符(自带的 `ContextMenuSeparator`)被使用 | 见下 |

**矛盾点(先决决策)**:frontend/AGENTS.md 明文规定 "**Use `Separator` not `<hr>` or `border-t` divs**",但实际是 **20 个文件手写 `border-t border-control-border`**(grep 验证),Separator 从未接线。二选一:①承认规则作废 → 删除 separator.tsx;②落实规则 → 保留并做一次 20 处替换。不要维持"规范要求用、实际没人用"的漂移状态。同理 Tooltip:sidebar 折叠态(`collapsed && "justify-center px-2"`,sidebar.tsx:368)没有 hover 提示,说明 Tooltip 不是不需要,而是从未做。

### 1.3 API 设计不一致(重构核心痛点)

**F-1|中|size 命名三套并存**
- 位置:`ui/button.tsx:22-29`、`ui/input.tsx:16-26`、`ui/checkbox.tsx:6`、`ui/switch.tsx:4`、`ui/sheet.tsx:63-76`
- 证据:Button 同时有 `default` 和 `md` 两个完全相同的键("default is an alias for md");Checkbox 只有 `sm|md`;Switch 用 `xs|sm|md|lg`;Input/Textarea/NumberInput/SelectTrigger 用 `xs|sm|md|lg`;SheetContent 用另一套语义 width 档位 `narrow|panel|medium|standard|wide|large|xlarge|huge|workspace` 共 9 档。
- 实际使用(grep 全部 19 处 `<SheetContent`):`medium`×9、`standard`×8(3 处默认)、`wide`×1;**narrow/panel/large/xlarge/huge/workspace 六档 0 使用**。
- 建议:size 统一为 `xs|sm|md|lg`,删除 Button 的 `default` 别名;Sheet 收敛为 3 档 `standard|medium|wide`。

**F-2|中|variant 色彩词汇分裂:destructive vs error**
- 位置:`ui/badge.tsx:12`(destructive)、`ui/alert.tsx:20`(error)、`ui/toast.tsx:33`(error)、`ui/button.tsx:19`(destructive)
- 证据:`ConnectionBadge`/`MachineConnectionBadge` 都在 switch 里写 `variant="destructive"`,而 Alert 家族写 `error`;`lib/command-status.ts` 的 `BadgeVariant` 类型同时收录两者。
- 建议:统一为一套语义色枚举(建议 `default|secondary|success|warning|error|info`),Badge 的 `destructive` 重命名迁移(codemod 一次 replace)。

**F-3|低|cn 组合姿势三种**
- Button/Badge/Alert 把 className 塞进 cva 调用:`cn(buttonVariants({ variant, size, className }))`(button.tsx:45);Input/Textarea/Select/Dialog 在外面拼:`cn(inputVariants({ size }), className)`(input.tsx:36);Checkbox 条件式:`!onClick && className`(checkbox.tsx:64)。
- 三者对 tailwind-merge 的结果等价,但 review/生成代码时无范式可循。建议统一为 `cn(cvaFn({ ...variants, className }))`。

**F-4|低|ref 策略两代并存**
- `ui/search-input.tsx:13` 仍用 `forwardRef<HTMLInputElement, ...>`;其余组件用 React 19 `ref` prop 解构;而 Tooltip/Separator/RadioGroup/Table 系用 `{...props}` 隐式 spread(React 19 下可用,但不可见、不可静态检查)。
- 建议:全库统一 ref-as-prop 显式解构 `({ ref, ...props })`;删除 search-input 的 forwardRef;禁止依赖 spread 隐式传 ref。

**F-5|低|unused 变体导出**
- 位置:`ui/button.tsx:52`、`ui/badge.tsx:32`、`ui/alert.tsx:114`
- 证据:grep 全仓库,`buttonVariants`/`badgeVariants`/`alertVariants` 三个对象 0 外部引用(仅自引用)。
- 建议:删除导出,只导组件。

**F-6|中|Tooltip 缺失导致 hover 语义降级**
- 全库无 tooltip 机制,只能原生 `title`。侧栏折叠态、icon-only 按钮等场景 a11y 信息依赖 aria-label 而无视觉提示。
- 建议:要么删除 tooltip.tsx 并接受 title=,要么基于 Base UI Tooltip 原语重建 ~30 行的 `Tooltip`(portal 到 overlay layer、delay、无裁剪)。当前 17 行实现两头都不占。

### 1.4 样式拼接方式

- 绝大多数经过 `cn()`(clsx+tailwind-merge),健康;唯一残留是 `combobox.tsx:124` 直接单字符串 className(配合 F-Bug-1 一并重写)。
- 模板字符串插值 layer class:`dialog.tsx:27`、`sheet.tsx:38`、`alert-dialog.tsx:24` 的 `` cn(`fixed inset-0 ${LAYER_BACKDROP_CLASS} ...`) ``。功能正确(结果仍进 cn),但违反 frontend/AGENTS.md "Do not hide raw global overlay classes in … interpolated template literals" 的精神。建议在 layer.ts 提供 `LAYER_BACKDROP_SET = "fixed inset-0 z-10 bg-overlay/50"` 复合常量,三处共用(见 §3 F-D3)。

---

## 2. 根目录组件

### 2.1 sidebar.tsx(514 行)——五个职责塞一个文件

- **位置:** `components/sidebar.tsx`
- **严重度:** 中
- **描述:** 单文件承担 ①导航配置(12 个权限 hook + 33 项 i18n + 路由常量,`useSidebarItems`:100-264,单条 useMemo 依赖 10 项);②激活路由启发式(`getItemClass`:64-94,含 `.list` 前缀匹配和 MEMBERS↔COMMAND 特判两条业务规则硬编码);③展开状态机(`expandedSet` + `manualToggledRef` + `autoExpandedRef` 三个可变容器互相纠缠,297-351);④两种布局渲染(`SidebarNav`);⑤两个外壳(`DesktopSidebar`/`MobileSidebar`)。
- **证据:** 章节 banner 注释 5 个(`// Types` ~ `// Mobile sidebar overlay`);`renderItem` 是组件体内递归函数,`_depth` 参数带下划线却在 385 行实际使用(`renderItem(child, _depth + 1)`),参数残留。
- **状态来源与耦合:** `collapsed` 由 `app/layouts/dashboard-layout.tsx:80-91` 从 localStorage(`laelia-sidebar-collapsed`)读写,经 props 下传;权限经 `stores/permissions`(zustand);无其它 store 耦合 —— **耦合面可控,这是该文件唯一的好消息**。
- **重构建议:** ①拆为 `sidebar/items.ts`(纯配置+权限)+ `sidebar/nav.tsx` + `sidebar.tsx`(外壳);②权限 hook 列表收敛为 `[perm, key][]` 配置 map,消灭 12 行 hook 串联;③展开状态砍掉 manualToggled/autoExpanded 双 ref 的反射逻辑(28 行换来的行为是"路由变化时自动展开,手动收起后路由再次变化又弹回",可用单一 `expandedSet` 表达);④MEMBERS 特判上移到路由 handles。

### 2.2 command-timeline.tsx(200 行):已验证死代码

- **位置:** `components/command-timeline.tsx:57`(export `CommandTimeline`)
- **严重度:** 高(纯维护税)
- **验证过程:** `grep -rn "CommandTimeline"` 全仓库仅命中自身定义与 `lib/use-auto-scroll.ts:5` 的一句**注释**。它渲染的"输出流 + 工具卡片时间线"已被 `components/command-events/` 家族取代 —— 该家族唯一消费方是 `pages/dashboard/command-detail.tsx:4-11`(inspector/ledger/timeline-overview/toolbar,共 1700+ 行,全部活跃)。
- **与 command-events/ 的关系:** 不是被它引用,而是被它**替代**。共享底层(`pairToolCallEvents`:另有 4 个活跃消费方;`ChatToolCall`:被 `chat/message-row.tsx:29` 使用)不受删除影响。
- **证据补充:** 组件的 `scrollToSeqNo`/`active` prop 注释(command-timeline.tsx:18-25)描述的是早已不存在的调用场景。
- **建议:** 整文件删除(200 行),`useAutoScroll` 注释同步更新。

### 2.3 command-terminal.tsx:半死(仅 FinalSummary 存活)

- **位置:** `components/command-terminal.tsx:13`(`CommandTerminal`)vs `:60`(`FinalSummary`)
- **严重度:** 中
- **验证:** 消费方仅 `command-detail.tsx:13`、`command-list.tsx:6` + 1 个测试 mock,**全部只 import `FinalSummary`**。`CommandTerminal` 组件本体(13-53 行)0 引用。
- **建议:** 删除 `CommandTerminal`;`FinalSummary` 是 10 行 markstream 包装,不值得独占 "terminal" 命名的文件 —— 移入 `lib/markdown.tsx`,删除本文件。

### 2.4 sidebar.tsx 的 MobileSidebar:已验证死代码 + 质量问题

- **位置:** `components/sidebar.tsx:465-514`(`export function MobileSidebar`)
- **严重度:** 中
- **验证:** `grep -rn "MobileSidebar"` 全仓库仅命中定义处(dashboard-layout.tsx:17 只 import 了 `DesktopSidebar`)。
- **质量问题(若保留复活):** 无 focus trap、无 Escape 关闭、无 inert/aria-hidden 处理(对比 Sheet 走 Base UI Dialog 全套);backdrop 是裸 `<button>`;`open=false` 仅 `pointer-events-none`,键盘 Tab 仍可进入隐藏面板。
- **建议:** 直接删除(50 行);未来需要移动端侧栏时复用 `Sheet`。

### 2.5 user-menu.tsx:hooks 复用是亮点

- `useDebugConfig`/`useLogout`/`useBuildInfo` 被 `pages/dashboard/settings-menu.tsx` 及其测试复用 —— 根目录组件里"逻辑与呈现分离"做对的样本。组件本体 `UserMenu` 仅 sidebar 一个消费者,collapsed(字母头像)与展开形态都在内部 switch。重构时归属拆分后的 `sidebar/`。

### 2.6 router-link.tsx:实现正确,但推广不彻底

- **位置:** `components/router-link.tsx:28-44`;**严重度:** 低
- modifier 键处理、target/download 透传、defaultPrevented 尊重都正确。但全仓库唯一消费者是 sidebar.tsx;`mobile-tab-bar.tsx:61-76` 四个 tab 用裸 `<button onClick={navigate}>`,丢失 `<a href>` 语义(中键/新标签/辅助技术)。
- 建议:TabBar 换用 `RouterLink`。

### 2.7 小组件速评

- `settings-page.tsx` / `selection-empty-state.tsx` / `token-usage-card.tsx` / `context-usage-bar.tsx`(被 command-event-inspector.tsx:312/522 消费)/ `setup-checklist-dialog.tsx` / `profile-common.tsx`(Card/Field 有 7-8 个消费方,是"根目录组件"应有的形态):props 驱动、无或最小 store 耦合 —— **健康,保留**。

---

## 3. 重复代码

**F-D1|高|StatusBadge 三胞胎 + ConnectionBadge 双胞胎**
- 位置:`command-status-badge.tsx:15-25`、`reminder-status-badge.tsx:16-27`、`chat/task-status-badge.tsx:15-35`(同构);`connection-badge.tsx:13-25`、`machine-connection-badge.tsx:8-23`(近似,后者多 KICKED)
- 证据:五个组件全是同一形状 —— enum→{variant,i18nKey} 查表 + `<Badge variant>{t(...)}`。查表分居 `lib/command-status.ts`、`lib/reminder-status.ts`、`lib/task-status.ts`,组件壳三处复制。`command-status-badge.tsx:22` 还有无意义代码 `cn(className)`(cn 单参)。
- 建议:抽范型 `StatusBadge({ mapping, status, className })`,五个组件各剩 10 行胶水;variant 色词在此一并收口。

**F-D2|高|Badge 无 size,消费端各自补小字样式**
- 位置:`chat/agent-badge.tsx:13`(`px-1.5 py-0 text-[10px] leading-4`)、`chat/task-status-badge.tsx:30`(`text-[10px] px-1.5 py-0`)
- 建议:Badge 增 `size="sm"`,删除消费端覆写。

**F-D3|中|modal 壳层三复制(Dialog / AlertDialog / Sheet)**
- 位置:`dialog.tsx:18-66`、`alert-dialog.tsx:15-57`、`sheet.tsx:28-118`
- 证据:三处共享骨架逐行一致 —— Backdrop(`fixed inset-0 ${LAYER_BACKDROP_CLASS} bg-overlay/50`)、Content(`usePreserveHigherLayerAccess("overlay")` + `<Portal container={getLayerRoot("overlay")}>` + `LAYER_SURFACE_CLASS` + p-6/shadow)、Title/Description/Close。差异仅定位、动画、宽度。
- 建议:抽共享背景常量与 `useOverlayModalPortal()` 骨架,可减约 120 行,layer 政策单点演进。Title/Description 类名(Dialog 与 AlertDialog 完全一致)一并共享。

**F-D4|中|弹层三连(Portal+Positioner+Popup)四复制**
- 位置:`select.tsx:84-105`、`popover.tsx:28-50`、`dropdown-menu.tsx:34-90`(主+子菜单)、`context-menu.tsx:21-37`
- 证据:popup 样式串四份近逐字复制(`rounded-sm border border-control-border bg-background py-1 shadow-md`),Positioner 都手动补 `LAYER_SURFACE_CLASS`;select 演化出私有 `positionerProps`,其余没有,API 开始发散。
- 建议:抽 `positioned-popup` 通用封装,四个组件复用。

**F-D5|低|搜索框 icon 前缀重复且绕过 ui 规范**
- `member-picker.tsx:59-66` 手写 `<Search/><input className="w-full h-9 pl-8 …">`,与 `ui/search-input.tsx` 同构,且绕过 Input 组件(违反 "Use existing UI components first")。改用 `SearchInput`。

**F-D6|低|Spinner 无共享组件**
- `Loader2 + animate-spin` 独立出现在 `settings-page.tsx:16`、`agent-status-bar.tsx:87`、`ui/combobox.tsx:129`、`chat/member-picker.tsx`,size/色各异。建议 ui 增 `<Spinner size="sm|md">`。

---

## 4. 潜在 bug

**F-Bug-1|高|ModelCombobox 弹层被 Sheet 滚动容器裁剪 + 违反自家 layering 政策**
- 位置:`ui/combobox.tsx:124`(`absolute z-50 mt-1 …`);消费方 `machine-profile.tsx:1598` 位于 `<Sheet>`(1199–1847)的 `<SheetBody>`(`overflow-y-auto`,sheet.tsx:162-171)内;`agent-profile.tsx:1845` 在页面流 Card 内(暂无裁剪)。
- 描述:绝对定位 dropdown 的祖先有 `overflow-y-auto`,下拉超出 body 剩余高度时被裁剪并触发 body 滚动而非浮层展开。这正是 frontend/AGENTS.md 点名的场景:("For shared controls that expose a `portal` prop, such as `Combobox` … pass `portal` instead of raising a local dropdown with raw `z-index`")—— 而该组件恰恰是唯一**没有** portal prop 的浮层控件,且是 components/ui/ 下唯一 raw `z-50`(守卫脚本全文白名单 `src/components/ui/`,check-react-layering.mjs:22-24,对此失明)。
- 附加脆弱点:blur 后 `window.setTimeout(() => setOpen(false), 120)`(combobox.tsx:117-119)靠 mousedown+preventDefault 补偿,touch/长按/测试(fake timers,combobox.test.tsx:41-45)都是时序雷。
- 建议:参照 `SelectContent` 接入 `getLayerRoot("overlay")` portal;用 Base UI Popover 重写定位与 outside-click/Escape,或最小改造:新增 `portal?: boolean` 并用 hook 替换 setTimeout。

**F-Bug-2|中|Checkbox 的 onClick 包装器语义漂移**
- 位置:`ui/checkbox.tsx:64,77-86`
- 传 `onClick` 时结构改变:`className` 从 checkbox root 转移到外层 `<span>`(`!onClick && className`);直接点击 checkbox 时冒泡使 Base UI toggle 与 span onClick **同时**触发;disabled 时 Base UI 阻止 toggle 但 span onClick 照常触发。且全部 7 个消费方无一使用 `onClick`,该分支只有自测覆盖。
- 建议:删除 wrapper 分支与 onClick prop。

**F-Bug-3|低|TableHead 点击合并:preventDefault 无效 + resize 与排序竞态**
- 位置:`ui/table.tsx:94-97,108-110`。`onClick={(e)=>{ onClick?.(e); if (sortable) onSort?.(); }}` —— 调用方 preventDefault 阻止不了排序;`ColumnResizeHandle` 在 th 内,拖拽结束的 click 冒泡触发 onSort(拖完列宽顺手重排序)。
- 建议:`if (sortable && !e.defaultPrevented)`;handle 上 stopPropagation。

**F-Bug-4|低|FieldRow 的 `text-danger` 是不存在的 token(必填星号静默无色)**
- 位置:`ui/field-row.tsx:27`。tailwind.css 全文件搜 `danger` 仅命中 `.btn-danger` 类(:250),`@theme` 只有 `--color-error`(:178)。全仓库 `text-danger` 共 13 处误用。codemod 换 `text-error`。

**F-Bug-5|低|原始色违反语义 token 规范**
- `agent-status-bar.tsx:28-35`:`text-blue-400/violet-400/emerald-400/amber-400`;`copyable-command.tsx:21`:`bg-white text-black dark:bg-zinc-900 dark:text-white`(手动 dark: 覆写,双料违反 frontend/AGENTS.md);`command-timeline/command-terminal`:`bg-dark-bg`/`text-matrix-green` 系 `:root` 里游离的二等 token。
- 建议:`dark-bg`/`matrix-green` 提升进 `@theme`(如 `surface-terminal`/`terminal-green`);agent-status-bar 改用语义色;CopyableCommand 改 `bg-background text-main`。

**F-Bug-6|低|Toaster 绕过 LAYER_SURFACE_CLASS**
- `ui/toaster.tsx:65-70`:Viewport `fixed` + inline `zIndex: LAYER_Z_INDEX.overlay(2500)`,与家族 root 同值而非 layer 内 `z-10` 层级约定;`toastManager as any`(:58-59)是全目录唯一类型安全洞。Viewport 改用 context class,或至少注释 layering 例外;`as any` 收敛为 typed adapter。

---

## 5. 性能

总体:**全部被审 44 个文件中 `memo`/`React.memo` 出现次数为 0**。当前列表规模(侧栏 ≤15 项、成员选择百级)不算急症,但:

- **F-P1|低|全目录零 memoization,member-picker 最痛**:MemberRow 是内联组件 + `filteredUsers/filteredGroups` 每按键整列表重渲染(member-picker.tsx:40-51)。建议行组件 memo + filtered useMemo(暂不必虚拟化)。
- **F-P2|低|ModelCombobox 过滤未 memo**(combobox.tsx:44-52),重写时(见 F-Bug-1)带上。
- **F-P3|低|`useSidebarItems` 依赖 10 项权限 + t**(sidebar.tsx:251-262),任一失效重建整棵树并重跑 expandForActiveRoute effect;重构时静态形状(items 结构)与动态 hide 分离,依赖降到 1。
- **F-P4|观察**:死代码 CommandTimeline 的"同流输出合并"(renderItems useMemo,112-135)在活跃的 `command-event-ledger.tsx` 有独立实现,删除不受影响。

---

## 6. 重新设计视角:如果重做 ui 组件库

**必须保留(验证过的资产):**
1. **layer.ts 三家族分层体系**(overlay/agent/critical/watermark + `usePreserveHigherLayerAccess` 的 MutationObserver 保活):架构独特、有 5 个场景测试(dialog.test.tsx)、全仓库堆叠一致性的支点 —— 本目录最值钱的 146 行。
2. **Base UI compound 包装族**(Dialog/AlertDialog/Sheet/Select/Popover/DropdownMenu/ContextMenu/Tabs/Checkbox/Switch/RadioGroup/Toast):ref 透传、layer portal、类型转发全部正确;Sheet 的 Header/Body/Footer 高层布局 API 是正确方向。
3. **button/badge/input 的 cva 基座**;**Alert** 的高层 API(title/description/icon/onDismiss,alert.test.tsx 明确锁死"单导出原语"取舍)。
4. **SecretInput**(弃用 type=password 的设计决策有注释论证)/NumberInput/FieldRow 等体验性表单件。

**应清理:** §1.2/§2.2/§2.3/§2.4 全部死代码(~300 行 + 3 个 unused 导出 + 6 个 sheet 宽度档);三套 size/variant 命名收敛为一套;重构后 `select.test.tsx` 的 positionerProps 断言必须随 API 演进同步。

**建议写入 AGENTS.md 的目标 API 约定:**

```
命名   : named export,函数组件,file = 组件名
ref    : React 19 ref-as-prop 显式解构;禁止 forwardRef、禁止 {...props} 隐式转发
variants: cva,回调命名 cmpVariants;不导出 variants 对象(未用即亡,见 F-5)
cn     : cn(cmpVariants({ variant, size, className })) — className 恒为 cva 最后一参
size   : xs sm md lg(禁 default 别名;Badge 增补 sm)
variant: default secondary success warning error info(禁 destructive 与 error 并存)
portal : 一切浮层经 getLayerRoot(family);浮层组件必须支持 portal 覆盖入参
asChild : 不引入(Radix 概念)。等价能力直接透传 Base UI 的 render prop
```

关于 `asChild` 策略的明确建议:**不引入**。本库选择 Base UI,`render` prop 是其扩展通道,原语包装层已全部 `ComponentProps<typeof Base...>` 透传,天然支持 render;再叠 asChild 只会制造两套 API。当前动机最强的 `<a>` 样式 Link 按钮场景 0 真实消费,需要时给 Button 透传 `render` 即可。

---

## 7. 本模块重构优先级清单(按性价比排序)

| # | 动作 | 工时 | 风险 | 收益 |
|---|---|---|---|---|
| 1 | **删除死代码**:tooltip.tsx、separator.tsx、command-timeline.tsx、CommandTerminal、MobileSidebar、buttonVariants/badgeVariants/alertVariants 导出、sheet 6 个未用宽度档 | ~1h | 近零 | -300 行;目录与规范一致 |
| 2 | **ModelCombobox portal 化**(接 getLayerRoot、删 raw z-50、去 blur setTimeout) | 2-4h | 低(2 消费方,有测试) | 修真实裁剪 bug;消除全库唯一政策违例 |
| 3 | **StatusBadge 合并**(command/reminder/task + connection×2 → 范型 + 查表) | 3h | 低 | -150 行;variant 色词收口 |
| 4 | **modal 壳层共享**(Backdrop 复合常量 + Content 骨架提取) | 4h | 中(有 dialog.test 保护;19 处 Sheet 消费) | -120 行,layer 政策单点化 |
| 5 | **命名统一 codemod**:destructive→error、删 size "default"、更新 Sheet 档注释 | 2h | 低 | 终结三套词典 |
| 6 | **Checkbox 移除 onClick wrapper 分支** | 0.5h | 零(无消费方) | 消除 API 陷阱 |
| 7 | **TableHead 排序/resize 竞态修复** | 0.5h | 低 | 消除可复现交互 bug |
| 8 | **语义 token 清理**(bg-white/text-black/dark:;dark-bg/matrix-green 进 @theme;text-danger codemod ×13) | 2h | 低 | dark mode 正确性 |
| 9 | **MobileTabBar 换 RouterLink**;member-picker 复用 SearchInput + 行 memo | 2h | 低 | a11y + 一致性 |
| 10 | **Separator 去留决策**(删 vs 落实 20 处 border-t 替换) | 0.5h 决策 | 低 | 与 AGENTS.md 对齐 |

> 分支切分建议:第 1、3、5、6、8、10 项纯机械替换可合并为单 PR;第 2、4 项动 portal 骨架,独立 PR 并手测三个嵌套场景(Dialog in Sheet / Select in Sheet / Combobox in Sheet)。

## 8. 可安全删除的组件清单(已逐一 grep 验证 0 引用)

| 文件/符号 | 行数 | 证据 |
|---|---|---|
| `src/components/ui/tooltip.tsx` | 17 | 全仓库 0 import |
| `src/components/ui/separator.tsx` | 27 | 全仓库 0 import(20 处手写 border-t 均未用它) |
| `src/components/command-timeline.tsx` | 200 | 全仓库 0 import;`CommandTimeline` 唯一引用是 use-auto-scroll.ts:5 的注释 |
| `CommandTerminal`(`command-terminal.tsx:8-53`) | ~50 | 消费方仅 import `FinalSummary`;文件可并入 lib/markdown.tsx |
| `MobileSidebar`(`sidebar.tsx:465-514`) | 50 | dashboard-layout 仅 import DesktopSidebar |
| `buttonVariants`/`badgeVariants`/`alertVariants` 导出(button.tsx:52、badge.tsx:32、alert.tsx:114) | — | 全仓库 0 import(含测试) |
| sheet 宽度档 `narrow|panel|large|xlarge|huge|workspace`(sheet.tsx:64-75) | 12 | 全部 19 处 `<SheetContent>` + 3 处动态 width 仅命中 medium/standard/wide |

删除前最后一步:执行 `pnpm --dir frontend type-check && pnpm --dir frontend test && pnpm --dir frontend biome:check` 三重验证(以上 0 引用结论以当日工作树为准)。