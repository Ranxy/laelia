# 前端深度重构分析报告

> 分析对象:`frontend/**` — 生产代码 222 个文件 / **48,214 行**,测试 95 个文件 / **17,969 行**(611 个 it 用例、0 快照),不含生成的 proto-es 代码。
> 分析方法:9 个模块级深度审查(逐文件精读、grep 交叉验证、全部死代码结论经全仓引用核验)+ 全局横切统计(git 热度、hooks 密度、分层扫描、i18n 双语 diff)+ 架构骨架人工审读。
> 详细证据:分模块报告共 9 份、约 2,400 行,见 `docs/refactor/01~09`。本文件是执行摘要与总体决策。

---

## ⚡ 实施进度总览(更新于重构执行批 0~批 14 后)

重构已执行 **110 个提交、15 个批次(含批次间 docs 收口提交)**,四道门禁(type-check / biome / vitest / check)持续保持全绿;测试规模从 95 文件 / 611 用例增长到 **124 文件 / 831 用例**,全量 vitest 干净退出(exit 0;预存的 jsdom IntersectionObserver unhandled 随重构消失)。各章文件头部已附加对应的"进度标注"块。

| 批次 | 提交范围 | 内容 | 状态 |
|---|---|---|---|
| 批 0(原 Phase 0)| `999e131`~`027009f`(7 提交) | 死代码清扫(-500 行)、七连修、XSS 白名单 + Combobox portal、语义 token、轮询止血、i18n 门禁修复 | ✅ 完成 |
| 批 1(Phase 1 第一批)| `d4774c3`~`5efb461`(3 提交) | ADR-2 落地(preview 退役 + 冻结删除)、equal-bailout(6 lists)、无界缓存 LRU | ✅ 完成 |
| 批 2(Phase 1 第二批)| `b95c530`~`389ce97`(4 提交) | TanStack Query 铺路 + api-provider/mcp 纵切、错误分类学 + showErrorToast、useResourceList | ✅ 完成 |
| 批 3(Phase 1 收官)| `e7aca3a`~`acb701d`(4 提交) | user/agent/machine 纵切、watch 断线重连、cleanup registry、usePolling 收敛 | ✅ 完成 |
| 批 4(Phase 2 聊天域收拢)| `4085c66`~`bc65511`(3 提交) | ChatGateway watcher 合一与可见性门控、useChatComposer(-600 行重复)+ 3 真实 bug 修复、批 4 归位(interval 收编 + lib 自注册) | ✅ 完成 |
| 批 5(Phase 3 页面拆分)| `627bf6b`~`b6664ad`(19 提交) | settings CRUD 脚手架四原语 + 7 页全迁移(roles/api-providers/mcp/idp/iam 特化 + 尾部统一,idp 测试从 0 → 9 用例,B4/B6/B7/B8/B12 随迁修复)、thread-panel 拆四件(790→393)、agent-profile ACP 编辑器抽取(2379→1168)、global-search 手写 pickers 收敛 Base UI combobox(1011→477)+ 首个测试、sidebar 拆件(457→49)、chat-conversation/machine-new 页面级测试 | ✅ 完成(19 个提交) |
| 批 6(Phase 4 UI/事件管线,重设计后路线)| `5fdf8fa`~`a4dcabf`(23 提交)| badge/词典收敛(StatusBadge 范型、destructive→error、Badge size="sm"、Button size 别名删除、Avatar 显式映射、组件 API 约定与 Separator 决策入 AGENTS.md)、modal 壳/弹层共享(LAYER_BACKDROP_SET + positioned-popup + ModelCombobox portal)、**TimelineModel 归一**(`lib/command-events-model.ts`:4 份 merge 拷贝→1、kind 注册表唯一化、isToolCallError 单点 + F-D7 死分支)、preview 收敛(CommentsPanel 双胞胎合并 + FilePreviewShell + useHtmlPreviewBridge,修 F-B9/F-S2/F-S3/F-B10)、**ADR-3 ②③**(ledger 虚拟化 + 100KB 输出截断 + 搜索防抖、workspace 树扁平化虚拟化 + role=tree a11y + 树内搜索、消息列表轻窗口化 `useWindowedMessageRange`)、TwoPaneShell/DetailTabsLayout 布局收敛、手势统一 + 共享 TransferOwnershipDialog + useMessageScroller 搬移(-499 行状态机出页面)、B7/B8/B-7 随手清偿、PWA reload 用户可见护栏、toast.ts 改 Base UI 官方工厂、tsconfig 覆盖 vite/sw(~~tool_call_id 需后端立项,未动~~ → 批 9 `68e53a6`~`baf4167` 根治)| ✅ 完成(24 个提交)|
| 批 7(05 章 stores 收尾)| `2d4f224`~`4dad293`(6 提交)| **types.ts 拆分**(903 行契约上帝文件→共享 UI 模型入 `stores/ui-models.ts`、19 个 slice 接口与实现同文件、51 行组合点,组件导入改 `@/stores/ui-models`,50 文件)、**写入面收敛**(`SliceSet` 重载收窄 + 四条跨 slice 写入授予注册表:channel→chat 消息 map、thread→chatMessages 回复数、task→threadByRoot、members→user/agent 花名册;直接字面量越权写入编译期拦截)、**conversations 私有化**(module 缓存 + cleanup 注册,reset 即清,+4 测试)、**polling.ts→delay.ts 更名**、**reset 测试表驱动化**(覆盖面随 slice 自动补全)、**store 写入面守卫入 check 门禁**(`check-store-writes.mjs` 置首位 + AGENTS.md「Store Write Surface」)| ✅ 完成(6 个提交)|
| 批 8(05 章收官:presence/activity/reminder 迁 Query)| `b7a3799`~`53b9cc3`(6 提交)| **presence 心跳保留、数据入 Query**(`["presences"]` 唯一 fetcher 挂 dashboard 布局,结构共享替代手写 bailout,空名单仍发心跳,消费端 `useOnlineUsers()` 只读,reset 清缓存)、**reminder 列表/详情迁 per-key 查询**(终态停轮、keepPreviousData 翻页、外包装+内体 key 化重置、list/detail 双 composable)、**activity per-(filter,pageToken) 查询 + useQueries**(requestSeq/手写 equal/首屏 merge hack 消解,markDone 乐观移除全部缓存页,detail 兜底 QueryCache 订阅扫描)、**三 slice 退役**(19→16 组合,手写 equal ×3、silent 样板 ×3 随之消失)| ✅ 完成(6 个提交)|
| 批 9(tool_call_id 全栈贯通)| `68e53a6`~`baf4167`(3 提交)| **proto 契约**(ToolCallStarted/FinishedPayload 增可选 `tool_call_id`,Go/proto-es/grpc-doc 重生成)、**后端全链路透传**(ToolCallSink 接口带 id,ACP 三帧、acp2 thread executor、pi executor 全部发射点透传 runtime id,interleaved/pi 测试钉死契约)、**前端配对 ID 优先 + FIFO 兜底**(并发交错不再错配,断线缝隙/legacy 仍事件序兜底,pair 按 started 顺序输出)| ✅ 完成(3 个提交)|
| 批 10(activity 双分页收敛,08 F-S8)| `29be54e`(1 提交)| **统一无限滚动**(产品拍板):activity-list 桌面 Prev/Next 分页栈/翻页脚手架/lastRowsRef 退役,桌面/移动共用 token 栈 + IntersectionObserver sentinel(316→235 行);5s 轮询统一骑第 0 页(`useActivityPages` 去 `intervalIndex`,newest-first offset 分页下唯一稳定窗口,顺带修掉轮询可见 offset 页的行漂移隐患);`activity.page/prev/next` 死键删除;测试改写为 scroll-append/加载中保留行/耗尽即止 三案| ✅ 完成(1 个提交)|
| 批 11(hooks 门禁 + 杂项清偿包,06 E-03/P1 + 08 杂项 + 07 杂项)| `96963cc`(1 提交)| **Biome hooks 正确性规则启用**:`useExhaustiveDependencies` + `useHookAtTopLevel` 上线,25 处存量清偿(6 处真漏依赖补齐、惯用法 reset/keyed effect 以带理由 `biome-ignore` 固化、`AcpConfigEditor` 退役 `memo(forwardRef)` 转回 ref-as-prop、comments-panel 条件 hook 上移)、profile 页 5 处 no-op `eslint-disable` 全删、biome.json 幽灵 overrides 清理 + `biome:lint` 重复脚本去重;**08 杂项**:inspector WARNING 游离块收进 summary tab(F-B6)+ 回归用例、html overlay locate `.then` 补 unmount/换代防护(F-B9 尾款)、F-B10 注入断言核实批 6 已修;**07 杂项**:`ui/spinner.tsx` 共享 Spinner(F-D6,4 处点名消费端)、MobileTabBar→RouterLink、member-picker 行 memo + joined 徽章归一 Badge size="sm"| ✅ 完成(1 个提交)|
| 批 12(数据层与 lib 收尾清偿,05 终局 + 06 P1 + 01 尾巴)| `d839784`~`bf39872`(6 提交)| **useResourceList 退役(05 章终局)**:最后消费者 command-list 迁 `composables/use-command-list.ts`(one query per (agent,status,pageToken) + scoped placeholderData 翻页保持),CommandSlice 死缓存(`commands`/`commandsLoading` 零读者)与 `listCommands` 退役,`lib/use-resource-list.ts` 删除;**缓存统一 + 三竞态修复(06 R-03/B-02/B-03/B-09)**:`lib/async-memo-cache.ts` 共享原语(世代号守卫 + invalidate 广播 + FIFO 容量),avatar/image 两站接入——invalidate 后 in-flight 不再写回、useAvatar 切换即清屏、avatar 获 500 条上限;**lib 整形(06 R-02/B-08)**:command-status 拆 `time-format.ts` + `resource.ts`(avatarName 构造并入),死导出 ×3 清除;**路由名缝合(06 P1)**:RouteName 联合 + `Record<RouteName, RouteInfo>` 穷尽 + 37 处 handle `satisfies RouteHandle` + backTo 改路由名(routeNameForPath 反查删除;顺带补齐 machine.new/channel.detail 的移动端标题缺口);**01 尾巴**:B10(groups owner 校验 + set 语义 dirty)、B13(profile 表单脏保护种子)、B15(死 `??`)+ 回归用例;agent-mcp 满载 flake 超时放宽(承 `d1301ba`)| ✅ 完成(6 个提交)|
| 批 13(08 章终局:时间轴/侧栏壳/成员选择器)| `49ae3c4`~`fd9419a`(4 提交)| **overview 真实时间轴 + span 上限(08 F-P4,`f462da9`)**:span 按真实时间线性定位(回归 trajectory 重设计原意,等宽钉死测试退役),500 上限保留最近 span 并以 "+N" 徽标标注截断前缀;同提交以单一 overlap 谓词统一拖选收集与渲染压暗 + span 按钮按下短路 + 4px 位移阈值 + pointerup 按实际位移判定,顺带清偿 F-B5;**SidePanel 壳统一(08 F-S5,`fd9419a`)**:新增 `ui/side-panel.tsx`(头行/toolbar 槽/滚动体/钉底槽,`mobileSheet` 窄屏走 Sheet + 边缘滑动关闭/历史哨兵),inspector(移动端不再盖死 ledger)与 CommentsPanel 换壳,workspace 文件面板按"布局面板非浮层"刻意保留;**AgentSelect 迁共享 Select(08 F-S7,`49ae3c4`)**:手写下拉(z-30/外点关闭/无 ARIA/无键盘)由原语接管,富内容行与排除语义保留 + 三用例;agent-profile 满载 flake 改 findBy(承 d1301ba 房法,`aea60d7`)| ✅ 完成(4 个提交)|
| 批 14(06 章终局:权限守卫/手势引擎/hooks 归一/@theme)| `8acfe2f`~`cc7e298`(5 提交)| **handle.permission 路由权限守卫(06 Rt-02,`f31b591`)**:`RouteHandle.permission`(单值/any-of 数组,镜像 sidebar/settings-menu 的可见性口径)落进 14 个 handle,`RoutePermissionGate` 以响应式组件门包住 dashboard Outlet(loader 只跑导航会话、硬刷新深链无法重估),无权限渲染禁止面不再拉取页面 chunk;**手势引擎合一(06 P2,`fef3b17`)**:`useEdgeDrag` 共享引擎(监听/边区/方向锁/阈值/settle 单点),useSwipeBack 改宿主、use-swipe-to-close-sheet 退役,touchCancel 策略按面保留零行为漂移;**resolvePath→generatePath(06 Rt-03,`3847640`)**:第二套路由 DSL 退役,缺参从静默留 `:id` 改为大声抛错;**hooks 目录合并(06 R-04,`8acfe2f`)**:lib/use-* 与 composables/ 并入单一 `src/hooks/` kebab-case(63 文件导入面随迁);**tailwind 迁 @theme(06 E-01,`cc7e298`)**:v3 配置文件删除,语义色/字体栈入 CSS,三元组换名 --rgb-*,死配置与死插件随之清零,vite build + 产物 CSS 验证 | ✅ 完成(5 个提交)|

**当前数据层状态**:Query 已纵切 12 个读族 + 应用级单例与 Provider(api-provider/mcp/user/agent/machine/settings 目录/iam-policy/presence/reminder/activity/command-list);聊天域长轮询已收敛为共享 ChatGateway 循环(可见性门控 + badge 同节拍);乐观发送经 useChatComposer 走 slice action;错误出口统一;登出经注册表(lib 自注册 + 各 Query 域 registerCleanup 清缓存)。**批 8 收官 = 05 章账面清零**:presence/activity/reminder 三 slice 退役,store 组合收缩至 16 slice。**批 12 = 05 章真正终局**:`useResourceList` 退役(全部列表域直奔 Query),CommandSlice 死缓存与手写分页薄壳删除;同批 avatar/image 缓存统一到 `async-memo-cache` 原语并修复三处竞态。**批 13 = 08 章账面清零**:overview 回归真实时间轴 + 500 span 上限并顺带清偿 F-B5 拖选几何缺陷,`ui/side-panel.tsx` 补上侧栏面板原语缺口(inspector/CommentsPanel 换壳),AgentSelect 收编共享 Select。**批 14 = 06 章账面清零**:路由级权限守卫(handle.permission + 响应式组件门)、手势栈收敛到共享 useEdgeDrag 引擎(两套 ~70% 重复状态机归一)、resolvePath 退役自造 DSL 改 generatePath、hooks 目录合一(`src/hooks/`,lib 只留纯函数)、tailwind v3 配置文件清零(@theme + 语义 token 单点)。**批 11 = hooks 正确性零黑洞**:`useExhaustiveDependencies`/`useHookAtTopLevel` 已随门禁启用,存量清偿完毕,后续 hooks 依赖漂移在 lint 阶段即被拦截。(产品拍板项已全部清零:流式管线拆除批 6、tool_call_id 批 9、activity 双分页批 10。)

---

## 0. 执行摘要:十项最高价值行动

> 下表的"状态"列为实施 5 个批次后的核查结果(提交号见 §0.5 进度总览)。

| # | 行动 | 状态 |
|---|---|---|
| 1 | **P0 修配包**:MCP key 失焦、ProviderSheet 串数据、machine-new 否认、mentionLabel、no-data 文案、TableHead 竞态、删除文案(`5bc9f90`) | ✅ 完成 |
| 2 | **死代码大扫除** ~500 行(`b0499db`) | ✅ 完成 |
| 3 | **安全与门禁**:i18n 门禁修复(`027009f`)、`safeOpenExternal` XSS 白名单 + Combobox portal(`75d844b`) | ✅ 完成 |
| 4 | **错误处理单点化**:`showErrorToast` + `connectErrorKind`,40+ 处裸 message 被 codemod 收敛到 `describeError`(`389ce97`) | ✅ 完成(146 处 toast 中形状规则的已收敛;动态 title 的 2 块保留 toastManager + 共享 description) |
| 5 | **列表获取基建**:`useResourceList`(`723de0e`,command/reminder 已迁;activity 于批 8 迁 Query per-(filter,pageToken),批 10 统一无限滚动后双分页语义随之消解);equal-bailout 六列表(`c9fe388`) | ✅ 完成(settings 7 页的脚手架迁移归入页面拆分阶段) |
| 6 | **统一发送/乐观更新管线**(`useChatComposer`) | ✅ 完成(`4085c66` watcher + `5c6665c` composer;三个聊天域 bug 一并修复) |
| 7 | **ADR-1 引入 TanStack Query**(`b95c530`~`e7aca3a`)/ **ADR-2 preview 退役 + 冻结删除**(`d4774c3`) | ✅ 完成:数据层 slice + 聊天域长轮询与组件 interval 全部收敛(`4085c66`/`bc65511`) |
| 8 | **流式管线拆除决策** | ✅ 完成(`07b2799`,产品确认拆除):ChatMessageUI.streaming 字段 + rowStreamingProps + typing-dots + fade 全部移除,MessageRow 接口面 -2 个流式 props |
| 9 | **三个巨型页面拆分** | ✅ 完成(批 5+6):agent-profile 2379→1168(ACP 编辑器共享化 `f4e5a36`)、machine-profile 2184→763 + 共享 Ownership 对话框(`b6664ad`/`663867b`)、chat 滚动状态机搬移为 `useMessageScroller`(`10cb1bb`)、thread-panel/chat-conversation 页面级测试齐备 |
| 10 | **事件渲染管线统一** | ✅ 完成(批 6):watch 断线重连(`4b7cf57`)+ TimelineModel 归一(`b3c2644`:唯一 pair/merge/kind 注册表/行键统一,行键漂移根治)+ ledger/workspace 虚拟化与消息列表轻窗口化(`38f78af`/`a4dcabf`/`2f3e949`);proto `tool_call_id` ✅ 批 9 根治(`68e53a6`~`baf4167`,ID 优先配对 + FIFO 兜底) |


**量化总览**:全部建议落地后,预计净删 **7,000~9,000 行**(约 15~19%),修复 **约 30 个已定位 bug(其中高危 12 个)**,收敛 **6 套互不一致的轮询策略、5 种错误呈现、4 套时间格式化、3 套 size 词典**。整体规划约 **8~12 人周**,Phase 0(见 §7)一周内可完成。

> 本节为**审计时点的原始分析与优先级**;执行状态以最上方"实施进度总览"与 §7 重设计版路线图为准。

---

## 1. 代码库全景与健康度

### 1.1 规模分布

| 模块 | 行数 | 状态一句话 |
|---|---:|---|
| pages/dashboard | 20,734 | 重灾区:3 个 2000+ 行巨型组件 + 7 个同模板设置页 + 全局 store 竞态 |
| components/chat | 6,698 | 局部抽象优秀(MemoMarkdown/LazyMarkdown),但发送管线双份拷贝 + 死的流式管线 |
| stores | 4,739 | 单 store 19 slice;模式性债务(9 处 silent 样板、6 套手写 equal、双 watcher loop) |
| lib | 2,916 | 单文件质量高于平均;杂物抽屉(command-status)与缓存三胞胎待整形 |
| components/ui | 2,349 | Base UI 包装族健康;layer.ts 是全库最值钱的 146 行;~300 行死代码 |
| components 根+command-events+preview+agent+activity+workspace+chat-events | 6,164 | command-events 处于"未完成的中期演进";preview 质量最高但有一个 XSS 面 |
| pages/auth | 1,210 | 质量高于平均;device-login 轮询健壮性差 |
| router + app + connect | 1,909 | connect 层质量好;路由名三处真相;swipe-back 依赖 UNSAFE API |
| locales | 1324 key ×2 | 双语零差集;**CI 门禁当前红**;6 个 camelCase 命名空间段 |

**git 变更热度**:最高频被改的源文件是 `stores/types.ts`(104 次提交,903 行类型上帝文件)——它同时是最高热度和最大文件,是"每次改哪个功能都要动它"的一手证据。其次 chat-conversation(65)、agent-profile(49)、thread-panel(38)。高频 × 臃肿 = 改造收益最大。

### 2. 横切统计(全库实测)

- hooks 总量:useState 547 / useEffect 155 / useMemo 76 / useCallback 102;前两名 chat-conversation(12 effect)、global-search(8)。
- 组件直连 RPC:pages/ 中 **60 处**(settings-* 7 页 + human-detail + global-search + team-detail 等集中),components/ 5 处 + 1 处裸 fetch;stores/ 115 处(分层健康的核心域);composables/ 0。
- 类型纪律好:`as any`/`@ts-ignore` 全库仅 5 处;~~`useExhaustiveDependencies` 未启用~~ → ✅ 批 11 已启用并清偿 25 处存量,4 处 no-op `eslint-disable` 注释随批删除(余 2 处在生成代码,不参与 lint)。
- 测试:行为断言为主、零快照,store 竞态测试(chat-stream/chat-history)是重构最值钱的安全网;但 **38 个 ≥150 行的文件无测试**,包括最复杂的 chat-conversation(2166 行)与 global-search(1011 行)。
- 安全:无 token 进 localStorage,会话 HttpOnly cookie,OAuth state 一次性消费——基本面好;唯二的口子在 iframe 桥 `window.open(href)` 无 scheme 校验(ch08 F-S1)与 auth redirect 参数未拒 protocol-relative(ch09 A-6)。

---

## 3. 分模块报告卡片(详细证据见对应章节)

> 每份章节含逐条 file:line 证据、严重度评级、修复建议与该模块的优先级清单。

**[01 设置类页面](./refactor/01-settings-pages.md)** — 14 文件 7,251 行。7 个 CRUD 页 25~35% 是复制脚手架;统一 `useResourceList + useCrudDialog + ResourceSheet/ResourceTable` 可净减 2,000~2,600 行(40~50%)。高危 bug:MCP header 动态 key 每键失焦、ProviderSheet 跨抽屉数据串台、`common.no-data` 实为 "No agents yet." 渲染进 IAM/角色表。设计不失效 stores 的 mcp/api-provider 切片,导致 agent/machine 表单显示陈旧数据。

**[02 三个巨型详情页](./refactor/02-giant-profile-pages.md)** — agent-profile(2405)/machine-profile(2200)/chat-conversation(2166),合计约 100 个 useState、20+ effect。ACP 配置表单跨页复制 ~650 行且行为已漂移(600ms vs 400ms 防抖);chat 域真实 bug:附件上传跨会话串台、删除 @mention 后 mentionMap 残留、发送失败输入不恢复。报告含三棵目标组件树与 6 步可回归迁移路径(约 2~3 人周)。

**[03 Dashboard 其余页面](./refactor/03-dashboard-rest.md)** — 20 文件。列表页三胞胎(~125 行×3 已漂移)、global-search 内造 ~460 行 Combobox 轮子、**6 套互不一致的刷新策略**(2s/3s/5s/10s/30s/长轮询)、列表 store 竞态(旧响应覆盖新过滤)、reminder 对终态仍 2s 无限轮询、`activeOutputs` 无清理全库只增不减。目标 -30% 行数。

**[04 Chat 组件库](./refactor/04-chat-components.md)** — 23 文件 6,698 行。三大结构性问题:流式管线运行时不可达却未拆除、发送/乐观更新管线双份拷贝(~600 行)、面板外壳与手势三套并行。正面资产(MemoMarkdown 冻结纪律、LazyMarkdown、EMPTY selector)明确列为不动点。ThreadReplies `mentionLabel` 漏传、ConversationRow 滑动无方向锁是已验证 bug。

**[05 stores 数据层](./refactor/05-stores-composables.md)** — 单 store 19 slice;watcher 长轮询 loop 双写(~150 行)、silent 样板 ×9、手写 equal ×6、吞错误 catch ×~35;三份冗余消息表示(chatMessages/threadByRoot/tasksByConv)靠 ~300 行双向同步苟活;`fetchChannels` 无 equal-bailout 导致每 5s 全列表重渲染;`suppressLoadingFlags` 冻结窗口会**静默丢弃 logout/reset**。结论:引入 TanStack Query,7 步迁移,stores 4739→~2600-3000 行。

**[06 基础设施](./refactor/06-infra-lib-router.md)** — connect/质量好(401 流式拦截正确、组件零直连);lib 需整形(`command-status.ts` 是杂物抽屉、三个同构缓存、`toast.ts` 依赖 Base UI 私有接口反射);路由名三处真相、`resolvePath` 自造第二套路由 DSL;构建债:tailwind.config.js 是 v3 遗物(半数死配置)、sw/vitest 不在任何 tsc project、biome overrides 指向不存在文件且未开 hooks 正确性规则。**iOS 边缘手势的 WebKit 专项处理确认未过时,必须保留。**

**[07 UI 基础组件](./refactor/07-ui-components.md)** — 保留资产:layer.ts 分层体系、Base UI 包装族、cva 基座。清偿:~300 行死组件(与 frontend/AGENTS.md 自相矛盾——规范要求用 Separator,实际 20 个文件手写 border-t 而组件 0 引用);三套 size 词典、destructive/error 色词分裂;ModelCombobox 裁剪 bug + `z-50` 政策违例;`text-danger` 是不存在的 token(13 处静默失效)。给出应写入 AGENTS.md 的组件 API 约定。

**[08 事件/预览/工作区](./refactor/08-events-preview-workspace.md)** — command-events 处于未完成的演进中段:旧管线(`command-timeline.tsx`)未删,"输出块合并算法"存在 4 份已互相分叉的拷贝(行键漂移→ inspector 打不开),工具调用状态判定三套(chat 侧把 error 显示成灰色 secondary)。**XSS 面**:`link-clicked` 转发无 scheme 白名单。watch 流断线不重连=页面永久停更。评论面板是 300 行复制双胞胎。统一管线方案(kind 注册表 + TimelineModel + 纯函数归一化层)已给出,proto 需加 `tool_call_id`。

**[09 Auth 与横切](./refactor/09-auth-crosscutting.md)** — auth 页质量高于平均;device-login 轮询无终态停止/无退避(用户挂机一晚仍每 3s 打 RPC)。横切:i18n en/zh 各 1324 key 零差集但 **check-react-i18n 当前红(1 missing + 3 unused)**;60 处组件直连 RPC 的完整 file:line 清单;38 个 ≥150 行无测试文件清单;安全基线确认(HttpOnly cookie、无 token 入库)。

---

## 4. 跨模块重复代码清单(统一视图)

按"复制份数 × 单份行数"排序,这是本次审计里最可机械消除的部分:

| # | 重复体 | 份数 | 单份规模 | 出处文件 |
|---|---|---|---|---|
| 1 | 设置页 CRUD 页面模板(表单状态三组 useState、load/create/save/remove、确认弹窗、空态、Sheet 壳) | 5~7 | 每页 200 行 | 01 §1.1 R1~R16 |
| 2 | ACP 配置表单(agent-profile ↔ machine-profile) | 2 | ~650 行/份 | 02 |
| 3 | 发送/上传/乐观更新 composer(thread-panel ↔ chat-conversation) | 2 | ~300 行/份 | 04 §1.1、05 D5 |
| 4 | 长轮询 watcher loop(channel ↔ thread,含注释复制) | 2 | ~150 行/份 | 05 D1 |
| 5 | 输出块合并算法(command-events 三处 + 死代码一处,**语义已分叉**) | 4 | ~50 行/份 | 08 F-R1 |
| 6 | 评论面板(comments-aside ↔ html-comments-aside,逐行同构) | 2 | ~300 行/份 | 08 F-R4 |
| 7 | "silent 刷新不翻 loading"样板 | 9 | ~10 行/份 | 05 D2 |
| 8 | 手写等价比较函数(2 个用 proto equals,4 个手写全字段) | 6 | ~20 行/份 | 05 D3 |
| 9 | StatusBadge/connection-badge 五胞胎 + modal 壳三复制 + 弹层三连四复制 | 12 | 10~120 行/份 | 07 §3 |
| 10 | rail+pane 两栏壳 ↔ 列表页三胞胎 ↔ 移动 FAB ×4 ↔ DesktopContextMenu ×3 ↔ 面板外壳 ×3 | 18 | 同构 | 03 R1/R2/R5、04 §1.3/§1.4 |
| 11 | 工具函数重复:`memberLabel` ×4(两份逐字节相同)、`slugify` ×2、时间格式化 ×4 套 + proto Timestamp 换算 ×2 种、`formatBytes` 住在 chat 被 4 模块跨域引用、资源 id 提取散落 29 处 | — | — | 01 R11/R17、04 §1.6、08 F-R2/F-R8、03 R6 |
| 12 | 组件直连 RPC(vs store 收口惯例):settings 全家 41 处 + human-detail/team-detail/global-search/auth 域 | ~60 | — | 09 §2.2 完整清单 |

**合计可消解约 7,000+ 行;且每一项都不只是代码量问题——已发生的"行为漂移"证明复制正在持续制造 bug**(防抖时间不一致、合并语义分叉、equal 漏字段、删除文案漂移)。

---

## 4b. 真实 bug 清单(Top,全部经代码位置核验)

高危(用户可见/数据风险):

1. **附件上传跨会话"串台"** — chat 切换会话后未完成的上传进入新会话草稿(draftRef 恢复漏了 uploads)[02]
2. **ProviderSheet 跨抽屉数据污染** — 编辑 A 提供商后切 B,残留模型条目 + A 的 apiKey 写进 B 保存 [01 B2]
3. **machine-new「不是我」按钮失效** — effect 依赖把候选机器立即复活, deny 等于不可拒绝 [03 B2,已本人复核]
4. **swipe-back 冻结窗口丢弃所有 store 写入(含 logout/reset)** — 500ms 窗口内一切 `set` 静默消失 [05 B3、06 B-01]
5. **watch 流断线不重连** — 长命令页面永久停更,只能手动刷新 [08 F-B2]
6. **iframe 桥 `window.open(href)` 无 scheme 白名单** — 预览不可信附件可触发 `javascript:` 执行面 [08 F-S1]
7. **列表 store 无请求序号** — 快速切过滤/页码时旧响应覆盖新列表(fetchChannels/agents/machines/audits 全族)[05 B1、03 B1、01 B6/B7]
8. **MCP header 输入框每键失焦** — `key={name-i}` 动态 key [01 B1,已本人复核]
9. **ThreadReplies mentionLabel 漏透传** — 共享频道回复行 mention 永不解析显示名 [04 §3.1,已本人复核]
10. **FIFO 工具调用配对** → ✅ 批 9 根治:`tool_call_id` 进 proto + 全链路透传(`68e53a6`/`69396f8`),配对 ID 优先 + FIFO 兜底(`baf4167`);chat 侧 error 状态显示成灰色已随批 6 `f01f818` 修复 [08 F-B1/F-R3]

中危(代表项):IAM/角色空表渲染 "No agents yet."、删除按钮一律 "Saving…"、设置页 CRUD 不失效对应 store 切片、`toggleReaction` 双击竞态、乐观发送失败输入框文本不恢复、头像/图片缓存 invalidate 后旧数据写回(跨会话数据残留)、`drainRoster` 与心跳互踩串页、device-login 终态后无限轮询、conversation-list 滑动无方向锁 60fps 重渲染、avatar `size-${n}` 动态类依赖巧合、8 处 mutation 无 catch 静默失败。

---

## 4b. 死代码/历史债务清单(合并,全部 grep 验证零引用)

| 类别 | 清单 |
|---|---|
| 整文件 | `components/command-timeline.tsx`(200 行)、`lib/use-auto-scroll.ts`(被死组件独占)、`ui/tooltip.tsx`、`ui/separator.tsx` |
| 文件内 | `CommandTerminal`(final 组件 FinalSummary 幸存,迁 lib/markdown)、`MobileSidebar`(50 行)、5 个投机枚举 value(steer/retry)、`commandEventTypeToI18nKey`、inspector timing 死分支、inspector WARNING 游离块 |
| store/slice | `TaskSlice.closeTask`(整 action + 类型 + 测试)、`stores.refreshAgentProviders`、`ChatSlice.conversations`(应私有化)、`splitByMentions` 分段渲染路径(只剩布尔门用途)、`reminder-list.initialLoadDone` |
| lib 死导出 | `formatToken`、`isHtmlPreviewable`、`isMarkdownPreviewable`、`reminderStatusShort`、`buttonVariants/badgeVariants/alertVariants`、`useMentionDetect` hook、`pairToolCallEvents` re-export、`EmptyState.action`/`Avatar.label` |
| 配置/文档 | Sheet 6 个未用宽度档、`tailwind.config.js` 的 v3 死配置、biome overrides 两个幽灵文件、4 处 eslint-disable no-op 注释、frontend/AGENTS.md 指向不存在的 `@/react/lib/utils`/`AgentWindow`/`SessionExpiredSurface`、package.json 重复脚本 |
| i18n | 3 个死 key(`sidebar.settings-agent-teams`、`settings.agentTeams.create-*`)+ CI 门禁红因(`common.deleting` 缺失) |
| 架构遗产 | **streaming 渲染管线**(全库无生产者)、`@config` 引入的旧主题 token(`dark-bg`/`matrix-green`) |

---

## 5. 重新设计蓝图(如果从头再来)

### 5.1 目标目录与分层

综合 9 份章节的重设计结论,收敛为一张图:

```
src/
├─ platform/            # 宿主能力:iOS 边缘手势语义、PWA/SW 更新策略、Web Push(回调注入,禁 import stores)
├─ connect/
│   ├─ transport.ts     #   + interceptors/ (unauth-redirect, error-taxonomy) ← 146 处 toast 的收敛终点
│   └─ clients.ts       # 服务注册表批量生成,替代 17 条手写
├─ router/              # 路由单一真相:handle{name, permission?} 派生 handles/route-info;
│                       # 权限下沉 route loader;anchor 跳转走 generatePath
├─ lib/ (纯函数,禁 import stores)
│   ├─ format/ (time 统一 4 套、resource-name 收编 29 处、bytes)
│   ├─ cache/async-memo  # avatar/image/preview 三胞胎缓存合一(带世代号)
│   └─ command-events-model.ts  # pair/merge/kind 注册表/lane 唯一实现,100% 单测
├─ hooks/               # 合并 lib/use-* 与 composables/(两套目录、两种命名并存是历史事故)
├─ data/                # TanStack Query 层 + ChatGateway(见 5.2)
├─ stores/              # 瘦身为纯 UI 状态(activeThreadRoot、panels/chat 跳转状态、预览、lightbox)
├─ features/
│   ├─ settings/shared/ # useResourceList + useCrudDialog + ResourceSheet/ResourceTable + directory 缓存
│   ├─ chat/            # ChatPanelShell + message-list/(轻窗口化) + composer/(useChatComposer)
│   ├─ command/         # 统一 TimelineModel 消费端(inspector/ledger/overview + SidePanel 壳)
│   └─ …
└─ components/ui/       # 保留 layer.ts 与 Base UI 包装族;补齐 size/variant 单一词典与 API 约定
```

### 5.2 架构决策记录(ADR)—— 已拍板生效

> 以下三项为重构总决策点,已经评审确认。后续各阶段与各章节的方案按此执行,不再重复讨论。

**ADR-1 轻缓存层:引入 TanStack Query。**【已拍板:引入】
决策:引入 TanStack Query 作为全局唯一服务器缓存层,`useResourceList` 作为其上的列表特化薄壳(分页 token → infinite query、请求序号、initialLoading/refreshing 分离)——消解 05 章(建议 Query)与 01 章(倾向 useResourceList)的表面冲突:二者不矛盾,前者是战略层,后者是战术层。理由:手写机制点(silent 样板 ×9、equal ×6、乱序防护、轮询可见性、乐观回滚、无界缓存、logout 核爆 reset)每项都需真实测试才可靠,自研成本 ≈ 引入成本;React 19 下 Query 是一等公民。落地约束:zustand 保留但职责反转为纯 UI 状态;不拆 19 个独立 store;迁移顺序按 05 章 §7.3 七步执行;全库唯一新增依赖为 `@tanstack/react-query@5`(`pnpm --dir frontend add @tanstack/react-query`)。

**ADR-2 移动端 swipe-back 预览:改只读快照/CSS 转场。**【已拍板:方案 (b)】
决策:退役"路由组件级 preview",手势预览改为主闭包式的 CSS 转场(当前页滑出 + 背景占位渐显),**不再二次挂载路由组件**。连带拆除:
- `src/router/use-preview-routes.tsx` 整文件删除(~158 行:UNSAFE_RouteContext 依赖、路由树克隆 cloneRouteTree、lazyCache/useSyncExternalStore 缓存、PreviewRouteScope param 剥离 hack);
- `stores/index.ts:37-58` 的 `set` 包装器与 `setSuppressLoadingFlags` 全部删除(保存"冻结窗口吞掉 logout/reset"的缺陷 05 B3 / 06 B-01);
- `lib/use-swipe-back.ts` 保留手势识别(方向锁、bezel guard、`platformOwnsEdgeSwipe` 平台分派、history sentinel——这些是验证过的资产),仅重写 commit 阶段:去掉 preview 挂载路径,提交为普通 `navigate()` 并复核 `replace: true` 的历史语义(06 B-04);
- `app/layouts/dashboard-layout.tsx` 的 preview 挂载点与 `ROUTE_INFO` 全量预载循环一并删除(顺带修 06 Rt-06 的首屏预载退化);
- 相关测试更新:`use-preview-routes` 相关用例删除,swipe-back 测试改为转场断言。
收益:-2 个 store hack、-1 个 UNSAFE API 依赖、logout 丢数据缺陷消除、每会话轻挂载成本归零。

**ADR-3 虚拟化策略:走"分片订阅 → 局部虚拟化 → 轻窗口化"三步,消息列表不引重库。**【已拍板:按报告建议】
决策:chat 列表(message-row memo + LazyMarkdown 高度门 + 100 条分页窗口)、ledger(全量 table)、workspace 树(递归)、member-picker(1000 人直渲)四处全量挂载按以下顺序收敛:① 先做分片订阅(徽标/上传进度从消息对象拆出独立分片)与行级 memo;② ledger 与 workspace 树引 `@tanstack/react-virtual`(固定行高、低风险);③ 消息列表最后做"距视口 >2 屏降级为高度记忆占位行"的轻窗口化(避免与 markstream 高度突变打架),**不引入 react-virtuoso**。顺序铁律:**先拆文件与死代码,再上窗口化**,否则迁移面双倍。

### 5.3 应作为"不动点"保留的资产

重构以它们为基准,防止推倒重来:
- `components/ui/layer.ts` 三家族分层体系(146 行,全库堆叠一致性支点,有 5 场景测试)
- `MemoMarkdown` 的 props 冻结纪律、`LazyMarkdown` 的 fallback→swap + overflow-anchor 方案、`EMPTY_*` selector 常量纪律、conversation-list 的 memo+primitive 范式
- `connect/auth-interceptor.ts`(流式中途 401 覆盖,有测试)
- iOS 边缘手势三层工件(platform-edge-swipe/sentinel/yield,WebKit bug 240892/136531 仍活跃)
- stores 的竞态测试(约 623 行,`chat-stream/chat-history/chat.test` 锁死行为)与 95 个测试文件/611 it 中的行为断言风格(0 快照)
- i18n/layers 三个门禁脚本本身(修红后继续当 CI 用)
- base-ui + cva + cn 的组件基座与 secret-input 等体验件

---

## 7. 重构路线图(重设计版:已完成 5 批次,剩余三阶段)

> 原"四阶段"路线在执行中按依赖关系重排:数据层统一先于页面拆分完成度更高,页面拆分顺延;watch 断线重连与 ADR-2 提前完成。下列为重设计后的剩余路线。

### ✅ Phase 0 · 快赢与止血 — 已完成
`999e131`~`027009f`:死代码清扫(§4b 全清单,-500 行)、七连修(七项用户可见 bug)、`safeOpenExternal` XSS 白名单、ModelCombobox portal 化、`text-danger`/裸色语义 token、reminder/activity 轮询降噪 + device-login 治理、i18n 门禁修复(`common.deleting` 补齐 + 6 个死键清理)。

### ✅ Phase 1 · 数据层统一 — 已完成(聊天域收拢除外)
| 原计划项 | 落点 |
|---|---|
| 错误分类学 + showErrorToast | `389ce97`,40+ 处 codemod 收敛,auth 五页 + settings 全域 |
| useResourceList 列表基建 | `723de0e`,command/reminder 已迁;设置 7 页的脚手架迁移归入页面拆分阶段 |
| 引入 TanStack Query | `b95c530`+`9c9c353`+`e7aca3a`:api-provider/mcp/user/agent/machine 五个 slice 已纵切;~~聊天域 + presence/activity/reminder 收编未动~~(聊天域经批 4 ChatGateway 收敛;presence/activity/reminder 经批 8 `b7a3799`~`53b9cc3` 全部迁 Query) |
| 拆除全局 store 冻结 + ADR-2 | `d4774c3`,preview 体系整体退役 |
| fetchChannels equal-bailout | `c9fe388`(扩展到六列表) |
| 无界缓存 LRU | `5efb461` |
| 统一释放注册表 | `e440ba0`(logout 手工清单归零;avatar/image-blob 的 lib 自注册归位待批 4) |
| watch 断线重连(原列于事件管线阶段) | `4b7cf57`,提前完成 |
| 乐观发送编排下沉 | ⏳ 未动(归入批 4 聊天域) |

### ✅ Phase 2(重设计)· 聊天域收拢 — 已完成(流式决策待产品确认)
1. **ChatGateway** ✅ `4085c66`:channel/thread 双 25s 长轮询 watcher 合一(`stores/chat-watcher.ts` 共享 round loop:同步首启 + abort-aware 退避 + 隐藏页暂停/回前台立即续发);5s badge interval 同节拍并接入可见性门控(`startBadgeInterval`);`channelWatchers` 句柄带 `badge.stop()`,reset 不再泄漏 visibilitychange 监听;`command.ts` prune/recency 复核无恙;`chat-stream` 竞态基线全绿并新增隐藏页暂停/恢复用例;
2. **useChatComposer** ✅ `5c6665c`:双份 ~600 行收编为 `composables/use-chat-composer.ts` + `components/chat/chat-composer.tsx`(per-surface keyed 挂载 + 宿主 draft map),ThreadSlice/ChatSlice append/patch/remove 三个 action 消灭全部组件内联 `useAppStore.setState`(05 D5);**三个真实 bug 一并修复**——上传跨会话串台(per-surface 隔离)、@mention 残留(mentionMap 改为从草稿文本派生)、发送失败输入不恢复(恢复原文并重派生 mentions);回归测试 `chat-composer.test.tsx` + `chat-optimistic.test.ts`;
3. **流式管线拆除** ✅ `07b2799`(产品确认后执行):streaming 字段/rowStreamingProps/typing-dots/fade 全部移除,MessageRow 渲染走纯 final 内容路径;
4. **批 4 归位** ✅ `bc65511`:avatar/image-blob 失效回调迁回 lib 自注册(auth.ts 不再持 per-cache shim);presence/activity/reminder/machine-new 的组件 interval 全部收编 `usePolling`;
5. reset() 的 watcher 枚举随 `badge.stop()` 形状调整完成(注册表已就位)。

### ✅ Phase 3(原 Phase 2)· 页面与组件拆分 — 完成(批 5,`627bf6b`~ 17+ 提交)
1. **settings 7 页脚手架迁移** ✅ `627bf6b`~`79351ff`:四原语(useResourceQuery/useCrudDialog/ResourceSheet/ConfirmActionDialog)+ MemberEditor/lib/slug/members;groups→roles→api-providers→mcp-servers→idp 逐页迁移并修 B4/B7/B9;iam 特化(use-iam-policy + 双 Sheet 拆出,B8);尾部(profile/smtp/storage 挂 SettingsPage + contentWidth、私有 Field→FieldRow、general 四 toggle 合并、audit B6/B12);idp 测试 0→9;
2. **thread-panel 拆四件** ✅ `3c64942`(790→393 主文件 + replies/header/task-controls 三件;composer 已在批 4 拆出);
3. **agent-profile 三棵树** ✅ `06f026c`+`f4e5a36`:AcpConfigEditor + useAcpConfigDraft + usePiModelOptions(2379→1168,24/24 测试保绿);✅ machine-profile `b6664ad`(2184→763,AddAgentSheet 独立化 + 校验单源,create 模式复用,双页 49/49 保绿);
4. ✅ `a40e83a`:global-search 手写 pickers(~460 行)重建于 Base UI combobox 家族并抽独立文件(1011→477)+ 首个测试;sidebar 拆三件 `8c583f6`(457→49);⏳ TwoPaneShell 合并与手势统一留待批 6(与 modal 收敛同类);
5. **测试补齐(第一梯队)** ✅ `f762c1b`(chat-conversation 6 用例)、`d88e0de`(machine-new 5 用例,含 B2 否认回归)、`a40e83a`(global-search 4 用例)。第二梯队(38 清单余量)随批 6 推进。

### ✅ Phase 4(原 Phase 3)· UI 体系与事件管线 — 完成(批 6,`5fdf8fb`~`a2a31bc`,23 提交)
1. Badge 家族 ✅ `5fdf8fa`:范型 StatusBadge(五胞胎收敛,查表保留在 lib/)、Badge 色调 `destructive`→`error`(与 Alert/Toast 词表统一,Button 的 destructive 保留为危险动作语义)、`size="sm"` 小字徽章、Button size `default` 别名删除、Avatar 显式尺寸映射;组件 API 约定(ref-as-prop/cva 不导出/cn 范式/size 与色调词表/portal/禁 asChild)与 Separator 决策(承认 `border-t border-control-border` 惯例)写入 frontend/AGENTS.md;modal 壳共享(`LAYER_BACKDROP_SET` + Title/Description 常量,`da3f234`)、弹层四复制收敛 positioned-popup、ModelCombobox portal 化 prop;
2. `lib/command-events-model.ts` ✅ `b3c2644`:merge×4→1(`mergeOutputRuns`,seqNo 全序 + 稳定排序,行键漂移根治)、kind/ostream 注册表迁入、isToolCallError 单点(chat 侧 error 徽章修复 `f01f818`)、safeStringify 入 lib;preview 收敛 ✅ `16a10c8`(CommentsPanel + FilePreviewShell + useHtmlPreviewBridge,F-B9/F-S2/F-S3/F-B10);
3. ADR-3 ②③ ✅:ledger 虚拟化 + 100KB 截断 + SearchInput/250ms 防抖(`38f78af`)、workspace 树扁平化 + 虚拟化 + role=tree 与键盘导航 + 树内搜索(`a4dcabf`)、消息列表轻窗口化(`use-windowed-message-range`,`2f3e949`);ActivityState 魔数 ✅ `3c9b9c5`;proto `tool_call_id` ✅ 批 9 完成(`68e53a6`~`baf4167`,ID 优先配对 + FIFO 兜底);
4. UI 侧收尾 ✅:`toast.ts` 改 `Toast.createToastManager()` 官方工厂(`9655606`,as any 洞消除)、PWA controllerchange reload 增用户可见 toast 护栏(`0db2492`)、Separator 决策落地(AGENTS.md)、tsconfig 覆盖 vite/sw(`d8c8e46`,type-check 现跑 node 工程);TwoPaneShell/DetailTabsLayout/RailRow 布局收敛(`8ed13c3`)、手势统一 + TransferOwnershipDialog + useMessageScroller 搬移 + activity-detail 死契约(`d861c56`/`663867b`/`10cb1bb`/`c84281f`)同批清偿。

### 贯穿全程的规则(部分已落地)
- ✅ AGENTS.md 幽灵引用修正(`b0499db`);✅ 组件直连 `useAppStore.setState` 禁令已入 check 门禁(`check-store-writes.mjs`,批 7 `4dad293`;设置域 *ServiceClient 直连收敛余量仍在 09 章 §2 清单);
- ✅ tsconfig 已覆盖 vite.config 与 sw(`d8c8e46`,type-check 现跑双工程);✅ **Biome `useExhaustiveDependencies` + `useHookAtTopLevel` 已启用**(批 11 `96963cc`:25 处存量清偿后上线,惯用法以带理由 biome-ignore 固化;5 处 profile 页 no-op eslint-disable 全删);✅ **路由名单一真相已缝合**(批 12 `e393232`:RouteName 联合 + ROUTE_INFO 穷尽 + handle satisfies + backTo 改路由名,新增页面漏配移动端 chrome 即编译失败);
- ✅ `frontend/AGENTS.md` 已补:组件 API 约定与 Separator 决策(批 6 `5fdf8fb`)、Store Write Surface 写入面策略(批 7 `4dad293`)。

---

## 8. 工作量与收益总账(重设计版)

| 阶段 | 状态 | 实际产出 |
|---|---|---|
| Phase 0 快赢 | ✅ 完成(7 提交) | 七项高危 bug 清零、-500 行死代码、i18n 门禁恢复、reminder/activity/device-login 轮询治理 |
| Phase 1 数据层 | ✅ 完成(批 1~3,11 提交) | Query 五 slice 纵切、ADR-2 -2 个 hack、useResourceList、错误出口单点化、缓存 LRU、注册表、watch 重连、usePolling |
| Phase 2 聊天域收拢(重设计)| ✅ 完成(批 4,3 提交)| ChatGateway watcher 合一 + 可见性门控、useChatComposer 收编 -600 行与 9 处内联 setState、三个聊天域 bug 修复、interval/cleanup 归位;**余:流式拆除待产品确认** |
| Phase 3 页面拆分(重排)| ✅ 完成(批 5,19 提交)| settings CRUD 四原语 + 7 页全迁移(settings -40%+)、三棵组件树收拢、idp/machine-new/chat-conversation 测试从 0 补齐 |
| Phase 4 UI/事件管线 | ✅ 完成(批 6,24 提交)| TimelineModel 4→1 + 行键统一、badge/modal/弹层收敛 + AGENTS.md API 约定、ADR-3 ②③ 虚拟化与轻窗口化、preview 收敛、TwoPaneShell/手势/Ownership 收敛;proto `tool_call_id` 留待后端立项 |
| 05 章 stores 收尾 | ✅ 完成(批 7,6 提交)| types.ts 903→51 行(ui-models + 接口同文件)、SliceSet 写入面授予注册表、conversations 私有化(reset 即清)、delay.ts 更名、表驱动 reset 测试、写入面守卫入 check;全量 120 文件/783 用例双跑 exit 0 |
| 05 章收官(批 8)| ✅ 完成(6 提交)| presence/reminder/activity 数据源全部迁 Query(refetchInterval 可见性门控 + 结构共享;手写 equal ×3、requestSeq、首屏 merge hack 消解),三 slice 退役 19→16 组合;心跳 hook 保留;全量 120 文件/797 用例双跑 exit 0 |
| tool_call_id 全栈贯通(批 9)| ✅ 完成(3 提交)| proto 两 payload 增 `tool_call_id`(Go/proto-es/grpc-doc 重生成)、ToolCallSink 接口 + 后端全部发射点透传、前端配对 ID 优先 + FIFO 兜底(并发交错根治);后端 6 包测试全绿,全量 120 文件/798 用例 |
| activity 双分页收敛(批 10)| ✅ 完成(1 提交)| 08 F-S8 产品拍板统一无限滚动:桌面分页栈退役、双端一套 token 栈 + sentinel(316→235 行),5s 轮询统一骑第 0 页(offset 分页稳定窗口),i18n 死键清理;全量 120 文件/799 用例双跑 exit 0 |
| hooks 门禁 + 杂项清偿包(批 11)| ✅ 完成(1 提交)| Biome hooks 正确性规则上线(25 处存量清偿、AcpConfigEditor 转 ref-as-prop、死 eslint-disable/幽灵 overrides/重复脚本清理)+ 08 杂项(F-B6 inspector WARNING 收进 summary tab、F-B9 overlay `.then` 防护、F-B10 核实批 6 已修)+ 07 杂项(共享 Spinner、MobileTabBar→RouterLink、member-picker 行 memo + 徽章归一);全量 120 文件/800 用例双跑 exit 0 |
| 数据层与 lib 收尾清偿(批 12)| ✅ 完成(6 提交)| useResourceList 退役(05 章终局:command-list 迁 Query、CommandSlice 死缓存删除)+ `async-memo-cache` 缓存统一与 B-02/B-03/B-09 三竞态修复 + command-status 拆 time-format/resource(死导出 ×3)+ 路由名 RouteName 缝合(backTo 改名、反查删除)+ settings B10/B13/B15;全量 121 文件/809 用例双跑 exit 0 |
| 08 章终局(批 13)| ✅ 完成(4 提交)| overview 真实时间轴 + 500 span 上限 + F-B5 拖选几何统一(F-P4/F-B5,`f462da9`)+ `ui/side-panel.tsx` 共享壳与 inspector/CommentsPanel 换壳(F-S5,`fd9419a`)+ AgentSelect 收编共享 Select(F-S7,`49ae3c4`,附三用例)+ agent-profile 满载 flake findBy 化;全量 123 文件/821 用例双跑 exit 0 |
| 06 章终局(批 14)| ✅ 完成(5 提交)| handle.permission 路由权限守卫(Rt-02,`f31b591`:14 handle 声明 + 响应式 RoutePermissionGate + 禁止面/返回首页,i18n 三键)+ 手势引擎合一(P2,`fef3b17`:useEdgeDrag 引擎,use-swipe-to-close-sheet 退役,-197 行重复)+ generatePath(Rt-03,`3847640`)+ hooks 目录合并(R-04,`8acfe2f`:29 文件归一 src/hooks/,63 文件导入面)+ tailwind @theme(E-01,`cc7e298`:v3 配置删除,-127 行死配置);全量 124 文件/831 用例双跑 exit 0 + vite build 产物验证 |

**已完成部分的实际收益(截至批 3)**:四道门禁全绿的测试规模 95→103 文件 / 611→686 用例;高危 bug 十项中**七项已修**(余三项在聊天域收拢内解决);错误呈现 5 种→1 种出口 + 2 处记录在案;轮询策略收敛(可见性门控、终态停轮、重连退避);预存竞态(fetchChannels 族)与三处无界缓存根治;swipe-back 的 UNSAFE_API + 冻结 hack 全部拆除。

**不建议做的**:全量自研 mini Query;把 19 个 slice 拆成 19 个独立 store;为聊天列表直接引入 react-virtuoso 全家桶(先轻窗口化);为"性能"提前上 React Compiler;在未拍板流式管线去留前迁移 MessageRow 的虚拟化。

---

## 附录:审查覆盖与方法

- **模块章节(^ = 本报告)**:`docs/refactor/01-settings-pages.md`(29KB)、`02-giant-profile-pages.md`(31KB)、`03-dashboard-rest.md`、`04-chat-components.md`(28KB)、`05-stores-composables.md`(42KB)、`06-infra-lib-router.md`(41KB)、`07-ui-components.md`、`08-events-preview-workspace.md`(37KB)、`09-auth-crosscutting.md`(26KB)。
- 每章节均声明逐文件精读 + grep 交叉验证;本总报告对四处最高危结论(machine-new 复活、MCP key 失焦、streaming 无生产者、closeTask 零调用)做了独立复核,全部属实。
- 统计口径:i18n 双语各 1,324 叶子 key;生产文件 222 个/48,214 行;测试 95 文件/611 it/0 快照;hook 统计与 60 处直连 RPC 清单见 09 章 §2。