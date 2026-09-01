# 前端架构评审报告 09:auth 页面分析与横切扫描

> **⚙ 实施进度标注(批 15 收口后)**
- ✅ 已完成(本批):A-6 redirect protocol-relative 校验——`sanitizeRedirect()` 单点落 `router/auth-redirect.ts`,signin 参数消费、oauth state 存储(startOAuthLogin)与回调消费三面统一,`//evil.com`/外站 URL/空值一律回落 `/`(`e368389`);A-5 auth 域 6 处直连 RPC 清零——公共配置读收敛 `hooks/use-workspace-policy.ts`(signin/signup/machines 三消费端共享一个 `["workspace-policy"]` 缓存)与 `hooks/use-identity-providers.ts`(signin/oauth-login 共享),A-8 的 `endsWith` 过宽匹配同步收紧为资源名精确匹配(`9ecb3cd`);横切防抖用户搜索双份拷贝合一 `hooks/use-user-search.ts`(member-picker/from-sender-picker,含生成号守卫修复两份拷贝共有的过期回写竞态,`4fdb72a`);getChannel 三胞胎(chat-conversation 兜底读/channel-detail 真相源/activity-detail 兜底读)收敛 `hooks/use-channel.ts`(`9b56444`);agent-teams 目录读三消费端(thread 分配下拉/管理卡片/TeamDetailPage)收敛 `hooks/use-agent-teams.ts`,TeamDetailPage 手写 load 退役 + agents 归共享花名册(`20195fe`);auth slice 15 用例、permission 目录一致性、search-result-list 4 用例(`cc377b4`)。
- ⏳ 未完成:AuthShell(A-4,布局复用);loginWithIdp/register 参数更名(A-9);i18n camelCase 段归一(P3);§2.2 直连 RPC 余量 72 处中,settings 域 ~40 处为四原语合法形态(queryFn 读 + useCrudDialog 写回调),真余量是 machine-profile IAM、user-menu、setup-checklist 等页面级特化点(低收益)。

> **⚙ 实施进度标注(批 3 收口后)**
- ✅ 已完成:device-login 轮询治理(终态停轮、退避、后台暂停、清理)+ 4 用例测试(`7eedafb`);auth 五页错误出口统一(389ce97);auth.oauth-callback 等 3 个描述性 fallback 键删除;stale-data 提示已加;auth 域 6 处直连 RPC 未变(见下)。
- ⏳ 未完成:auth 域直连 RPC 收敛(useWorkspacePolicy 等,归页面拆分);AuthShell;loginWithIdp/register 参数更名;redirect protocol-relative 校验;oauth 两页测试;settings 域 60 处直连 RPC 收敛(归页面拆分);i18n camelCase 段归一与 12~15 个未翻译键。

> 评审对象:`frontend/`(React 19 + Zustand 5 + ConnectRPC(proto-es)+ Tailwind 4 + Biome)。
> 范围 A:`src/pages/auth/` 全部 6 个页面(1210 行:signup 417、device-login 285、signin 211、verify-email 137、oauth-login 87、oauth-callback 73)逐行精读,并延伸核对 `stores/auth.ts`、`lib/oauth.ts`、`lib/connect-errors.ts`、`connect/index.ts`、`router/auth-redirect.ts`、`router/routes/auth.tsx`。
> 范围 B:脚本化统计(src 下 222 个生产文件,排除 `*.test.*`、`src/test/`、`types/proto-es/`、`locales/`,共 48,214 行)+ 人工抽查。
> 严重度:高 / 中 / 低。

---

## 一、auth 页面分析

### 1.1 总体判断

6 个页面整体质量**高于全仓库平均**:全部文案走 i18n(含动态 key 且已在 `check-react-i18n.mjs` 的 `DYNAMIC_PREFIXES` 注册)、表单控件均有 label/htmlFor 关联与 `autoComplete`、会话为 HttpOnly cookie、OAuth state 一次性消费且 10 分钟 TTL、store 耦合方向正确(页面只取动作不感知 RPC 细节)。主要问题集中在:**device-login 轮询健壮性、auth 域 6 处组件直连 RPC 绕过 stores/auth、5 处重复的 "Laelia" 头部无共享组件、错误呈现四种形态并存、device-login 与 oauth 两页零测试**。

---

### 1.2 发现清单

#### A-1【高】device-login 轮询缺少终止兜底与退避,终态后仍在无限轮询

- **位置**:`src/pages/auth/device-login.tsx:68-73`(interval 3000ms)、`:47-64`(poll 实现)。
- **严重度**:高
- **描述与证据**:轮询 effect 的唯一终止条件是 `!userCode || approved`(L69)。`setInterval(() => void poll(), 3000)` 无退避、无连续失败上限、无总时长上限:
  1. status 变为 `EXPIRED`(L154 分支)或 `DENIED`(L163 分支)后,`setInterval` 仍在每 3 秒调用 `getDeviceLoginStatus`——终态后纯属浪费带宽;
  2. 无 `visibilitychange` 处理,后台 tab 永久轮询;
  3. `pollFailed` 无连续失败计数:一次失败即 `setPollFailed(true)`,一次成功即清除(L60/L62),UI 在 unreachable(L172)与正常态之间抖动;
  4. 服务器端 user_code 过期后,前端只能靠 status=EXPIRED 停在提示页,轮询却不随之停止。
- **建议**:
  1. status 进入 `EXPIRED`/`DENIED` 时 `clearInterval` 终止轮询;
  2. 连续失败 ≥3 次才显示 unreachable,成功即复位(现在是一次失败立刻置位、一次成功立刻清除,网络抖动时 UI 抖动);
  3. `document.visibilityState === "hidden"` 时暂停;
  4. 若希望保留"过期后设备重新发起"的能力,可改为指数退避(3s→6s→12s,上限 15s)+ 总时长上限。
  - 参考实现:仓库已有 `stores/polling.ts` 轮询封装,可将该页面接入同一套节流机制。

#### A-2【中】device-login 网络中断期间 UI 静默陈旧,`pollFailed` 语义过窄

- **位置**:`device-login.tsx:172`(渲染分支)、`:61-63`(catch)
- **严重度**:中
- **描述与证据**:`pollFailed` 只在 `status === UNSPECIFIED`(从未成功拉到数据)时才渲染 unreachable 提示;若设备信息已渲染成功,之后的网络抖动完全无提示——用户看到的是"静默不再更新的数据",且一次成功就清除告警。`denialReason` 直接渲染服务端字符串(L169,React 文本转义,无 XSS 面)。
- **建议**:在已渲染的设备卡片上增加"连接中断"条幅(保留最后一次成功数据,标注数据时间),连续 N 次失败后才降级为 unreachable 全量提示。

#### A-3【中】错误呈现存在四种并存形态

- **位置与证据**:
  1. toast + `err instanceof Error ? err.message : t(fallback)`——`signin.tsx:72-84`、`signup.tsx:139-150`;
  2. toast + `String(err)` 回退——`signup.tsx:161-166`、`verify-email.tsx:53-58`(resend 路径);
  3. 页面内 error 文案(无 toast)——`oauth-callback.tsx:41-48`、`oauth-login.tsx:59-62`,且 oauth-login 是全 auth 域唯一的 `console.error`(L60);
  4. 内联错误文本 + `describeError`——`device-login.tsx:85`。
- **描述**:同一类"auth 动作失败"有 4 种呈现。`err.message` 原样展示意味着 ConnectError 的英文原始信息(如 `permission denied`、`unauthenticated`)直接面对用户;`describeError` 输出的 `missing laelia.devices.approve on machines/xxx` 对终端用户可读性同样差。
- **建议**:抽 `showErrorToast(err, fallbackKey)`:内部统一 `describeError` + 针对 `permission_denied` / `unauthenticated` / `not_found` 等常见 ConnectError code 做 i18n 映射,auth 5 页与后续页面共用;oauth-login 的 `console.error` 改为统一 logger 或 toast。

#### A-4【中】布局复用不足:"Laelia" 头部复制 5 处、同构容器 8 处

- **位置与证据**:
  - `<h1 className="text-2xl font-semibold text-main">Laelia</h1>` + 副标题块共 5 处:`signin.tsx:88-93`、`signup.tsx:175-180` 与 `:216-221`(registered 分支重复一次)、`verify-email.tsx:66-71`、`device-login.tsx:141-146`;
  - `<div className="flex w-full max-w-sm flex-col gap-y-6">` 容器在 auth 目录 8 处(signin.tsx:87、signup.tsx:174/215、verify-email.tsx:65、oauth-callback.tsx:54/67、oauth-login.tsx:68/81);
  - "去登录"文本链 `navigate("/auth/signin")` 用 `<button className="text-accent hover:underline">` 实现至少 3 处(signup.tsx:202-208、212-208、signin.tsx:196-207、verify-email.tsx:125-131),未用 `Link`;
  - 路由层已有 `SplashLayout`(shell 层,提供 LocaleSwitch + footer,`routes/auth.tsx`),但**页面级卡片头部没有共享组件**,样式漂移已发生(device-login 独自使用 `max-w-md mt-20` 布局,其余页 max-w-sm)。
- **建议**:新建 `components/auth/auth-shell.tsx`(props:title/subtitle/wide?)统一品牌头与容器;文本链接统一为 `Link` + `AuthLink` 样式常量。
- **说明**:`Laelia` 品牌字面量属可接受例外;若要彻底 i18n 化可加到 locale,优先级低。

#### A-5【中】auth 页 6 处组件直连 RPC,绕过既有的 stores/auth.ts 封装

- **位置与证据**:`signup.tsx:79`(getWorkspaceInfo)、`signin.tsx:31`(getWorkspaceInfo)+ `signin.tsx:49`(listIdentityProviders)、`device-login.tsx:50`(getDeviceLoginStatus)+ `:80`(approveDeviceLogin)、`oauth-login.tsx:34`。
- **严重度**:中
- **描述与证据**:`stores/auth.ts` 已封装 login/logout/register/verifyEmail/resendVerificationEmail,方向正确;但"公共配置读取"类 RPC 全部散落在页面 useEffect 里(见第二部分分层统计,`frontend/src` 全库页面直连 60 处中 auth 占 6 处)。两个具体代价:
  1. `getWorkspaceInfo` 在 signin 与 signup 各自无缓存地请求一次,用户在登录/注册间来回跳转时重复 RPC;
  2. store 的 `register` 名下实际是 `userServiceClient.createUser`(auth.ts:84),页面还需要自行解读 `requireVerification` 决定后续登录流(signup.tsx:130-138),策略状态散落在页面与 store 两处。
- **建议**:
  - `useWorkspacePolicy()` composable(或 `stores/workspacePolicy`)带模块级缓存,signin/signup 共享;
  - device 的 `getDeviceLoginStatus/approveDeviceLogin` 收进 store(便于测试 mock 一处 + 未来加轮询节流);
  - store 层面考虑把 "注册 + 是否需要验证" 的判定收成一个动作,让 signup 页面不再读两个状态字段拼逻辑。

#### A-6【总体良好→低风险项 4 个】token 与路由安全

- **已做对的部分(证据)**:
  - 会话是 HttpOnly cookie:`connect/index.ts:70-74` transport 统一 `credentials: "include"`;登录/登出/401 兜底均不触碰 localStorage 的凭据;全库 grep `localStorage|sessionStorage|document.cookie` 仅命中 UI 偏好、i18n 语言、OAuth state、折叠状态 4 类,无任何 token;
  - agent bootstrap token 展示有掩码 `formatToken`(`lib/agent-token.ts:3-9`);machine setup 命令不嵌 token,改走 device flow(`lib/machine-token.ts:20-27` 注释明确);
  - OAuth state:`lib/oauth.ts:14-21` 32 字节 `crypto.getRandomValues` + URL-safe base64;`storeOAuthState` 写入 `laelia_oauth_state_<token>`(L23-28);retrieve 时 10 分钟 TTL 校验(L36-39);callback 先 `retrieveOAuthState` 后立即 `clearOAuthState`(oauth-callback.tsx:23-24)一次性消费 ✅;`authUrl` 有 http/https 白名单(oauth.ts:54-61);redirect_uri 固定 `origin + /oauth/callback`(oauth.ts:97),未接受外部输入 ✅;
  - 401 全局兜底(`connect/index.ts:31-68`)带防抖闸(`authRedirecting`)且对公开 auth 页豁免,注释解释了与 loadSession 的竞态。
- **风险项**:
  - 低:`redirect` 参数未校验"必须以 `/` 开头"(`signin.tsx:62`、`router/auth-redirect.ts:61`、`lib/oauth.ts:94`、`oauth-callback.tsx:38-39`)。react-router 的 SPA `navigate/redirect` 不会离站,当前不构成可利用的开放重定向,但 `//evil.com` 这类 protocol-relative 值建议显式拒绝(一行校验),防止未来有人把 `navigate` 换成 `window.location.assign` 时埋雷;
  - 低:`lib/oauth.ts:23-28` state 孤儿清理缺位——用户放弃 OAuth 后 entry 永久留在 localStorage(TTL 只在 retrieve 时判),建议 `storeOAuthState` 时顺带清扫同前缀过期项;
  - 低:state 存 localStorage 而非 sessionStorage,可接受但 sessionStorage 更贴合"单标签一次性"语义;
  - 低:`device-login.tsx:90-96` `handleUseAnotherAccount` 的 `await logout()` 无 catch,logout 内部 RPC 失败会向外抛 unhandled rejection(调用点 `void handleUseAnotherAccount()` 吞掉);包 try/finally 再 navigate;
  - 低:`device-login.tsx:107-111` `handleClosePage` 的 500ms `setTimeout` 未在组件卸载时清理。

#### A-7【低】signup 表单交互细节

- `signup.tsx:111-121` 自动补名 effect:用户清空姓名后 `nameManuallyEdited` 复位 false,立即被自动重填,用户无法"清空后自己再填";首字母大写仅处理 email 前缀前两段,unicode/连字符不友好;建议仅在 name 为空时自动填或用 `useMemo` 派生。
- `signup.tsx:95-108`:checks/hasHint/mismatch/emailValid/allowSubmit 每次渲染重算——量小无碍,重构表单时一并派生即可。
- 低:`signin.tsx:188` / `signup.tsx:391` 提交中显示 `"…"` 文本,verify-email 已用 `Loader2` 组件——风格不一致,建议统一 `LoadingButton`。
- 低:signup PASSWORD_CHECKS 的 `key` 与 `label` 值完全相同(signup.tsx:28-43),类型可砍掉一字段。

#### A-8【低】oauth 流程细节

- `oauth-login.tsx:34-41`:进入即全量 `listIdentityProviders` 前端 find;匹配条件 `p.name === \`idps/${id}\` || p.name.endsWith(\`/${providerId}\`)` 的第二分支过宽(可命中 `idps/foo-bar` 的尾部),建议删除 endsWith 或后端提供按 idp 获取。
- `oauth-callback.tsx:34` 的 `login("", "", { idpName, code })` 复用语义怪(见下条 A-9)。
- `oauth-callback.tsx:23-24` state 一次性消费 ✅;但对"直接访问 /oauth/callback"等未命中场景无孤儿 state 清扫(低)。

#### A-9【低】stores/auth.ts 的 API 形态

- **位置与证据**:`stores/auth.ts:23-63` `login(email, password, idp?)` 一个方法承载两态:密码登录(必传 email/password)与 OAuth2 交换(传空字符串 + idp 三元组,`oauth-callback.tsx:34` 即 `login("", "", { idpName, code })`)。魔法空参数损害可读性。
- `stores/auth.ts:83` `register(email, title, password)` 的第二参数命名 `title`,与 UI 的 "name" 字段错位(易混淆)。
- 已验证 store 与全局 401 拦截器的协同是正确的:`connect/index.ts:31-68` 对公开 auth 路径(4 个前缀)豁免 401 重置,避免 loadSession 竞态;`stores/auth.ts:56-62` 登录后 refetch GetCurrentUser 补齐权限字段的动机有注释、失败回退保留了 login 响应的 user ✅。
- **建议**:拆 `loginWithIdp(idpName, code)`;`register` 参数更名 `name`;`handleUseAnotherAccount`/`logout` 失败路径 try/finally(见 A-6 末条)。

#### A-9【低】布局与 i18n 复用

- **位置与证据**:`Laelia` h1 + subtitle 头部块在 5 处重复(signin.tsx:88-93、signup.tsx:175-180 与 216-221、verify-email.tsx:66-71、device-login.tsx:141-146);`max-w-sm flex flex-col gap-y-6` 容器 8 处;app/layouts 只有 `dashboard-layout / machine-detail-layout / agent-detail-layout / splash-layout`,没有 auth 级布局。
- **建议**:见 A-4/优先级 #6,`AuthShell` 一并解决。

#### A-10【中】auth 页测试覆盖不均

- **位置与证据**:`signin.test.tsx`(196 行)、`signup.test.tsx`(227)、`verify-email.test.tsx`(120)为真实行为断言(RPC 载荷、store 状态、按钮禁用、toast、redirect 保留);但 **device-login(285 行,本目录逻辑最复杂:轮询/过期/拒绝/approve 重试)与 oauth-callback(73)/oauth-login(87) 零测试**。
- **建议**:为 device-login 补:轮询推进(mock interval + fake timers)、EXPIRED/DENIED 渲染、approve 失败重试、登出切换账号 redirect 保留;oauth-callback 补"无效 state / 过期 state / 成功登录跳 redirect"三例。

---

## 二、横切扫描分析

> 统计口径:src 下 222 个生产文件(排除 `*.test.*`、`src/test/`、`types/proto-es/`、`locales/`),共 48,214 行;统计脚本为一次性 node 脚本,运行后删除。

### 2.1 i18n 体系

**基础事实**:`src/locales/en-US.json` 70,441B / `zh-CN.json` 69,047B;node 脚本 flatten 后统计:

| 指标 | 结果 |
|---|---|
| en-US 叶子 key | **1324** |
| zh-CN 叶子 key | **1324** |
| 仅存在于 en / 仅存在于 zh | **0 / 0**(双向零差集,跨语言一致性由脚本强制) |
| en 值 == zh 值 | 62 个(其中约 12–15 个为真未翻译,其余为语言中立项) |
| 顶层命名空间 | `settings`=439(33%)、`agent`=149、`machine`=107、`command`=86、`auth`=72、`chat`=61、`user`=52、`members`=51、`reminders`=51、`channel`=46、`common`=42、`channelTask`(camel)=32、`activity`=21、`sidebar`=20、`tasks`=20、`globalSearch`/`channelFiles`/`teamPrompt` 等,共 23 个顶层、765 个二级前缀 |

- **未翻译值抽样(en==zh 且非占位符,约 12–15 个真实漏翻)**:
  - `tasks.final-summary` / `tasks.header-final-summary` = "Final Summary"
  - `settings.agentTeams.leader` = "Leader"
  - `members.section-agents` = "Agents" / `members.section-humans` = "Humans"
  - `channel.member-type-agent` = "Agent"、`chat.agent` = "Agent"、`chat.tool-error` = "Error"
  - `common.hash` / `machine.detail-hash` = "Hash"、`machine.detail-ip` = "IP"
  - `agent.acp-config-provider` = "Provider"、`settings.agentTeams.leader` = "Leader"
  - `settings.identity-providers.field-client-id` = "Client ID"、`field-scopes` = "Scopes"
  - 其余 ~45 个为占位符/专名(`you@example.com`、`https://example.com`、`Linux/macOS/Windows`、`{{n}}`、`sk-...`、`OAuth2`、`DeepSeek` 等),**合理保留,但应在脚本中加白名单避免每次人工复核**。
- en-US 中 2 个含汉字值是语言自名(`简体中文 (Simplified Chinese)`、`日本語`),合理。
- **命名规范**:末段 kebab-case 1058、plain 265、camel 1、snake 0——主导风格统一;但有 **6 个 camelCase 命名空间段混杂:`channelFiles`、`channelSearch`、`channelTask`、`globalSearch`、`agentTeams`、`teamPrompt`**(settings 下既有 `settings.api-providers` 又有 `settings.agentTeams`)。建议一次性改为 kebab 并同步 `DYNAMIC_PREFIXES`。
- **硬编码检查**:全部 tsx 含汉字行 **0**;JSX 原生英文句子(启发式)只命中代码注释,**无用户可见文案硬编码**;`useTranslation` 覆盖 100/149 个 tsx(其余为无文案展示组件,合理);仅 2 处字面量 `aria-label`(见 2.3)。

**frontend/scripts 约束**:

1. `check-react-i18n.mjs`(277 行)四重检查,任一失败 exit 1:
   - missing:t() 在代码出现但 locale 没有;静态可追踪的形式包括 `t("k")`、三元对 `t(cond ? "a" : "b")`、`titleKey|descriptionKey|messageKey|labelKey` 字面量;`_one/_other/…` 复数后缀折叠为 base key;
   - unused:locale 存在但代码未引用;豁免靠 `DYNAMIC_PREFIXES` 手工白名单(目前 15 组前缀,含 `auth.sign-up.password-`、`agent.lifecycle.`、`command.token-*` 等,每条注释了调用点);
   - 跨语言一致性:每个 locale 的 key 集合必须与 en-US 完全一致;
   - 值中出现未转义的单 `{name}` 占位(react-i18next 插值需要双花括号 `{{name}}`)。
   - **当前状态:红**——missing 1(`common.deleting`,`src/pages/dashboard/team-detail.tsx:443` 使用)+ unused 3(`settings.agentTeams.create-description`、`settings.agentTeams.create-failed`、`sidebar.settings-agent-teams`)。即 `pnpm --dir frontend check` 门禁当前失败,应立即修复恢复 CI。
2. `sort_i18n_keys.mjs`:递归深排序 + 2 空格缩进 + 尾随换行归一化 locale JSON;`--check` 模式退出码控制 CI;写失败时逐文件回滚。当前 2 个文件均 normalized(过)。
   - 另有第三个门禁 `check-react-layering.mjs`:TS AST 扫描 feature 代码的 raw global z-index / document.body portal(`src/components/ui/` 白名单);三者共同构成 `pnpm --dir frontend check`。

### 2.2 重复模式全局量化

hooks 统计(222 个生产 ts/tsx):

| Hook | 总数 | 全仓库平均/文件 | 有该 hook 的文件均摊 |
|---|---|---|---|
| `useState` | 547 | 2.46 | 5.5 |
| `useEffect` | 155 | 0.70 | 2.4 |
| `useCallback` | 102 | 0.46 | 1.6 |
| `useMemo` | 76 | 0.34 | 1.2 |

- `useEffect` 前 15(节选):`chat-conversation.tsx` 12、`global-search.tsx` 8、`agent-profile.tsx` 7、`machine-profile.tsx` 6、`html-preview-overlay.tsx` 5、`dashboard-layout.tsx` 4、`thread-panel.tsx` 4、`channel-members-panel.tsx` 3、`member-picker.tsx` 3、`activity-detail.tsx` 3、`agent-mcp.tsx` 3、`command-detail.tsx` 3、`human-detail.tsx` 3、`machines.tsx` 3。
- hook 密度警示(总调用量 ≥30):`machine-profile.tsx`(55 useState,共 62)、`agent-profile.tsx`(49/56)、`chat-conversation.tsx`(12 eff+25 cb,共 55)、`user-list.tsx`(27)、`global-search.tsx`(19 se+8 eff)。这三个 2000+ 行的页面是全仓库状态熵中心(与 2.4 测试缺口叠加)。
- 最大文件:`agent-profile.tsx` 2405 行、`machine-profile.tsx` 2200 行、`chat-conversation.tsx` 2166 行、`thread-panel.tsx` 1405 行、`global-search.tsx` 1011 行。
- `zDescribe` 类自定义封装:**0 命中**。

**分层统计(`*ServiceClient.` 直接调用点)**:

| 区域 | 调用点数 | 说明 |
|---|---|---|
| `pages/` 组件直连 | **60** | 违反"组件不直连 API"约定的点 |
| `components/` 等 | 5(+1 处裸 `fetch`,user-menu.tsx:85) | —— |
| `stores/` | 115 | 正面案例:chat/channel/task/machine/agent 等 18 个 slice 全部收口 |
| `lib/`(缓存/下载等基础层) | 12 | avatar-cache、file-download、web-push、image-blob-cache,属基础设施 |
| `composables/` | 0 | —— |

**组件直连 API 全量 file:line(60 + 5 处,按文件归类)**:

```
auth 页(6):device-login.tsx:50,80;oauth-login.tsx:34;signin.tsx:31,49;signup.tsx:79
global-search.tsx:277,649
human-detail.tsx:99,104,199,200
machine-profile.tsx:304,819
settings-api-providers.tsx:151,152,153,200,271,313,525
settings-audit.tsx:65,112
settings-groups.tsx:127,128,161,234,285,316
settings-iam.tsx:124,125,126,367,395
settings-identity-providers.tsx:128,203,239,271
settings-mcp-servers.tsx:191,192,201,202,203,204,254,289,324
settings-notifications.tsx:39,69,86
settings-profile.tsx:111,116,153
settings-roles.tsx:105,197,232,256
team-detail.tsx:87,88,93,213,228,269
components/agent/agent-teams-manager.tsx:45,46
components/chat/member-picker.tsx:103
components/setup-checklist-dialog.tsx:53
components/user-menu.tsx:50(+ 85 裸 fetch)
```

模式很清晰:**chat/task/thread 等核心域走 store(115 处、18 个 store 文件封装),而 settings-*(7 页)、human-detail、machine-profile 的 IAM 部分、global-search、team-detail 共约 60 处直接在组件函数体里 RPC**。代价:(1) 无缓存,`listUsers/listGroups` 等被多页重复拉取;(2) 53 个文件 import `@/connect`,38 个测试文件各自手写 `vi.mock("@/connect")` mock 块。建议:设置类页面收敛到 settings store 或 `useXxxQuery` composable 系列,并把 connect mock 收敛为一处共享工厂。

### 2.3 可访问性与安全

- **img/alt**:全部 `<img>` 均有 `alt`(含动态 `alt={attachment.name}`)✅。
- **键盘可访问性**:`remote-image.tsx:46-56`(thumb)与 `:93-105`(inline)`onClick + role="button" + tabIndex=0` 却无 `onKeyDown`(Enter/Space),键盘用户无法打开灯箱。全库 `role="button"` 6 处、`onKeyDown` 16 处,此 2 处漏网。
- **aria-label 字面量 2 处未走 i18n**:`src/components/chat/file-card.tsx:122`(`aria-label="download"`)、`src/components/preview/html-preview-overlay.tsx:471`(`aria-label="comment"`)——小写、非完整短语,读屏播报差。
- **target="_blank"**:**0 处静态写法**;`window.open` 2 处均第 3 参传 `"noopener,noreferrer"` ✅。
- **URL 处理**:见范围 A 第 A-6/A-8 节:auth 域 encodeURIComponent 已到位、oauth `authUrl` 有 scheme 白名单;`html-preview-overlay.tsx:219-220` 与 `workspace/html-file-view.tsx:37` 接收 iframe postMessage 的 href 直接 `window.open` 未校验 scheme(P1 项);`connect/index.ts:64` 与 `lib/oauth.ts:100` 的 location.assign 均安全。
- **凭据存储**:全库无 token 进 localStorage/sessionStorage/cookie 读取;会话 HttpOnly cookie(`connect/index.ts:72` `credentials: "include"`);agent token 展示有掩码;machine setup 命令不嵌 token(device flow)。

### 2.4 测试质量概览

- **规模**:95 个测试文件、17,969 行、155 describe / 611 it;**快照测试 0 个**。
- **结论:以行为断言为主,质量高于平均**。抽样证据:
  - `pages/auth/signin.test.tsx`:6 个 it 全部行为级——login RPC 载荷字段、store `isLoggedIn` 翻转、toast `objectContaining`、按钮禁用阈值、signup 链接随 policy 消失;
  - `stores/chat-stream.test.ts`:手写长轮询 promise resolver 精确控制 watcher 自调度时序,测的是重订阅/竞态语义;
  - `agent-profile.test.tsx`(852 行,全库最大测试):mock 边界在 store 动作层而非 fetch 层。
- **测试分布**:`pages/dashboard` 34、`components/chat` 15、`stores` 12(全部)、`lib` 11、`components/ui` 11、`router` 3、`pages/auth` 3、`components/command-events` 3、`connect` 1、`composables` 1、`app` 1。
- 38 个测试统一 `vi.hoisted + vi.mock("@/connect")` 样板,建议抽 `test/helpers/connect-mock.ts` 共享工厂。
- **缺口(≥150 行无同名测试)**:38 个,其中 `chat-conversation.tsx` 2166、`global-search.tsx` 1011、`stores/types.ts` 903(纯类型,可豁免)、`settings-identity-providers.tsx` 566、`sidebar.tsx` 515、`html-preview-overlay.tsx` 501、`team-detail.tsx` 451、`routes/dashboard.tsx` 435、`device-login.tsx` 285、oauth 两页等——见优先级 #7。

---

## 三、本范围重构优先级清单

| # | 优先级 | 事项 | 位置 | 预估收益 |
|---|---|---|---|---|
| 1 | **P0** | 修复 `check-react-i18n` 红灯:补 `common.deleting`,删除/挂回 3 个 unused key,恢复 CI 门禁 | `team-detail.tsx:443`、locales | 半小时内可完成,门禁已失效 |
| 2 | **P0** | device-login 轮询治理:终态(EXPIRED/DENIED)停轮询、失败退避+连续失败阈值、`visibilitychange` 暂停、卸载清理 setTimeout | `device-login.tsx:68-73,107-111` | 公网页面,资耗与体验直接相关 |
| 3 | **P1** | `window.open` 前 scheme 白名单(仅 http/https),抽 `safeOpenExternal(href)` | `html-preview-overlay.tsx:220`、`html-file-view.tsx:37` | 1 个 helper + 2 处替换 |
| 4 | **P1** | auth 域 6 处直连 RPC 收敛:`useWorkspacePolicy()`(模块级缓存,signin/signup 共用)、IdP 列表进 composable;device 2 个 RPC 移入 store | `signin.tsx:29-60`、`signup.tsx:77-93`、`oauth-login.tsx:34`、`device-login.tsx:47-88` | 与 store 层风格对齐,消除重复请求 |
| 5 | **P1** | 错误呈现统一:抽 `showErrorToast(err, fallbackKey)`(describeError + 常见 ConnectError code→i18n 映射),auth 5 页共用 | auth 5 页 | 消除 4 种并存形态 |
| 6 | **P1** | 新建 `AuthShell`(品牌头+副标题+容器变体)替换 5 处重复头部、8 处同构容器;`LoadingButton` 替换 `signin.tsx:188`/`signup.tsx:391` 的 `"…"` | auth 4 页 | 布局漂移已发生(device-login 独自 max-w-md) |
| 7 | **P2** | `login("","",{idp})` → `loginWithIdp`;`register(title)` 参数更名 `name`;补 device-login / oauth-callback / oauth-login 三页测试(优先 device-login 轮询状态机) | `stores/auth.ts:23-98`、auth 测试 | 模式可复制 signin.test.tsx |
| 8 | **P2** | redirect 参数校验 `startsWith("/") && !startsWith("//")`,三处入口统一 `sanitizeRedirect()` | `signin.tsx:62`、`auth-redirect.ts:61`、`lib/oauth.ts` | 加固项 |
| 9 | **P3** | i18n 命名归一:6 个 camelCase 段(`channelFiles/channelSearch/channelTask/globalSearch/agentTeams/teamPrompt`)→ kebab;清理 12–15 个真正未翻译键(`tasks.final-summary`、`Leader`、`Provider`、`Client ID` 等) | locales + 引用点 | 一次性脚本 + `node scripts/sort_i18n_keys.mjs` 提交 |
| 10 | **P3** | 后续重构(超出本范围):`agent-profile`(2405 行/49 useState)、`machine-profile`(2200/55)、`chat-conversation`(2166)拆分;38 个无测试大文件按 2.4 清单补测 | 见 2.4 | 长期 |

---

### 附:统计口径备注

- hooks 统计含 `src/components/ui/` 基础组件;`src/test/setup.ts` 未计入测试文件(95 = 96 - setup)。
- en==zh 62 个 key 中,"占位符/专名"(you@example.com、Linux/macOS/Windows、{{n}}、SMTP、OAuth2、URL 等)约 45 个为合理中立项;真未翻译约 12–15 个,明细见 2.1 抽样。
- 本报告统计均为静态扫描,`vi.mock("@/connect")` 计数 38 指测试文件中含该 mock 的文件数。