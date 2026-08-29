# Laelia Frontend 深度审查报告:基础设施 / connect / router / app 布局层

> **决策状态更新**:本报告 P0 首项(废除 `suppressLoadingFlags` 全局冻结)与 Rt-05(UNSAFE_RouteContext)已由总报告 **ADR-2 拍板**:`use-preview-routes.tsx` 整体退役、冻结删除、swipe-back 保留手势识别仅重写提交阶段(复核 `replace: true` 历史语义,Rt-05/B-04/B-01 一并解决);错误映射单点化(connect/errors.ts)维持 P0 不变。

> 评审范围:`src/lib/` 33 个源文件(~2900 行,另附 8 个测试文件)、`src/connect/`(164 行)、`src/router/`(609 行)+ `src/router/routes/`(503 行)、`src/app/` + `src/app/layouts/`(628 行)、`src/types/` 非 proto-es 文件、`vite.config.ts`、`vitest.config.ts`、`tsconfig*.json`、`biome.json`、`index.html`、`sw/sw.ts`、`tailwind.config.js`、`src/test/setup.ts`、`frontend/scripts/check-react-layering.mjs`、`check-react-i18n.mjs`。全量阅读,所有结论经 grep 交叉验证。

## 总体评价

这套基础设施的**单文件质量普遍高于平均水平**——注释密度高、边界(iOS 边缘手势、PWA 更新、OAuth state、i18n 竞态)的处理有理有据且多有测试。真正的债不在这类"深度",而在三个结构性问题:

1. **手势栈用全局副作用串联(store 冻结 monkey-patch + history sentinel + window 级 touch 监听 + preview 路由克隆)**,任何一环的时序假设破裂都会产生难以复现的 UI 错乱;
2. **同名资源具备 2~3 份"真相"**(路由名 3 处、断点语义 2 处、资源名构造函数散布、双 member-picker、双 slugify、3 个同构 blob 缓存);
3. **配置层漂移**——`tailwind.config.js` 是 Tailwind v3 遗物、biome overrides 指向不存在的文件、`sw/` 不在任何 tsc project 内。

---

## 一、基础设施质量(lib/)

### 1.1 lib/ 文件职责与引用度全景

grep 统计的"被引用文件数"(不含 lib 自身与测试):

| 引用 | 文件 |
|---|---|
| **1** | agent-token(且仅被 machine-token 引)、markdown、permissions、pwa、reminder-status |
| 2–5 | caret-position、clipboard-file、file-upload、image-blob-cache、machine-token、oauth、task-status、user-filter、web-push、image-file、image-resize、presence、use-history-sentinel、file-download、html-file、i18n、tool-call-events、use-auto-scroll、use-swipe-back、use-swipe-to-close-sheet |
| 8+ | markdown-file(8)、platform-edge-swipe(8)、connect-errors(14) |
| 17+ | command-status(17)、avatar-cache(23)、use-is-desktop(23)、toast(52)、utils(68) |

**没有零引用文件**;5 个单引用文件中 `markdown.tsx`(侧效导入)、`permissions.ts`(静态目录)、`pwa.ts`(仅 main.tsx 调用)是合理形态,`agent-token.ts` 是错位(见下)。

**【中】R-01 `agent-token.ts` 名不副实且含死导出**
- 位置:`src/lib/agent-token.ts:1-20`
- 问题:文件名暗示 agent 令牌管理,实际只有 `formatToken`(打码工具)与 `getManagerURL`。`formatToken` 全库 **0 引用**(死代码);`getManagerURL` 唯一消费者是 `machine-token.ts:1`,即该文件其实是 machine 安装命令的私有依赖,被放进了错误的主题文件。
- 证据:`import { getManagerURL } from "./agent-token";`(machine-token.ts:1);`grep formatToken` → 无任何 lib 外调用点。
- 建议:删除 `formatToken`;将 `getManagerURL` 并入 `machine-token.ts`(或独立 `manager-url.ts`),消灭 `agent-token.ts`。

**【高】R-02 `command-status.ts` 是伪装成"状态映射"的杂物抽屉**
- 位置:`src/lib/command-status.ts:13-131`
- 问题:17 个文件依赖它,但内容横跨 5 个不相干职责:CommandStatus/EventType → i18n key + Badge variant 映射、4 个互不复用的时间格式化器(`formatDuration`/`formatTimestamp`/`formatTimeOfDay`/`formatActivityListTime` 与 `formatConversationListTime` 各写一套"/分"拼接逻辑)、以及资源名工具(`agentResourceName`/`commandResourceName`/`roleIDFromName`)。
- 证据:`command-status.ts:95-109` 手写 `HH:MM` / `M/D` / `YYYY/M/D` 三段格式;`command-status.ts:111-131` 是纯资源名函数,与命令状态无关。
- 建议:拆为 `lib/format/time.ts`(统一全部时间格式化,含 locale 策略)与 `lib/resource.ts`(AIP 资源名解析,与 `avatarNameForUserId` 等合并);状态→variant 映射保留。

**【中】R-03 三个同构模块级缓存,三套实现**
- 位置:`src/lib/avatar-cache.ts:23-40`、`src/lib/image-blob-cache.ts:10-54`、`src/router/use-preview-routes.tsx:17-51`
- 问题:三者都是 "Map 缓存 + inflight 去重 + 失效广播",但失效通知机制各不相同(avatar 用 epoch+`useSyncExternalStore`、image-blob 无通知、preview 用 version+listener)。每个都自带 FIFO/全清逻辑,边界行为不一致(竞态差异见 B-02/B-03)。
- 建议:沉淀一个 `createAsyncMemoCache<T>(fetcher)` 工具(inflight 去重 + invalidate + 失效订阅),三个站点各 10 行接入。

**【中】R-04 hooks 目录二义:`lib/` 与 `composables/` 并存、命名风格混杂**
- 位置:`src/lib/use-*.ts`(6 个 kebab-case)vs `src/composables/`(4 个文件:`useAvatarEditor.ts`、`useMentionDetect.ts`、`useMentionTargets.ts`、`use-presence-heartbeat.ts`——3 个 camelCase + 1 个 kebab-case)
- 问题:同样"裸函数集合 + hook"的东西,一个目录叫 lib(React 词汇),一个叫 composables(Vue 词汇);`use-presence-heartbeat.ts` 是应用级 hook 却不在 lib。对 33 个 lib 文件的新人而言"该放哪"没有可执行规则。
- 建议:合并进单一 `src/hooks/`(kebab-case);保留 lib 放非 React 纯函数。

**【中】R-05 `lib/toast.ts` 依赖 Base UI 未公开接口 + 前导空格属性 hack**
- 位置:`src/lib/toast.ts:9-34`,被 52 个文件 / 146 处 `toastManager.add()` 使用
- 问题:`" subscribe"(fn)` 属性名带前导空格以反射匹配 Base UI `ToastManager` 的内部形状;`close(_id)` 是空实现。`interface` 与实现之间靠文件末尾的 `as {…}` 断言缝合。Base UI 任意内部改动(改属性名、改方法签名)即静默失效——146 个调用点没有编译期防护。
- 证据:`toast.ts:10` `" subscribe"(fn: Listener)`;`toast.ts:27-28` `close(_id?: string) { // Base UI handles toast lifecycle internally. }`
- 建议:要么给 Base UI 提 issue/等官方 adapter,要么自建 20 行 store 型 toast(与 layer 政策一致),别让全局错误提示通道建立在对第三方私有形状的字符串反射上。

**【中】R-06 `use-auto-scroll` 的 `deps: T[]` 直通 useEffect 是 React 反模式且零防护**
- 位置:`src/lib/use-auto-scroll.ts:7-19`
- 问题:`deps` 数组直传 `useEffect`,靠 `eslint-disable-next-line react-hooks/exhaustive-deps` 压制警告——**但本项目 linter 是 Biome,这行注释是 no-op**;Biome 的 `useExhaustiveDependencies` 规则又未启用,所以这条最危险的模式处于完全无守卫状态(调用方传错依赖项 → 滚动定位静默失灵)。
- 建议:改签名为显式数据(如 `useAutoScroll(ref, scrollSignal)`,由调用方传一个递增的 count/最后消息 id),或启用 `lint/correctness/useExhaustiveDependencies`。

**【高】R-07 `web-push.ts` 让 lib 层依赖 stores——分层规则名存实亡**
- 位置:`src/lib/web-push.ts:2`(`import { useAppStore } from "@/stores"`)
- 问题:唯一的"架构守卫脚本"(`check-react-layering.mjs`)只管 z-index/portal;lib → stores → lib(avatar-cache/connect)的环状依赖没人管。`connect/index.ts:28-30` 注释甚至明确记录了靠动态 `import()` 绕过 connect↔stores 循环。这类"补丁式解耦"会随规模继续繁殖。
- 精确证据:`disableDesktopNotifications` 内部 `useAppStore.getState().currentUser?.handle`(web-push.ts:133)——`subscriptionName` 的 handle 参数不该从 store 里现取,由调用方传入;connect 的 401 处理改为事件总线或由 `main.tsx` 注入。

**【低】R-08 `oauth.ts` state 存储的孤儿泄漏**
- 位置:`src/lib/oauth.ts:23-28, 89-101`
- 问题:`laelia_oauth_state_{token}` 只有在读到(用户真正回到 callback)或过期被读到时才删除;用户中途关掉 IdP 授权页 → localStorage 永久残留(含 redirect 目标)。`startOAuthLogin` 在 `buildOAuthAuthorizeUrl` 返回 null 时已经先存了 state 再 return false(oauth.ts:90-99),同样留孤儿。
- 建议:写入时记录时间戳并顺手清理所有过期 entry(`for` 遍历 prefix);失败路径先 build 后 store。

**【低】R-09 `clipboard-file.ts` 的非图片无名文件兜底成 `image.bin`**
- 位置:`src/lib/clipboard-file.ts:24-25`
- 建议:按 `file.type` 补一个稍完整的 MIME→ext 表(或统一 `file.bin`);当前 `EXT_BY_MIME` 只有 4 种图片。

**【低】R-10 iOS 特殊处理未过时,但判据字符串硬编码**
- 位置:`src/lib/platform-edge-swipe.ts:22-29`
- 结论:这套 `platformOwnsEdgeSwipe` + sentinel/yield 机制**不是遗留**。WebKit bug 240892/136531(edge-swipe 与页面手势竞争、touchcancel 不送达)至今未修,三层工件在 home-screen PWA 仍可复现(注释 6-21 行有完整的机理记录,且有 382 行的 `use-swipe-back.test.tsx` 托底)。重构时**必须保留**,但建议:1) `navigator.vendor !== "Apple Computer, Inc."` 增加对 UA-CH 的兜底;2) 把机理注释抽为 docs/ 引用,lib 里留链接。

### 1.2 hooks 实现质量小结

- `use-history-sentinel.ts`(90 行):单 token 序列 + `setTimeout(0)` 延迟平衡 pop,处理了 StrictMode 重挂与堆叠 overlay;是本仓库最精巧的 90 行,但理解成本极高(注释即 30 行)。**保留,建议封装成 `overlay-history.ts` 专用模块并补文档**。
- `use-swipe-back.ts`(335 行):核心手势质量高(方向锁、bezel guard、touchcancel 即时重置、1000ms 导航兜底),但存在 B-01(store 冻结)与 B-04(replace 导航)的结构性问题。
- `use-swipe-to-close-sheet.ts`(197 行):与 `useSwipeBack` 的 thread 模式重复约 70%(同为方向锁+阈值+`SWIPE_BACK_*` 常量透传),且 `enabled` 参数表明它在 iOS 上整体旁路。**应合并**为 `useEdgeDragToClose(rootRef, { onCommit })`(中)。
- `useAvatar`(avatar-cache.ts:154-191):见 B-02/B-03 竞态。
- `useIsDesktop`(30 行):实现干净(useSyncExternalStore + server snapshot),但见 L-03 断点双定义问题。

---

## 二、connect 层

### 2.1 客户端创建与传输

**【中】C-01 17 个 client 手工列举,connect/index.ts 是唯一"手动注册表"**
- 位置:`src/connect/index.ts:76-101`
- 问题:每新增一个 proto service,需要手写一条 `export const xServiceClient = createClient(XService, transport)`。17 条重复模式无类型约束,漏一个 client 只会在运行时报错。
- 建议:维护一个 `const SERVICES = { Agent: AgentService, … } as const`,用 mapObject 生成 clients(仍可 tree-shake)。属于低风险高确定性的机械重构。

**【中】C-02 "auth-interceptor" 名不副实:不注入 token,只做 401 监听**
- 位置:`src/connect/auth-interceptor.ts:27-63`、`src/connect/index.ts:70-74`
- 回答"认证 token 注入"问:**本项目不存在 token 注入**——Web 端认证完全靠 `credentials: "include"` 的 cookie(connect/index.ts:72),access token 只存在于 CLI/机器侧。拦截器唯一的职责是捕获 `Code.Unauthenticated` 并触发登出重定向。命名应改为 `unauth-error-interceptor` 或 `session-interceptor`,否则新人会按名字去找根本不存在的"token 注入逻辑"。
- 流式响应的中间迭代包装(auth-interceptor.ts:33-54)是正确的——覆盖 stream 中途 401,且有 112 行测试兜底。**设计合理,保留**。

**【高】C-02a 401 → 硬跳转的路径判断与路由表重复**
- 位置:`src/connect/index.ts:39-46`
- 问题:`/auth/`、`/oauth/callback`、`/oauth/login`、`/login/device` 四个前缀在 `index.ts` 用字面量重新罗列一遍——与 `router/auth-redirect.ts` 的 `isAuthPath`/`isPublicPath` 是**同一事实的第三份拷贝**(路由表里还有第四份)。一旦新增公共路由(或改路径),这里必然漏。
- 证据:`auth-redirect.ts:25-31` `isPublicPath` 与 `index.ts:40-44` 逐条比对完全平行。
- 建议:把 `isAuthPath/isPublicPath` 提到与 `authRoutes` 定义同源的模块(如 `router/routes-meta.ts`),connect 层 import 之。

**【中】C-03 store reset 靠硬导航 + 动态 import 破环**
- 位置:`src/connect/index.ts:48-67`
- 评价:`onUnauthenticated` 中 `useAppStore.getState().reset()` + `setState({sessionLoaded:true})` + `window.location.assign`(硬跳转)是**安全且简单**的做法,配合 `authRedirecting` 锁防 401 风暴(connect/index.ts:22)。质量合格。但依赖 `dynamic import("@/stores")` 解决循环依赖属于 R-07 同一根因。
- 细节:`sessionLoaded: true` 的保留(connect/index.ts:56-59)注释清楚——好。

### 2.2 错误码 → UI 的映射(回答"在哪里、是否统一")

**【中】C-04 错误映射只有两级,其余散布在大量 toast 调用点**
- 现状:
  1. `Unauthenticated` → 拦截器统一处理(唯一全局映射);
  2. `PermissionDenied` 的结构化 detail → `connect-errors.ts:10-31` `permissionDeniedInfo` + `describeError`(14 个文件使用),展示逻辑仍由调用方各自拼装;
  3. 其余错误码(`Unavailable`/`ResourceExhausted`/`DeadlineExceeded`/`Internal`…)→ `err.message` 直出,`grep toastManager.add` 全库 146 处,`describeError` 只覆盖其中少数。
- 证据:`connect-errors.ts:37-49` 的 `describeError` 输出形如 `missing laelia.commands.get on agents/x` 的英文硬编码字符串;没有按错误码的差异化表现(如 `Unavailable` 的 hint、`ResourceExhausted` 的配额提示)、没有全局去重/节流;`detail.type` 判断用魔法字符串 `"laelia.v1.PermissionDeniedDetail"`(connect-errors.ts:17,建议改用 schema 的 `typeName`,防 proto 更名漂移)。
- 建议:在 connect 层加一个 `error-taxonomy` 中间件:统一 `ConnectError → { titleKey, description, recoverable, dedupeKey }`;`describeError` 迁进去并 i18n 化。**这是错误处理统一的唯一机会点**,越晚做调用点越难迁。

**【低】C-05 无超时/取消策略**
- 位置:`src/connect/index.ts:70-74`
- unary RPC 无 `timeoutMs` 约定(可 per-call 传但无约定);presence 轮询(fetchAgents 30s)与 channel watcher 靠各 store 自行 abort。建议:transport 层提供带 timeout 的调用辅助函数,或至少在文档约定。

---

## 三、router 设计

**先答"懒加载是否普遍"**:是,且执行彻底——`routes/dashboard.tsx` 中 60+ 个路由页全部 `lazy: () => import(...)`(dashboard.tsx:58-425),`routes/auth.tsx` 全部 lazy(routes/auth.tsx:12-68),两个 detail-layout 也 lazy(dashboard.tsx:134-137, 260-263)。静态进入首屏的只有 `RootLayout`/`DashboardLayout`/`SplashLayout` 及其依赖。评价为**良好**。

**【中】Rt-01 自制命名路由系统的三份真相**
- 位置:`handles.ts`(38 个常量)、`routes/dashboard.tsx`(每条路由再写一遍 `handle: { name: XX }`)、`route-info.ts`(再用常量作键声明 title/backTo)
- 问题:`buildRouteNameIndex`(route-index.ts:16-40)在模块加载时扫描路由树重建 `name→path` 映射(router/index.tsx:28),任何一条 `handle.name` 与 `ROUTE_INFO` 键不同步不会报编译错(`ROUTE_INFO` 用宽松 string 键)。三处文件加起来约 220 行只为维护 38 个名字。
- 建议:用类型缝合——`satisfies Record<RouteName, …>` 或在 `dashboard.tsx` 中直接对 `handles` 常量取值并断言其完整性,让漏配在 `tsc` 阶段爆红。

**【中】Rt-02 没有权限路由守卫,路由可达性叠加在页面自身**
- 位置:`routes/dashboard.tsx:303-426`(settings 全家桶),全文件无 `handle.permission` / loader 校验
- 问题:rootGuard 只做登录态。`/settings/iam`、`/settings/roles` 等高敏页面在后端自然 403(API 层兜底),但路由级权限为零——深链接可达后再失败,而不是路由级拒绝;移动端 Mobile Header/ROUTE_INFO 也没有做权限相关的可见性分支。
- 证据:grep 全 router 目录无 `permission`;仅有 `SetupChecklistGate`(dashboard-layout.tsx:74-77)自行检查 `laelia.settings.get`。
- 建议:扩展 route handle 为 `{ name, permission? }`,rootGuard(或 dashboard layout loader)统一跳 forbidden;成本一个文件,收益一处授权策略。

**【中】Rt-03 `resolvePath` 的字符串正则是 react-router 语法之外的第二套路由 DSL**
- 位置:`route-index.ts:60-86`
- 证据:`path.replace(new RegExp(`:${key}(?![A-Za-z0-9_])`, "g"), encodeURIComponent(single))` —— 不支持 splat、不支持可选段、**params 缺失时静默把 `:id` 留在 URL 里**、路径模板与 `path` 属性两处字符串一处真相。`agent-detail-layout.tsx:107/117/129` 等处用它导航。
- 建议:换成 react-router v7 的原生 `generatePath`/`<Link>`(v7 内置路径编译),或至少在 params 缺失时 `console.warn`。

**【低】Rt-04 `ROUTE_INFO.backTo` 用路径字面量而非 handles 常量且 15 次重复 "/settings"**
- 位置:`route-info.ts:48,56-58,81-89,90-146`
- 建议:`backTo` 改用 route name(`SETTINGS_ROUTE` 等)驱动 `resolvePath`,与 handles 同源。

**【中】Rt-05 swipe-back preview 依赖 react-router 内部 API `UNSAFE_RouteContext`**
- 位置:`use-preview-routes.tsx:9-14`(import)、`93-108`(剥 params 的 Provider hack)
- 问题:preview 机制的存在理由(注释 82-91 行)是绕 `useRoutes` 内部 params 合并行为——绑定到 react-router 实现细节,升级 react-router(当前 7.16)随时 break;`matchRoutes`/`cloneRouteTree` 都是绕过 data router 的旁路渲染。整个 preview + 哨兵 + 克隆路由树体系,为的是"拖动边缘时下一页在下面可见"——而手势提交后仍用 `navigate(target, { replace: true })` 做真正导航(use-swipe-back.ts:248)。
- 建议(重构视角):若保留手势,把 preview 升级为受支持的方案(预渲染 `matchRoutes` 元素或快照屏),或退而求其次改为"半透明快照屏"(纯 UI,不再 useRoutes 二次渲染),直接消除 store 冻结(见 B-01)与 UNSAFE 依赖两大风险。

**【低】Rt-06 DashboardLayout 挂载时预载所有 backTo 目标 chunk**
- 位置:`app/layouts/dashboard-layout.tsx:132-140`
- 问题:`for (const info of Object.values(ROUTE_INFO)) preloadPreviewRoute(...)` — backTo 集合覆盖 12+ 个独特路径(含全部 settings 子页 chunk),首屏就并行拉取,实质抵消这些路由的懒加载首跳收益。
- 建议:改为手势首次 hover/start 时按需预载(触摸开始前 100ms 预取也来得及),或仅预载 4 个顶层 tab。

**【低】Rt-07 lazy 返回 `element` 与 `Component` 混用**
- 位置:`dashboard.tsx:123-130, 240-246`(index 路由用 `element: <m.SelectionEmptyState .../>` + lazy)vs 全部其余 `Component:` 形态
- 建议:统一为 `Component` 包装小组件,减少一种分支形态。

**【低】Rt-07a 模块加载副作用注册路由名索引**
- 位置:`router/index.tsx:28`(`setRouteNameIndex(buildRouteNameIndex(allRoutes))` 顶层执行)+ `route-index.ts:3`(模块级可变全局)
- 建议:改为 `getRouteIndex()` 惰性单例,测试可 reset。

**【低】Rt-08 legacy `/agents` 重定向**
- 位置:`dashboard.tsx:44-52, 286-301`;grep 验证:源码内唯一引用是 `mobile-tab-bar.tsx:25` 的 `!pathname.startsWith("/agents")` 排除逻辑,无活跃 navigate 调用
- 结论:纯外部兜底(书签/旧机器输出),成本 15 行,可保留但应加弃用标记/埋点确认仍有人来,一年后删除。

---

## 四、app 布局层

**职责归属总评**:`RootLayout`(67 行)→ 认证守卫 + 会话加载;`DashboardLayout`(219 行)→ 应用壳;两个 detail-layout → tab 壳。层次是清楚的,但有以下问题:

**【高】L-01 DashboardLayout 承担 6 类互不相干的职责**
- 位置:`app/layouts/dashboard-layout.tsx:90-219`
- 现在一个 layout 里并存:侧栏折叠持久化(80-125)、presence 心跳挂载(97)、swipe-back 手势 + preview + 预载(100-140)、Web Push reconcile/suppressRoute/MessageEvent(147-179)、四种 lazy overlay gate(35-78)、移动 chrome 定位(188-207)。每加一个"全局副作用"都被塞进来。
- 建议:拆成组合式 hooks:`useSidebarCollapse()`、`useOverlays()`(现有 gate 已经是好模式,抽走即可)、`usePushBridge()`、`useSwipeBackShell()`;DashboardLayout 退化为纯布局。

**【中】L-02 detail-layout 用 `segments.indexOf(id)` 猜 activeTab**
- 位置:`agent-detail-layout.tsx:71-81`、`machine-detail-layout.tsx:58-64`
- 证据:`const afterId = segments[segments.indexOf(agentId ?? "") + 1];` —— 若 agentId 本身等于段名(如 id="chat"),或同 id 出现在路径别处,切 tab 判断错。用 `useMatches`/`useResolvedPath` 即可精确。
- 同时 `agent-detail-layout.tsx:85-95` `startChat()` 无 catch:`getOrCreateConversation`/`fetchChannels` 任一 reject → unhandled rejection,无错误 toast(FAB 恢复可用靠 finally,用户不知道失败原因)。

**【中】L-03 移动端适配"双轨制":JS hook + CSS 断点两处定义同一语义**
- 位置:`use-is-desktop.ts:9-11` 的 `"(min-width: 1024px), (display-mode: standalone) and (hover: hover) and (pointer: fine)"` vs `assets/css/tailwind.css:10-16` 的 `@custom-variant lg`(同样的复合查询)
- 影响:断点含义改动需要同步改 2 处;`useIsDesktop` 已被 21 个文件使用(lib 内另有 2 处),`lg:` 工具类散布在 38 个文件(components/pages/app)—— 语义判断共 60+ 个触点却有两个定义源,长期必然漂移(事实上 `lg:hidden`/`lg:pt-0` 在 dashboard-layout 一个文件里就用 6 处)。
- 建议:布局决策收敛为一个"shell 概念":布局层只允许用 `useAppShell() → "desktop" | "mobile"`,CSS 侧保留 `lg:` 但把两处的 query 字符串抽成常量并互指注释;feature 组件逐步用 shell 值替代散布判断。

**【低】L-04 两个 member-picker 同名不同物**
- 位置:`src/components/member-picker.tsx`(settings/machine 系 6 个文件用)vs `src/components/chat/member-picker.tsx`(chat 面板用,带测试)
- 建议:重命名其一(如 `chat/member-picker-panel.tsx`),或合并差异(大概率可合并——都是"搜索 + 多选")。

**【低】L-05 auth 路由布局微整理**
- 位置:`routes/auth.tsx:7,40,51,62` —— 4 条散根路由各自重复 `element: <SplashLayout />`,可合并为一个父路由包裹。低优先级。

---

## 五、构建与工程债

**【高】E-01 `tailwind.config.js` 是 Tailwind v3 时代遗物,半数配置在 v4 中为死配置**
- 位置:`tailwind.config.js:9-10`(content 配置,v4 自动扫描,无效)、`11-29`(safelist —— 验证:raw 色类全库仅 2 处且不在 safelist 列表内,safelist 的 gray/blue/yellow/red/indigo 系列全部为死字符)、`113-124`(`darkMode/variants/mode:"jit"` v4 皆无效)
- 证据:`assets/css/tailwind.css:4` `@config "../../../tailwind.config.js"` —— 只有 `theme.extend` 的 colors/spacing/screens/animation 真正生效(v4 通过 @config 合并);文件头注释 "Colors for dark theme / Only used by Web Terminal now"。grep 验证 raw 色仅 `search-result-list.tsx:84`(bg-yellow-200)与 `agent-status-bar.tsx:28`(text-blue-400)2 处——违反自家 AGENTS 语义 token 规则。
- 建议:把 colors/spacing/screens/animation 迁入 `tailwind.css @theme`,删除 config 文件与 `@config`;顺手清除 2 处 raw 色。**这是重构者第一眼看了会困惑的最大配置债。**

**【中】E-02 tsc 盲区:`sw/`、`vitest.config.ts` 都不在任何 ts project**
- 位置:`tsconfig.json:25`(`"include": ["src"]`)、`tsconfig.node.json:17`(`include: ["vite.config.ts"]`)
- 后果:`sw/sw.ts`(170 行手写 Workbox/推送代码,含 `declare const self` 等需要类型护航的部分)**只能靠构建时 esbuild 转译发现问题**,`pnpm type-check` 完全不碰它;vitest.config.ts 同理。且 tsconfig.json 无 project references,`build` 脚本的 `tsc -b` 等价于单包构建。
- 建议:`tsconfig.node.json` include 加 `vitest.config.ts`;`sw/sw.ts` 单独 swconfig(lib 加 `"WebWorker"`)或使用 project references。`scripts/*.mjs(deliberately JS)可维持现状但建议加 eslint/biome 覆盖说明。

**【中】E-03 biome.json 配置漂移 + React 规则缺位**
- 位置:`biome.json:203-212`(overrides 指向 `src/shims-3rd-party.d.ts` 与 `vite-plugin-export-csp-hashes.ts`,两文件均已不存在;实际遗留的是 `src/shims-css.d.ts` 未列入)
- 另:`biome.json:2` schema 版本 2.5.0 高于 devDependency `@biomejs/biome ^2.4.14`;`linter.preset: "none"` 显式 opt-in 30+ 规则但**漏掉了 `useExhaustiveDependencies`/hooks 系列正确性规则**(与 R-06 呼应;`avatar-cache.ts:187`、`use-auto-scroll.ts:17`、`thread-panel.tsx:1186` 等依赖数组 hack 全部无守卫)。
- 建议:schema 与版本对齐;overrides 清单巡检;开启 biome 的 hooks 系列正确性规则(需要先修复存量)。

**【中】E-04 PWA 更新策略的已知坑(质量总评良好,两处易忽视的风险)**
- 位置:`sw/sw.ts:31-57`(precache 顺序注释正确,network-first navigation)+ `pwa.ts:36-55`
- 已规避的坑(值得肯定):路由注册顺序防 "/" 被 precache 钉死;`hadController` 防首次 claim 误 reload;`visibilitychange → reg.update()` 收敛长命 tab;dev 模式禁 SW(pwa.ts:14)。
- **坑 1(中)**:`pwa.ts:38-44` controllerchange → `window.location.reload()` 无条件执行——SW 静默升级触发时用户可能正在输入草稿/上传中,reload 丢内容且无提示。建议:编辑态检测 + 延后到 idle/下一次 visibilitychange,或加"新版本可用"横幅由用户确认。
- **坑 2(低)**:`sw/sw.ts:62,77-80` `suppressedRoute` 是单值——双 tab 各看一个会话时,后 postMessage 的那个 wins;`isViewingRoute` 的 URL 比对(sw/sw.ts:125-135)部分缓解,但 chat 不改 URL 的场景恰好是要害。建议:per-tab topic 协商或 matchAll 后集中判定。
- **坑 3(低)**:push payload 解析失败静默 return(sw.ts:84-89)——加 debug 日志可帮助线上排查。

**【低】E-05 dev 配置含生产域名硬编码**
- 位置:`vite.config.ts:95` `allowedHosts:["localhost","laeliapage.metaxisdata.com"]`
- 建议:域名进 env,配置文件不留生产指纹。

**【低】E-06 tsconfig 规范度尚可,个别项缺位**
- `tsconfig.json`:有 `esModuleInterop: true`、`resolveJsonModule`、`moduleResolution: bundler`、`isolatedModules`、`forceConsistentCasingInFileNames` 等关键项 ✔;可补 `verbatimModuleSyntax`(激进可选)。
- `shims-css.d.ts`(4 行)手写 `declare module "*.css"` 在 Vite 8 下应删除(vite/client 类型已含 CSS 侧声明);且该文件未列入 biome overrides(见 E-03)。

**【低】E-07 vitest 覆盖范围错位**
- 位置:`vitest.config.ts:16-24`(coverage include 只有 stores/pages/auth-redirect)
- lib/ 33 文件里恰好是**最高风险**的 hooks(platform-edge-swipe/use-swipe-back/use-history-sentinel 有测试;web-push/pwa/image-blob-cache/file-upload/i18n/oauth 无)不在 coverage 统计视野。`src/test/setup.ts` 的 matchMedia/scrollIntoView polyfill 与 use-is-desktop 的 `getServerSnapshot` 配对良好。

**【低】E-08 package.json 脚本冗余**
- `"lint"` 与 `"biome:lint"` 完全同串(package.json:13,15);删 dup。

**【低】E-09 index.html `user-scalable=no, maximum-scale=1.0`**
- 位置:`index.html:7` —— a11y 反模式(iOS 现已忽略,但桌面 PWA/安卓 webview 仍生效禁止缩放)。PWA 里禁缩放疑为手势防冲突,建议在手势区用 `touch-action` 局部解决后取消禁缩放。

**【低】E-10 frontend/AGENTS.md 文档路径漂移**
- `frontend/AGENTS.md` 写 `import { cn() } from "@/react/lib/utils"` —— 实际路径是 `@/lib/utils`(utils.ts:1-6;68 个文件引用)。按文档写 import 会失败。同类:`biome.json` overrides 的两个幽灵文件(E-03)。

---

## 六、潜在 bug 与脆弱点(竞态/泄漏/手势栈交互)

**【高】B-01 `suppressLoadingFlags` 全局 store 冻结是整个手势栈最脆弱的一环**
- 位置:`src/stores/index.ts:37-56`(set 包装器)、`src/lib/use-swipe-back.ts:192`(`setSuppressLoadingFlags(true)`)、`use-swipe-back.ts:135,330`(500ms 延迟解除)
- 问题:preview 挂载 + 其后 500ms 内,**所有** `useAppStore.set` 调用都被静默丢弃——包括后台轮询(presence 心跳 30s 的 `fetchAgents`/`syncPresence`)、chat 流增量、push toast。异常路径(手势中途 touchcancel、双指先后触达、pendingReset 1000ms 兜底与 500ms 解冻竞态、组件卸载时未 touchend)都会延长或不确定地交错冻结窗口。被丢弃的 store 更新没有补偿机制,只能等下一轮轮询/重连兜底。
- 证据:store 注释自己承认这是 "no-op: store is frozen"(index.ts:54);而 `fetchAgents` 已支持 `opts.silent`(stores/agent.ts:37-59,`use-presence-heartbeat.ts:47` 正是这么调的)——**项目内已有不冻结 store 的正确模式**。
- 重构:废弃全局冻结;给 preview 会跑的 fetch 统一接 silent 参数(或在 preview scope 中把 set 重定向到 shadow store),`use-preview-routes.tsx` 已能拿到 scope 边界。

**【中】B-02 `invalidate` 后 in-flight 写回竞态(avatar 与 image 缓存同型)**
- avatar:`src/lib/avatar-cache.ts:73-96` —— fetch 完成虽有 `inflight.delete` + 写 `blobUrls`,但若中途 `invalidateAvatar(name)`(上传新头像后触发)已清掉 entry,旧 promise 完成时仍 `blobUrls.set(name, oldUrl)`,把旧头像写回 → 头像回跳,配合 B-03 是"上传新头像后显示旧头像"的可见 bug。
- image-cache:`image-blob-cache.ts:30-47` + `invalidateImageBlobs:51-54`(logout 调用,stores/auth.ts:79)——logout 时 in-flight 下载完成后 `cacheImageBlob` 把**前一个 principal 的图片字节**重新写回刚清空的缓存(跨会话数据残留,安全语义被打破)。
- 建议:promise 持有"世代号",完成时校验世代一致才落缓存;或在缓存工具(R-03 抽象)内建 per-entry cancel。

**【中】B-03 `useAvatar` 切换 name 时不清 stale URL**
- 位置:`avatar-cache.ts:164-188` —— effect 里 `!name` 分支 setUrl(null),但 `name` A→B 且 B 未缓存时不重置,继续显示 A 的头像直到 B 的 fetch resolve(快速切换 DM 对象时可见串头像)。
- 建议:effect 开头对未命中缓存的名字先 `setUrl(null)`(或改用 `useSyncExternalStore` 直接映射缓存态)。

**【中】B-04 route 级手势 commit 用 `replace: true` 改写历史**
- 位置:`use-swipe-back.ts:248`(`navigate(target, { replace: true })`)
- 后果:从 B 页(route 层手势 back 到 A)返回后,B 的历史 entry 被 A **替换**,浏览器物理 back 不再有 B;和 `useHistorySentinel` 的 push 模型(overlay push 一个 entry)不一致,用户从浏览器返回的行为在手势返回后发生不可见变化。若意图是防回环,应注释说明;若非意图,改 `navigate(target)`(pendingReset 逻辑不变)。

**【低】B-05 `use-swipe-back` 的解冻 timer 不入数组,依赖 500ms 魔数兜底**
- 位置:`use-swipe-back.ts:135`、`:330` —— `window.setTimeout(() => setSuppressLoadingFlags(false), 500)` 未 push 进 `timers`,cleanup 不清;幂等无害但把"500ms 内 fetch 完成"设为暗约定。合并进 B-01 修复。

**【低】B-06 window 级 4 个 touch 监听全页常驻**
- 位置:`use-swipe-back.ts:301-304`、`use-swipe-to-close-sheet.ts:184-187` —— touchstart(passive)每个触摸都走 `target.closest(...)`;DashboardLayout 挂载即生效。量级可接受,但两套手势 hook 都在 window 上,叠放(mention sheet 在 thread 里)时依赖 `[data-bb-layer-family]` 排除(use-swipe-back.ts:141)——layer 家族之外的自绘浮层不在防护内。建议:手势起点做 `closest("[data-swipe-block]")` 通用标记。

**【低】B-07 `watchForUpdates` 的 visibilitychange 监听不清理**
- 位置:`pwa.ts:50-55` —— `document.addEventListener("visibilitychange", …)` 注册一次后永不移除。因页面生命周期内 registerServiceWorker 只跑一次,实际无泄漏放大;列出以保完整,重构注册生命周期时顺手修。

**【低】B-08 资源名/工具函数散点重复**
- `slugify` 在 `markdown-file.ts:30` 与 `settings-roles.tsx:56` 各写一份;`avatarNameForUserId/AgentId`(avatar-cache.ts:52-60)与 `agentResourceName`/`commandResourceName`(command-status.ts:121-126)是同族 AIP 资源名构造,分属两处。统一进 `lib/resource.ts`(响应 R-02)。

**【低】B-09 avatar 缓存无上限**
- 位置:`avatar-cache.ts:23`(`blobUrls` Map 永久增长;对比 `image-blob-cache.ts:10` 有 MAX_CACHED_IMAGES=100 FIFO)。在有几百成员的 workspace,长驻 PWA 会累积等量 object URL。建议:接入 R-03 的带容量缓存;logout 全清已具备(stores/auth.ts:78)。

---

## 七、死代码与历史债务(全部 grep 验证)

| 项 | 位置 | 证据 | 处置建议 |
|---|---|---|---|
| **死导出** `formatToken` | agent-token.ts:4 | lib 外 0 引用(唯一 getManagerURL 被 machine-token 用) | 删除;文件并入 machine-token(见 R-01)|
| **死导出** `isHtmlPreviewable` | html-file.ts:22 | 0 引用(大小判断直接用 MAX_HTML_PREVIEW_BYTES,见 stores/preview.ts:27)| 删除或让 preview.ts/message-row 改用它统一口径 |
| **死导出** `isMarkdownPreviewable` | markdown-file.ts:21 | 0 引用 | 同上,与 html 端对齐 |
| **死导出** `reminderStatusShort` | reminder-status.ts:37 | 0 引用(姊妹 `taskStatusShort` 是活的,tasks-panel.tsx:7)| 删除 |
| 过度导出 `fetchAvatarUrl/getCachedAvatarUrl/isAvatarKnownMissing/permissionDeniedInfo/escapeFilterQuery/buildOAuthAuthorizeUrl` | 各文件 | 只有 lib 内部消费 | 收敛 export |
| **Tailwind v3 遗留配置** | tailwind.config.js 全文 | v4 + @config 只用 theme.extend(content/safelist/variants/darkMode 死)| 迁 @theme 后删除(E-01)|
| biome overrides 幽灵文件 | biome.json:205-211 | `src/shims-3rd-party.d.ts`、`vite-plugin-export-csp-hashes.ts` 不存在 | 清理 |
| eslint-disable no-op 注释 ×4 | thread-panel.tsx:1186、avatar.tsx:43、avatar-cache.ts:187、use-auto-scroll.ts:17 | linter 是 Biome | 改 biome-ignore 或删(配合开启 hooks 规则)|
| **重复组件** member-picker ×2 | components/member-picker.tsx vs components/chat/member-picker.tsx | settings/machine 6 文件 vs chat 面板 | 重命名或合并(L-04)|
| **重复 slugify** | settings-roles.tsx:56 vs markdown-file.ts:30 | 同名不同实现 | 统一 resource 工具 |
| raw 颜色 ×2 | search-result-list.tsx:84、agent-status-bar.tsx:28 | 违反自家语义 token 规则 | 换 token |
| `composables/` 目录 + 命名混用 | composables/*(4 文件)| lib/ 也含 hooks | 合并 hooks 目录(R-04)|
| 脚本重复 | package.json:13/15 同为 "biome lint src" | — | 删 dup |
| legacy `/agents` 兼容路由 | routes/dashboard.tsx:44-52, 286-301 | 无内部跳转点(仅 mobile-tab-bar.tsx:25 的 startsWith 排除)| 保留 + 弃用标记(Rt-08)|
| AGENTS 文档漂移 `@/react/lib/utils` | frontend/AGENTS.md | 实际 `@/lib/utils` | 修文档 |

**无废弃 styles 目录**:唯一样式文件为 `assets/css/tailwind.css`(581 行,@theme 语义 token 集中,质量好);`src/types/` 下除 proto-es 生成物只有根级 `vite-env.d.ts`/`shims-css.d.ts` 两个声明文件——结构干净。

---

## 八、重新设计视角:理想的基础设施结构

保留现有资产(注释、测试、iOS 语义知识)基础上,重新划分如下:

```
src/
  app/
    root.tsx / root-layout.tsx        # 仅会话装配(现状保留)
    layouts/                          # Dashboard/Splash/Auth 3 个壳
    shells/                           # 新: useAppShell() + DesktopShell/MobileShell
                                      # —— 断点语义唯一来源,消灭 21 文件 useIsDesktop 散布
  platform/                           # 新:环境与宿主能力("平台适配"层)
    sw.ts(现 sw/sw.ts)               # 注册生命周期 + 更新策略(用户可确认后 reload)
    push.ts                           # 现 web-push,不 import stores(回调注入)
    edge-swipe.ts                     # platform-edge-swipe 原样迁移
  i18n/                               # 现 lib/i18n + locales
  router/
    routes.ts                         # 唯一路由树,handle{name, permission?} 与
                                      # handles/route-info 由此派生(单一真相)
    guard.ts                          # 登录守卫 + 权限守卫(handle.permission)
    route.ts                          # type-safe RouteName/params + generatePath 适配层
  connect/
    transport.ts                      # createConnectTransport + fetch(credentials)
    interceptors/                     # unauth-redirect / error-taxonomy(错误码→UI 映射唯一处)
    clients.ts                        # 服务注册表批量建 client
    errors.ts                         # ConnectError→{i18n key, recoverable, dedupe} —— 146 处
                                      # toast 的收敛目标
  lib/                                # 纯函数,禁 import stores/components(R-07 铁律)
    format/   time.ts token.ts resource.ts
    net/      upload.ts download.ts
    cache/    async-memo.ts             # R-03 的统一抽象(avatar/image/preview 三合一)
  hooks/      use-edge-drag-to-close.ts  (use-swipe-back thread 模式 + use-swipe-to-close-sheet 合体)
              use-history-sentinel.ts / use-auto-scroll.ts / use-is-desktop.ts(→ shells 内部)
  stores/  (维持 slice 结构;去掉 index.ts 的 set 冻结,B-01 改为 silent-fetch 约定)
```

关键决策:
1. **手势/overlay 体系的收益重估**:三层快照 preview + 哨兵 + store 冻结 + UNSAFE_RouteContext 的组合维护成本(5 文件、~900 行、2 个 store hack)是本仓库复杂度之最。若产品允许"iOS 上交给系统边缘手势(现状已在 iOS 委让)+ 安卓用简化 push-pop",可删除 preview 克隆与 store 冻结,把 usePreviewRoutes/UNSAFE_RouteContext 整个退役。
2. **错误处理单点化**:`connect/interceptors/error-taxonomy` 是本次重构中"犹豫成本最低、收益最快"的一块。
3. **platform/ 层成立后**,iOS/PWA/SW/Web-Push 这些"非常识性知识"有独立归档住所,lib 归位为纯工具。

---

## 九、本模块重构优先级清单(按性价比排序)

| # | 事项 | 严重度 | 理由(收益/风险/工作量) | 关键位置 |
|---|---|---|---|---|
| **P0** | 废除 `suppressLoadingFlags` 全局 store 冻结,统一改 `silent` fetch 约定(项目内已有先例) | 高 | 消灭最大竞态源,方案已在库内存在;中工作量、低风险 | stores/index.ts:37-56、use-swipe-back.ts:135,192 |
| **P0** | 错误映射单点化:`connect/errors.ts` 建立 错误码→i18n/重试/dedupe,`describeError` i18n 化并迁入 | 高 | 新增 feature 时的最高复用点;146 个调用点可渐进迁移 | connect-errors.ts、146 处 toastManager.add |
| **P1** | 修复 avatar/image 缓存三竞态:invalidate 后写回、useAvatar stale URL、加容量上限 | 中 | 用户可见 bug(串头像/跨会话残留)+ 顺带引入统一 cache 工具 | avatar-cache.ts:73-96,164-188;image-blob-cache.ts:30-47 |
| **P1** | lib 目录整形:agent-token 并入 machine-token、command-status 拆 format/resource、hooks 合并到单一目录、toast 去 Base UI 私有接口依赖 | 中 | 纯机械重构;为"真正的 lib"立规(禁 import stores) | agent-token/command-status/toast/composables |
| **P1** | 路由名三合一:handles + dashboard.handle + ROUTE_INFO 用 `satisfies` 缝合,backTo 改名常量;handle 加 permission 权限守卫 | 中 | 防以后每次加页面的双份维护;低风险 | handles.ts、route-info.ts、routes/dashboard.tsx |
| **P1** | 开启 Biome React 正确性规则(useExhaustiveDependencies 等),同步清理 4 处 no-op eslint-disable | 中 | 当前 deps 数组类 bug 零防护;一次性修复量可控 | biome.json:24-67 |
| **P2** | 手势收敛:`useEdgeDragToClose` 合并 use-swipe-back(thread 模式)与 use-swipe-to-close-sheet;决策是否退役 preview 克隆(UNSAFE_RouteContext) | 中 | -900 行复杂度;可与 P0 联动 | use-swipe-back.ts、use-swipe-to-close-sheet.ts、use-preview-routes.tsx |
| **P2** | 布局适配单点:useAppShell(desktop/mobile 壳),收敛 21 文件 useIsDesktop + 2 处断点定义 | 中 | 断点语义漂移防患;渐进可行 | use-is-desktop.ts、tailwind.css:10-16 |
| **P2** | 工程清障:tailwind.config.js 迁 @theme 删除、tsconfig 补 sw/vitest 覆盖、biome.json 幽灵条目+schema 对齐、allowedHosts 进 env | 高(配置债)| 一次 PR 解决,后续自文档化 | tailwind.config.js、sw/sw.ts、biome.json:203-212、vite.config.ts:95 |
| **P3** | 死代码清扫:4 个死导出、双 member-picker、双 slugify、2 处 raw 色、package.json 脚本 dup、`user-scalable=no` | 低 | 纯删除;随手可做 | 见第七章表格 |
| **P3** | PWA reload 加用户可见性护栏(编辑态延后)+ push suppressedRoute 多 tab 语义 | 中 | 低频但伤信任;改动小 | pwa.ts:38-44、sw/sw.ts:62-80 |

---

### 附录:审查方法说明

- 引用度统计:对全部 33 个 lib 文件逐一 `grep -rE "from \"@/lib/…|from \"\./…"` 计数,并对每个导出符号单独 grep 全库(排除 lib/ 自身与测试);死导出结论均经两轮正向/反向验证。
- 手势栈知识(platform-edge-swipe 注释、382 行 sentinel/swipe 测试)经全文研读确认,iOS 特殊处理**未过时**,WebKit bug 240892/136531 仍为活跃限制。
- 配置一致性:`tsconfig include`、`biome.json overrides`、`tailwind @config`、`vite manualChunks` 均以文件系统实际状态交叉核对。