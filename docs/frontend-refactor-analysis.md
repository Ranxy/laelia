# 前端深度重构分析报告

> 分析对象:`frontend/**` — 生产代码 222 个文件 / **48,214 行**,测试 95 个文件 / **17,969 行**(611 个 it 用例、0 快照),不含生成的 proto-es 代码。
> 分析方法:9 个模块级深度审查(逐文件精读、grep 交叉验证、全部死代码结论经全仓引用核验)+ 全局横切统计(git 热度、hooks 密度、分层扫描、i18n 双语 diff)+ 架构骨架人工审读。
> 详细证据:分模块报告共 9 份、约 2,400 行,见 `docs/refactor/01~09`。本文件是执行摘要与总体决策。

---

## 0. 执行摘要:十项最高价值行动

如果只做十件事,按"单位工作量收益"排序如下(前三项合计 3~4 天,即可消掉全部用户可见高危 bug 与约 800 行死代码):

| # | 行动 | 类型 | 规模 | 详细依据 |
|---|---|---|---|---|
| 1 | **P0 修配包**:MCP header 输入框每键失焦(`key={name-i}`)、API Provider 编辑串数据(ProviderSheet state 残留)、machine-new「不是我」按钮失效、ThreadReplies `mentionLabel` 漏传、IAM/角色空表渲染 "No agents yet."、TableHead 排序/列宽竞态、删除按钮文案 "Saving…" | 真实 bug ×7 | 1~1.5 天 | 01 §3、03 §3、04 §3、07 §4 |
| 2 | **死代码大扫除**(全部经 grep 验证零引用):`command-timeline.tsx`、`CommandTerminal`、`MobileSidebar`、`use-auto-scroll.ts`、`ui/tooltip.tsx`、`ui/separator.tsx`、`closeTask` action、`refreshAgentProviders`、4 个 lib 死导出、Sheet 6 个未用宽度档、3 个 variants 导出、若干死 i18n key | 死代码 | ~500 行,半天,零风险 | 07 §8、08 §2、05 §6、06 §7 |
| 3 | **安全与门禁修复**:修复已变红的 `check-react-i18n` 门禁(补 `common.deleting`、清 3 个 unused key);`window.open(href)` 加 scheme 白名单堵 XSS 面;ModelCombobox portal 化(全库唯一 layering 政策违例 + 真实裁剪 bug) | 安全/门禁 | 1 天 | 09 §2.1、08 §4 F-S1、07 F-Bug-1 |
| 4 | **错误处理单点化**:`connect/errors.ts` 建立错误码→i18n/重试/去重的统一映射;`showErrorToast()` 收敛全库 146 处 `toastManager.add` 与 26 处裸 `err.message`(同一文件里 describeError 与裸 message 混用) | 设计统一 | 0.5~1 天 | 06 C-04、01 D6、09 A-3 |
| 5 | **列表获取基建**:`useResourceList`(请求序号/AbortController + initialLoading/refreshing 分离)替换 7 页设置页 + command/reminder 列表三胞胎,根治整类"旧响应覆盖新页面"竞态 | 重复+竞态 | 1~2 天 | 01 §7、03 R1/B1、05 D2 |
| 6 | **统一发送/乐观更新管线**:抽 `useChatComposer`(channel/thread 双份 ~600 行逐行同构),9 处组件内联 `useAppStore.setState` 改为 slice action | 重复+设计 | 2~3 天 | 04 §1.1/§1.2、05 D5 |
| 7 | **引入 TanStack Query**(已拍板 ADR-1,详见 §5.2),逐步接手"列表+分页+silent 刷新+equal 比较+失败吞掉"的手写样板;swipe-back 预览改 CSS 转场(已拍板 ADR-2)并废除全局 store 冻结 | 架构 | 2~3 周(分域) | 05 §7、06 B-01 |
| 8 | **决定流式渲染管线去留**:`ChatMessageUI.streaming` 全库无生产者(生产代码 `rowStreamingProps` 全部传 `false`),整条 typing-dots/fade/streaming-props 链是旧 DM 架构遗产。确认产品不再要 token 流式 → 一次性拆除(MessageRow 接口面 -7 props) | 历史债务 | 0.5 天,需产品确认 | 04 §5.1 |
| 9 | **三个巨型页面拆分**(合计 6,771 行、~100 个 useState、25 个 effect):agent-profile / machine-profile / chat-conversation 按 02 章方案拆为组件树,并顺手修掉"附件上传跨会话串台"等 4 个真实 bug | 重构+bug | 2~3 人周 | 02 全篇 |
| 10 | **事件渲染管线统一**:合并 4 份"输出块合并算法"为 `lib/command-events-model.ts` 单一纯函数(语义已分叉,行键漂移导致 inspector 打不开),watch 流加断线重连 + chunk 合批 + LRU 上限 | 重复+性能 | 1 周 | 08 §3/§7 |

**量化总览**:全部建议落地后,预计净删 **7,000~9,000 行**(约 15~19%),修复 **约 30 个已定位 bug(其中高危 12 个)**,收敛 **6 套互不一致的轮询策略、5 种错误呈现、4 套时间格式化、3 套 size 词典**。整体规划约 **8~12 人周**,Phase 0(见 §7)一周内可完成。

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
- 类型纪律好:`as any`/`@ts-ignore` 全库仅 5 处;但 `useExhaustiveDependencies` 未启用 —— **15+ 处 `exhaustive-deps` 黑洞目前零守卫**,且 4 处 `eslint-disable` 注释因 linter 是 Biome 而是无效 no-op。
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
10. **FIFO 工具调用配对** — proto 无关联 ID,并发工具必错配;chat 侧 error 状态显示成灰色 [08 F-B1/F-R3,需 proto 联动]

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

## 7. 重构路线图(四阶段,每步独立可合入)

### Phase 0 · 快赢与止血(第 1 周,3~5 天)
零风险纯减法 + 用户可见 bug 直修,全部不依赖架构决策:
1. 死代码大扫除(§4b 清单,-500 行)+ 3 个死 i18n key + 修 `check-react-i18n` 红灯(补 `common.deleting`);
2. 七连修:MCP key 失焦、ProviderSheet 重置、machine-new dismissed、mentionLabel 透传、`common.no-data` 文案、表格 TableHead 竞态、Checkbox onClick 分支删除;
3. `safeOpenExternal` scheme 白名单 ×2 处 + ModelCombobox portal 化;
4. `text-danger`→`text-error` codemod(13 处)、裸色 5 处改语义 token;
5. 轮询止血:reminder-detail 终态停轮、reminder/activity 2s→5s + visibility gating(请求量 -60%);
6. device-login 轮询治理(终态停止、退避、后台暂停)。

### Phase 1 · 数据层统一(第 2~4 周)
1. `connect/errors.ts` 错误分类学 + `showErrorToast`,迁移 146 处调用(渐进,新旧并存);
2. `useResourceList`(请求序号/Abort + initialLoading/refreshing)落地,command/reminder 列表三胞胎先迁,设置 7 页跟进;
3. directory store(users/groups/roles 会话缓存 + invalidate);8 处 try/finally 补 catch;
4. **引入 TanStack Query**:先 rosters/setting 纵切(三个 slice 文件消失),再 presence/activity/reminder 收编,最后聊天域 ChatGateway(双 watcher 合一、11 个定时器收敛、以现有竞态测试为行为基准);
5. **执行 ADR-2**:退役 `use-preview-routes.tsx`(UNSAFE_RouteContext/路由树克隆,~158 行),删除 `setSuppressLoadingFlags` 全局冻结与 dashboard-layout 的 ROUTE_INFO 预载循环,swipe-back 提交阶段改普通 navigate(复核 replace 语义);乐观发送编排下沉为 slice action/useMutation(10 处组件 setState 消灭);`fetchChannels` equal-bailout 先行单点(20 行,立刻消除每 5s 全列表重渲染);
6. 三处无界缓存 LRU 化(command activeOutputs/主动驱逐、threadByRoot、消息视窗)。

### Phase 2 · 页面与组件拆分(第 5~8 周)
1. thread-panel 拆四件 + `useChatComposer` 收编双份管线(消 600 行);
2. agent-profile / machine-profile 按 [02 章] 三棵组件树迁移(ACP 表单抽 `AcpConfigForm` 共享件,消 650 行复制);chat-conversation 同期,顺带补齐其测试;
3. settings 7 页按 [01 章] 脚手架逐页迁移(groups→roles→api-providers→mcp→idp→iam),每页一 PR 测试随迁;
4. global-search 的 460 行 Combobox 轮子换共享 `Combobox` + 双筛选栏合并;sidebar 拆三件;rail+pane/TwoPaneShell 合并 4 布局;`useEdgeDragToClose` 统一手势。

### Phase 3 · UI 体系与事件管线(第 9~10 周)
1. Badge 家族(xs variant + 范型 StatusBadge)、modal 壳/弹层三连提取、size/variant naming codemod、组件 API 约定写入 AGENTS.md;
2. `lib/command-events-model.ts` 统一 merge/pair/kind(4 份拷贝归一);ledger 虚拟化 + 输出截断 + 搜索 debounce;overview 改真实时间轴;
3. 至 proto 团队:ToolCall 事件加 `tool_call_id`(彻底修 FIFO 错配)、ActivityState 之类枚举不再写魔数;
4. 消息列表轻窗口化(ADR-3 第③步,在 Phase 2 拆分完成后);
5. 补测试:38 个无测试大文件按风险排序(device-login、settings-identity-providers、machine-new 优先)。

### 贯穿全程的规则(写入 AGENTS.md 并加 lint)
- 组件禁止直接 `useAppStore.setState`(当前 10 处)+ 禁止页面直连 `*ServiceClient`(settings 域 60 处收敛进 store/hook);
- Biome 打开 `useExhaustiveDependencies` 等正确性规则(先修 15+ 存量黑洞),tsconfig 补 sw/vitest 覆盖;
- 文档随代码:修正 AGENTS.md 的三处幽灵引用;Sheet/Dialog/Checkbox 决策与组件 API 约定成文。

---

## 8. 工作量与收益总账

| 阶段 | 工作量 | 直接收益 |
|---|---|---|
| Phase 0 | 3~5 天 | 修掉全部高危用户可见 bug(10 个)、-500 行死代码、恢复 CI 门禁、网络请求 -60% |
| Phase 1 | 3~4 周 | 消灭整类竞态与静默吞错;stores 4,739→~2,800 行;11 个定时器→1 gateway;删除全局 freeze |
| Phase 2 | 3~4 周 | 三个 2000+ 页面与 thread-panel 共拆掉 ~4,000 行;settings 页 -40~50%;测试盲区收窄 |
| Phase 3 | 2 周 | 事件管线归一(4 拷贝→1)、badge/modal 收敛(~400 行)、proto 级修复 |
| **合计** | **约 8~12 人周** | **源码 -7,000~-9,000 行(15~19%);重复模板全部单点化;已验证 30+ bug 清零** |

**不建议做的**:全量自研 mini Query;把 19 个 slice 拆成 19 个独立 store;为聊天列表直接引入 react-virtuoso 全家桶(先轻窗口化);为"性能"提前上 React Compiler;在未拍板流式管线去留前迁移 MessageRow 的虚拟化。

---

## 附录:审查覆盖与方法

- **模块章节(^ = 本报告)**:`docs/refactor/01-settings-pages.md`(29KB)、`02-giant-profile-pages.md`(31KB)、`03-dashboard-rest.md`、`04-chat-components.md`(28KB)、`05-stores-composables.md`(42KB)、`06-infra-lib-router.md`(41KB)、`07-ui-components.md`、`08-events-preview-workspace.md`(37KB)、`09-auth-crosscutting.md`(26KB)。
- 每章节均声明逐文件精读 + grep 交叉验证;本总报告对四处最高危结论(machine-new 复活、MCP key 失焦、streaming 无生产者、closeTask 零调用)做了独立复核,全部属实。
- 统计口径:i18n 双语各 1,324 叶子 key;生产文件 222 个/48,214 行;测试 95 文件/611 it/0 快照;hook 统计与 60 处直连 RPC 清单见 09 章 §2。