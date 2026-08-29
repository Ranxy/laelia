# 巨型单文件页面深度审查:agent-profile / machine-profile / chat-conversation

> 审查范围:`frontend/src/pages/dashboard/agent-profile.tsx`(2405 行)、`machine-profile.tsx`(2200 行)、`chat-conversation.tsx`(2166 行)。技术栈:React 19 + Zustand 5(单 store 多 slice,`src/stores/`,无 react-query)+ ConnectRPC(proto-es)+ Tailwind 4 + Biome。
> 所有行号均已逐一核对,所有三个文件已全文阅读,结论均在仓库内 grep/验证过。

---

## 0. 总览指标

| 指标 | agent-profile | machine-profile | chat-conversation |
|---|---|---|---|
| 总行数 | 2405 | 2200 | 2166 |
| `useState` | **50** | **56** | 19 |
| `useEffect` | 7 | 7 | 13(+2 `useLayoutEffect`) |
| `useRef` | 8 | 3 | **25** |
| `useCallback`/`useMemo` | 0 / 0 | 0 / 1 | **26** / 0 |
| `React.memo` | 0 | 0 | 1(文件内 `MessageList`) |
| 对应测试 | `agent-profile.test.tsx`(852 行) | `machine-profile.test.tsx`(826 行) | **无页面级测试**(仅 `MessageRow` 级:`chat.test.tsx`) |
| 顶层组件数 | 1(全部内联) | 1(全部内联) | 3(`MessageList` + 页面 + 空态) |

三个文件的本质问题是:**"页面 = 数据获取 + 表单状态机 + 弹窗集群 + 派生计算 + 全部 JSX"五个职责压在一个没有拆分的组件里**;其中两个 profile 页之间又互相复制(约 600–900 行级别),chat 页则把一个复杂的"滚动 + 乐观更新"状态机内联在组件里(25 个 ref)。

---

## 1. 组件拆解(逐文件路标)

### 1.1 `agent-profile.tsx`(2405 行,单组件 `AgentProfilePage`,L79 起)

**区块路标(JSX)**:

| 区块 | 行范围 | 数据来源 / 绑定状态 | 事件处理 |
|---|---|---|---|
| 加载/失败分支(提前 return) | 448–466 | `agent`(本地 useState)、`loadError` | `loadAgent` 重试 |
| 身份卡 Identity & status | 983–1237 | `agent`、`users`、`avatarSrc`、`machineProviders` | 跳转机器卡、`openTransferPicker`(1027)、描述行内编辑(1057–1119)、persona 行内编辑(1121–1184)、头像上传(1186–1235) |
| Channel access 卡(3 个开关) | 1240–1279 | `agent.allowAddToChannel` 等 3 个 proto 字段 + 3 个 saving state | `handleToggleAllowAdd/FollowOwner/CanManageMembers`(789–843) |
| Actions 卡(启停/重启/删除) | 1281–1337 | `agent.enabled` | `handleStart`(264)、4 个弹窗 open/error state(236–244) |
| Runtime config 表单 | **1339–2201(≈860 行)** | 17 个 config useState(115–146)+ `configRef` 镜像(156–171) | `saveConfig`(730)、`savePersona`(748)、`refreshModels`(623)、`fetchPiModels`(651)、十几个 select/input 的 `onValueChange`+`onBlur` |
| 底部弹窗集群:Restart/Stop/Delete AlertDialog ×3 + 转让两段式 Dialog+AlertDialog | 2205–2402 | 15 个弹窗 useState(230–244) | `handleStop/Restart/Delete/Transfer`(246–313, 864–890) |

**状态普查**:50 个 `useState` ≈ 30 个表单字段 + 11 个 saving/busy/error + 5 个弹窗 open 标志 + `agent/loadError` + pi 模型列表三态;8 个 ref 中 `configRef`(156)是整个草稿的**影子副本**——每个输入都要双写(state + `configRef.current = {...}`),例如 L920–924、L1384–1400、L1584–1593、L1925–1929。

**圈复杂度来源**:
1. **pi 三态机**:provider(`builtin-pi`/`pi`/10+ 机器探测 provider/`custom`)× `piMode("own"|"global"|"self")` × `selfProvidedKeysEnabled` × `canEditAdminOnly` 的四维分支。`onValueChange` 的级联重置最多一次清 8 个字段(L1384–1415)、连写 5 处 `configRef.current`。
2. **保存队列**:`saveChainRef` promise 链(L177,568–594)+ `agentRef` 快照(L172–173)+ 双 payload 构造器(`buildFromDraft` 508 / `buildFromPersisted` 537)+ BigInt 兼容的脏比较(`stringifyConfig` 499)。
3. **权限二重门**:`canEdit`(server 每资源解析,L472)与 `canEditAdminOnly`(workspace 权限,L91)在各按钮/fieldset(1362–1364)重复判断。

**关键处理函数清单**:`handleStop/Start/Restart/Delete`(246–313)、`loadAgent`(315)、`enqueueSave`(568)、`refreshModels`(623)、`fetchPiModels`(651)、`canSaveFor`(695)、`isConfigDirty`(723)、`saveConfig`(730)、`savePersona`(748)、`saveDescription`(767)、3 个 toggle(789–843)、`userTitle`(848)、`handleTransfer`(864)、`renderPiContextFields`(905,被 2 处复用的 JSX 生成函数)。

### 1.2 `machine-profile.tsx`(2200 行,单组件 `MachineProfilePage`,L106 起)

**区块路标**:

| 区块 | 行范围 | 绑定状态 | 事件处理 |
|---|---|---|---|
| 升级横幅 ×3 | 856–898 | `machine.upgradeStatus` + 3s 轮询 effect(364–373) | `handleUpgrade`(531) |
| 加载/失败分支 | 375–393 | `machine`、`loadError` | `reload` 重试 |
| 身份卡 | 903–971 | `machine`、`users` | 跳转 owner |
| Token & 连接卡 | 974–1050 | `revoking/forcing/actionError` + 2 个 copied state | `handleRevokeToken`(570)、`handleForceDisconnect`(620)、`handleCopyInstall/Setup`(549/560)、`openTransferPicker`(591) |
| Access(IAM)卡 | 1053–1081 | `policyState`、`agentCreatorMembers` memo(330) | `openAccess`(763) |
| Providers 卡 | 1086–1125 | `machine.info.availableProviders` | `handleRefreshProviders`(512) |
| Agent roster 卡 | 1127–1193 | `agents`(本地)、`agentsLoading` | 行点击导航 |
| **Add-agent Sheet** | **1199–1847(≈650 行)** | ~28 个散装 useState(154–195,含 13 个 ACP 字段) | `handleAddAgent`(639,20 条校验)、`resetAddForm`(490,连清 17 个 setter)、`fetchPiModels`(252)、`refreshModels`(436) |
| Created 提示 Dialog | 1850–1863 | `addedOpen/addedTitle` | — |
| Access 管理 Sheet | 1866–2003 | `accessMembers`(Set) | `handleAccessAdd/Remove/Save`(770–841,含 etag 冲突处理) |
| Revoke/Force AlertDialog ×2 + 转让两段式 | 2005–2059, 2090–2197 | 12 个弹窗 state | 同上 |

**圈复杂度来源**:与 agent-profile 同构的 provider×piMode×权限三维分支(1252–1817),**外加**:`handleAddAgent` 的 20 条内联校验(639–691)与提交按钮 disabled 表达式(1825–1840)**对同一规则的两份手写副本**;IAM 的 etag 乐观并发(786–841)。

### 1.3 `chat-conversation.tsx`(2166 行,`ChatConversationPage` L217 起且被 `activity-detail` 内嵌复用)

**区块路标**:

| 区块 | 行范围 | 绑定状态 |
|---|---|---|
| `MessageList`(memo) | 156–215 | props 全部来自页面(15 个回调/props) |
| 存储 selector 区 | 224–309 | **~33 个 `useAppStore` 逐 key 订阅**(消息、成员、activity、jump、hasOlder/Newer 等,均是 per-conversation key 切片——这是全库做得最好的部分) |
| 滚动状态机 | 967–1028(`handleScroll`)、613–707(jump 布局 effect)、713–767(anchor 捕获/抑制)、830–876(锚点恢复) | **10 个 ref**:`pendingScrollAnchorRef`、`nativeScrollAnchorSuppressed*` + token、`restoringHistoryScroll*` + token、`lastScrollTopRef`、`suppressHistoryLoadRef`、`stickToBottomRef`、`lastChannelRef`、`messagesRef` |
| 会话初始化 | 493–554(`init`) | `loadMessages`、`startWatchingChannel`(长轮询 watcher,lives 在 store)、`listChannelMembers`、`markConversationRead` |
| 草稿缓存 | 373–378, 886–909 | `draftRef`(per-channel input/attachments/mentions) |
| 深链 effect | 744–811 | `?thread=` / `?message=&version=` 一次性跳转 |
| 发送(乐观) | 1141–1296(`handleSend`,~155 行) | `useAppStore.setState` 直接写 5 处(1089/1181/1221/1240/1268) |
| 上传 | 1044–1139 | `uploads` state + `inFlightUploadsRef` + `adoptedUploadIdsRef` + `activeOptimisticIdRef` |
| 其余回调 | 1298–1554 | mention 选择、reaction、thread、复制 markdown、转任务、3 种跳转、面板开关(共 ~15 个 useCallback) |
| JSX:header / 消息区 / composer / Thread / Tasks / 3 Drawer / MentionDetailSheet | 1556–2143 | — |

**圈复杂度来源**:① 上面的 10-ref 滚动状态机(方向判定、程序化滚动豁免、generation token、ResizeObserver 重居中、原生 scroll-anchoring 抑制,共约 350 行);② 乐观发送与上传"领养"(adopt)机制的交错(1141–1296);③ 4 种会话类型 × 归档/只读/移动端的面板分支。

---

## 2. 跨文件重复(file:line 证据)

| # | 重复模式 | 位置 | 严重度 |
|---|---|---|---|
| D | **ACP 配置表单整体复制**(provider 级联重置 select、pi 三态 select、global provider/entry 级联、self 模式 provider/baseURL/model/key/refresh、custom protocol/executable/args、env 编辑器、派生命令提示) | agent 1378–2198 ↔ machine 1252–1803;`profile-common.tsx` 头部注释自认:"the ACP config form itself is still page-local" | 高 |
| D | **`fetchPiModels` + `piModelsCacheRef` + 刷新按钮** | agent 651–687、146 ↔ machine 252–283、172;**行为已漂移**:agent 出错写 `piModelsError`+toast,agent 的 key 输入防抖 600ms(ref 手工实现 1930–1946)而 machine 是 400ms 的 effect(287–297) | 高 |
| D | **两步式 ownership 转让**(选人 dialog → 确认 AlertDialog,含 reason/busy/error) | agent 853–890 + 2299–2402 ↔ machine 587–618 + 2090–2197(两份 ~180 行弹窗 JSX 逐字同构,仅 t() key 前缀不同) | 高 |
| D | **`getSetting("settings/llm_agent_config")` 直连 RPC 逐字复制** | agent 363–374 ↔ machine 236–248;而 `stores/setting.ts:122` 已有 `fetchLlmAgentConfig()` slice | 中 |
| D | **加载失败 + 重试分支**(`if (!x) return <loading/loadError+retry>`) | agent 448–466 ↔ machine 375–393;同模式亦在 `agent-mcp.tsx`、`machine-workspace.tsx`(grep `loadError` 命中 4 个页面) | 中 |
| D | **`userTitle`(users/{id}→标题,回退原名)** | agent 848–851 ↔ machine 741–744 | 低 |
| D | **provider 列表项渲染(displayName + incompatibilityReason 拼接)** | agent 1437–1448 ↔ machine 1115–1123、1287–1298 | 低 |
| D | **身份 `<dl>` 网格 + `formatTimestamp` 行** | agent 985–1055 ↔ machine 904–970 | 低 |
| D | **会话类型魔法数 `1/2/3/4` 常量复制 5 份** | chat-conversation 104–106、`components/chat/conversation-list.tsx` 44–46、`global-search.tsx` 412–415、`agent-chat.tsx` 12–13、`activity-detail.tsx:170`(内联 `type === 1 || type === 4`) | 中 |
| D | **破坏性确认 AlertDialog 样板** | 本模块 8 个;全库 15 个页面使用同一套 30 行样板(`alert-dialog.tsx` 使用者列表见 grep) | 中 |
| D | **折叠 env entry → Record 的 fold 逻辑三份** | agent 476–486(`foldCustomEnv`)、machine 441–446(`refreshModels` 内)、machine 694–699(`handleAddAgent` 内) | 中 |
| D | **实体双重拉取**:layout 与 page 各打一次 GetX | `app/layouts/agent-detail-layout.tsx:59` + agent-profile:317 = 2× GetAgent;`app/layouts/machine-detail-layout.tsx:45` + machine-profile:214 = 2× GetMachine;agent-profile 还第三次 GetMachine(356) | 中 |

---

## 3. 设计问题

**D-1 (高) 数据流"谁负责拉数据"没有单一名义。** 实体页有意不用缓存(`stores/agent.ts:64-77`、`machine.ts:55-65` 注释解释了原因:canEdit/is per-caller),于是每个页面自己 `useState` + `loadX` + 每次变更手动 refetch 链(`setAgent(await getAgent(...))` → 再 `fetchAgents({pageSize:100},{silent:true})` 同步全局花名册,agent 579/774/795/875)。结果:①layout 与 page 双请求(§2 末行);②页面承担了本属于 store 同步逻辑的"跨页面缓存修正"职责;③没有 `useAgent(id)` / `useMachine(id)` 这样的资源 hook,新页面只能再复制一遍。
**建议**:新增 `useResource(fetcher, key)` 骨架 hook({data, loading, error, refetch, mutate},内置取消与 unmount 保护),profile 页与 layout 共用同一 hook 实例数据;`agents`/`machines` roster 的修正改为 store action 内部职责(如 `updateAgent` 内部同步 agents 数组),页面不再自行 `fetchAgents`。

**D-2 (高) proto 结构被手工摊平到 30+ 个 useState。** `AgentACPConfig` 的 17 个字段被拆成 17 个 useState(agent 115–131)+ 一个手维护镜像 `configRef`(156–171),再加 4 个手写映射器(seed effect 380–436、`buildFromDraft` 508、`buildFromPersisted` 537、machine 的 15 个散装 state 156–179)。**给 proto 加一个字段需要改 5 处**。profile-common 注释也承认两页 wiring 不同所以没共享——但真正的修法不是共享 JSX 而是抽出 reducer hook。
**建议**:`useAcpConfigDraft(entity) → { draft, setField, reset, isValid, isDirty, toInput() }`,内部单 reducer;两页共用,`configRef` 影子同步自然消失。

**D-3 (中) 组件里定义本应是 lib 的逻辑**(证据见 §2 D 组各行)。特别是:`foldCustomEnv`(agent 476)、`toOptionalBigInt`/`stringifyConfig`(agent 491–503,BigInt→string 再 JSON 的脏比较是通用需求)、`userTitle`×2、`memberLabel`(machine 746)、会话类型谓词 ×5 文件、DM peer 头像名拼接 `${peer}/avatar`(chat 451)。

**D-4 (中) 权限逻辑三分天下。** ①server per-resource 布尔(`agent.canEdit` 472、`machine.canEdit/canCreateAgent/canManage` 395–397 + `hasAnyAction` 400);②workspace 权限 `useHasPermission("laelia.agents.edit")`(agent 91);③chat 页手写属主比较 `channel.ownerId === currentUser.handle`(444–445)。门控散落在 `disabled` 属性、提前 return、`fieldset disabled`(agent 1362)三层,注释(agent 88–90、468–471)承担了大量"为什么这里用 A 不用 B"的解释成本。
**建议**:沉淀 `useResourcePermissions()`(返回 `canEditAdmin/canEdit/canCreate/canManage/viewOnly`),页面只消费一个能力对象。

**D-5 (中) chat 组件直接以 `useAppStore.setState` 写 store 5 处**(1089–1107 进度镜像、1181–1189 乐观插入、1221–1235 附体替换、1240–1247 回滚、1268–1275 失败回滚),而 store 侧 `sendChannelMessage`(channel.ts:319–382)又有一套自己的乐观处理(`appendNewMessages` 去重回放)。两套乐观更新约定并存,回滚逻辑(包括 §4 B-3 的丢文本 bug)只能藏在组件闭包里。
**建议**:把"乐观发送 + placeholder 替换 + 失败回滚"整体下沉为 `channelActions.sendPendingMessage(composer 输入)`;组件只负责收集 `text/mentions/attachments`。

**D-6 (低) 表单验证双写**:machine `handleAddAgent`(639–691)的 20 条校验与提交按钮 disabled 表达式(1825–1840)是同一规则的两份手写副本,新增校验必须同步两处。
**建议**:`useAcpConfigDraft.isValid` 单一来源,按钮与 handler 都读它。

---

## 4. 潜在 bug 与脆弱点

| # | 位置 | 严重度 | 问题与证据 | 建议 |
|---|---|---|---|---|
| B-1 | chat-conversation 886–895(草稿恢复)、320(`inFlightUploadsRef`)、1109–1113(上传完成 `setPendingAttachments`) | **高** | **切换会话后,未完成上传会"串台"到新会话的草稿**。切会话时恢复 effect 只重置 `input/pendingAttachments/mentionMap`,不清 `uploads`、不清 `inFlightUploadsRef`;在会话 A 开始上传 → 切到会话 B → 上传完成,附件被 push 进当前(=B)的 `pendingAttachments`,进度 chips 也继续显示在 B 的输入区,最终随消息发到错误会话 | 恢复 effect 同时清空/隔离 `uploads`、`inFlightUploadsRef`;或把 composer 整体(含 uploads)纳入 per-channel 状态;上传完成回调按发起时的 `conversationName` 落盘 |
| B-2 | chat-conversation 1905–1917(仅在 `state?.active` 时重建 map)、1326–1331(选中累加)、1255(发送时直接用 `mentionMap`) | **中** | **陈旧 mention**:用户输入 `@alice` 后再把该 token 删掉,`mentionState` 变 null,`setMentionMap` 不会触发清理 → 消息正文没有 @alice 却仍向 alice 发 mention 通知;且 `draftRef` 会把残留 mention 跨会话保存 | 在 `onChange` 里无条件重建/修剪 mention map(扫描现有 @token),或仅在"选人动作"里追加、正文 token 删除时同步删除 |
| B-3 | chat-conversation 1193(发送前 `setInput("")`)与 1265–1277(catch 回滚) | 中 | **发送失败丢正文**:失败时恢复 `pendingAttachments` 与 `asTask`,但从未恢复被清空的 `input`,用户打的字全部丢失 | catch 中 `setInput(text)`(text 已在闭包里) |
| B-4 | agent-profile 315–326、350–359;machine-profile 213–230、302–324 | 中 | **数据拉取无取消**:quickly 切换 agentId/machineId 时两个 in-flight GetX 竞速,后完成者覆盖(不是后导航者);`getMachine().then(setMachineProviders)` 同理可把机器 A 的 provider 列表显示在会话 B 上;unmount 后 setState。对比:`agent-detail-layout.tsx:57–65` 是写了 `cancelled` 标志的正确写法,同一仓库两种规范并存 | 抽 `useResource` hook(内含 `cancelled`/`AbortController`),所有 load 一次性替换 |
| B-5 | agent-profile 325、334、435;machine-profile 229、296、325 | 中 | **6 处 `eslint-disable-next-line react-hooks/exhaustive-deps`**。其中 agent:435(seed effect,依赖只有 `agent?.name`)是**有意为之**且注释完备(防止自动保存后的 refetch 打掉编辑中草稿),逻辑本身成立,但该 effect 里实际读写 20+ 个 setter,disable 掩盖了"今后有人把依赖改成 `agent` 就会踩编辑丢失"的现实风险。其余 5 处属"挂载即拉取"惯例;machine 317–326 还把 `fetchUsers`(pageSize 1000)与 `loadPolicy`/`listGroups` 三个不相关副作用混在一个 effect 里 | 混合副作用 effect 拆分;seed effect 改为"以 `agent?.name` 为 key 的 key 强制重挂载子组件"(即 `AGENTS.md` 规定的 outer wrapper + key 模式),从根上消除 disable |
| B-6 | agent-profile 177/180(1.5s)、582;machine-profile 554/564(2s);chat 1284、1334 | 低 | 未清理的定时器:`savedTimerRef` 在 unmount 时不清(440–446 只清 apiKey 防抖);复制按钮 `setTimeout` 裸调;unmount 后 setState(React 19 无警告但属脏写) | 统一 `useTimeout` 小工具,自动清理 |
| B-7 | chat-conversation 与 `activity-detail.tsx:180` | 中 | **死契约**:`ChannelConversationViewProps.onClose`(124)声明了、`activity-detail` 也传了 `onClose={() => navigate("/activity")}`,但组件从未读取 `props?.onClose` → 内嵌视图的关闭回调静默无效 | 二选一:实现 header close 按钮或删除属性 |
| B-8 | machine-profile 763–768(`openAccess` 直接填充)+ 341–349(policy 迟到时覆盖) | 低 | IAM sheet 打开后若 policy 晚到,effect 会用 `new Set(agentCreatorMembers)` 覆盖管理员在间隙里做出的增删(窗口极小但存在) | 统一"以 policy 加载完成事件为唯一初始化点",删掉 `openAccess` 里的预填充与 `accessInitializedRef`(206)冗余路径 |
| B-9 | agent-profile 330–335(`pageSize:100`,且仅 `users.length===0` 时拉) ↔ machine 319(`pageSize:1000`) | 中 | 转让目标选择框在 agent 页**静默截断到 100 个用户**,两页 pageSize 还不一致;拉取失败是 `silent:true`,选框只会是空列表,无错误提示 | userTitle/选人数据统一走一个 `useUserNames()`(store 层缓存),pageSize 统一 |
| B-10 | machine-profile 364–373 | 低 | 升级轮询:若升级卡死(机器永不上线),interval 永不停止(仅靠 `upgradeInProgress` 终态退出);`upgrading` 按钮态只反映"触发"结果,与轮询进度脱节 | 给轮询加最长时限或指数退避即可,优先级低 |

---

## 5. 性能

**P-1(高)agent-profile:任何输入键入 = 整个 2405 行树重渲染。** 50 个 state 全部挂在顶层:在 `executable`/api key/context window 等任意输入框每敲一键,都会 re-render 身份卡、3 个 toggle 卡、actions 卡、**约 860 行的 runtime config 表单**、以及 5 个 AlertDialog + 转让 2 段 Dialog(它们始终挂载)。文件内 0 个 `React.memo`。
**建议**:最小拆两块并 memo 化:①`RuntimeConfigCard`(1339–2201,表单 state 收进 reducer,与页面只共享 `agent`/`canEdit`/providers);②`AgentIdentityCard`。可把单键 re-render 范围缩小一个数量级。

**P-2(高)machine-profile:Add-agent Sheet 的每次键入重渲染整个 profile 页。** ~28 个 sheet 状态都挂在页面级(154–195),而页面还同时承载 IAM 卡、providers 卡、roster。650 行 sheet JSX(1199–1847)应抽为 `AddAgentSheet({ open, machine, onClose })` + 内部表单组件 + `key`(AGENTS.md 已有现成模式),顺带替代手写 `resetAddForm()`(490,17 连清)。

**P-3(中)chat:上传进度事件逐 tick 重映射整个消息数组。** onProgress(1082–1107)里每次进度回调都 `chatMessages[conversationName].map(...)` 生成新数组(为乐观气泡更新进度),外加 `setUploads` 全页 re-render。多文件并行上传时 = 每秒 N×tick 次的全列表分配。**建议**:进度高频写入降频(节流 100–200ms)或把"上传中文件→乐观气泡"的映射下沉到 store 单点更新。

**P-4(中)消息列表未虚拟化。** 当前窗口 = 最新 100 条 + 每次滚动 +30(`chat.ts` 309–398),长时间阅读可达数百 DOM 行;`MessageList`(156–215)对整表 map,每行构造 `rowStreamingProps` 新对象、`senderKeyForMessage(prev/cur)` 两两比较。缓解项已有:每行 `MessageRow` 自己的 memo + lazy-markdown(`eager={messages.length <= 40}` 门控)。**现状可接受,建议在长会话(>500 行常驻)实测卡顿后引入虚拟化**(`@tanstack/react-virtual`),优先级低于拆分。

**P-5(低)composer 每键全量正则扫描**(1905–1917,`/(?:^|\s)@(\S+)/g` + targets.find);以及 `handleSend`/`uploadFile` 闭包每输入重建(deps 链)——量级小,拆出 Composer 组件后自然消解。

**P-6(正面)chat 的 per-key 订阅与 store 侧等值跳变是本库亮点**:逐 key selector + `EMPTY_*` 常量(83–85)、`agentActivitiesEqual`(channel.ts 49–61)、watcher append 同引用 bail-out(channel.ts 484–505),这些应在重构中**作为范例保留**。

---

## 6. 死代码 / 历史债务

| # | 位置 | 严重度 | 说明(均已 grep 验证) |
|---|---|---|---|
| C-1 | `stores/agent.ts:230-238` + `stores/types.ts:300` | 低 | `refreshAgentProviders` store action **全库零调用**(仅定义+类型);agent profile 页并未像 machine 页那样提供"刷新 providers"按钮。删除或接入 UI 二选一 |
| C-2 | chat-conversation 124 + activity-detail 180 | 中 | 见 B-7:`onClose` 声明并被嵌入方传值但从未读取,是带契约的死代码(比纯死代码更危险) |
| C-3 | machine-profile 206、341–349 vs 763–768 | 低 | `accessInitializedRef` 双路径初始化(见 B-8),其中一条是冗余防御 |
| C-4 | `stores/setting.ts:57,122-129` | 低 | `fetchLlmAgentConfig` slice 与两页面的直连 RPC 并存;两页绕过 slice 的同时,slice 的 `llmAgentConfig` state 只为其它设置页服务——同一配置三个获取入口(tsc/biome 均干净,故没有经典 unused import 可报,债务在"绕过层") |
| C-5 | chat 头部注释(108–118)与文件尾 `ChannelConversationView = ChatConversationPage`(2166) | 低 | 页面以"可选 props 同时服务路由与内嵌"双身份存在,`onViewInChannel/onClose` 语义只对内嵌生效——建议内嵌场景独立成薄包装组件,让路由组件不再带 props(与 `CONVERSATION_TYPE-*` 抽到 lib 同步做) |
| C-6 | `stores/channel.ts:374-380` 注释 | 记录 | "旧 pollChannelMessages 双轮询已删除"的历史债已在该层还清(channel.ts 注释自证),证明 store 层经历过整理;**剩余债务集中在三个页面文件**——这也说明重构页面层与 store 层演进方向一致 |

**死代码核查说明**:`pnpm exec tsc --noEmit` 与 `biome lint`(三个目标文件)均零告警——三个文件没有经典的 unused import/变量;真正的债务是"声明了但契约断裂的 prop"(C-2)、"零调用的 store action"(C-1)与"绕过既有层的复制逻辑"(C-4),已逐一 grep 引用核实。

---

## 7. 重新设计视角

### 7.1 共享基础设施(先建,三个页面同时受益)

```
lib/conversation-type.ts          // 会话类型枚举+谓词,替换 5 处魔法数
hooks/useResource.ts              // {data,loading,error,refetch,mutate} + 取消 + unmount 安全
hooks/useAcpConfigDraft.ts        // ACP 配置 reducer:seed/setField/isValid/isDirty/toInput()
hooks/usePiModelOptions.ts        // fetchPiModels + per-provider 缓存 + 统一 400ms 防抖(两份合一)
hooks/useTransferOwnership.ts     // 两步转让状态机 + <TransferOwnershipDialog resource="agents|machines">
components/confirm-action-dialog.tsx  // 通用破坏性确认(title/description/busy/error/onConfirm)
components/agent/acp-config-editor.tsx // ACP 表单(edit/create 两 mode),内部按需 memo
hooks/useLlmAgentConfigSetting.ts // 走 setting slice,替换 2 处直连 getSetting
```

### 7.2 目标组件树

**agent-profile(目标 ≈300 行叶子页)**

```
AgentProfilePage                    // useParams + useAgentResource(agentId) + 失败分支
├─ AgentIdentityCard(memo)          // 身份网格 + InlineTextareaEditor×2(描述/persona)+ 头像(useAvatarEditor)
├─ AgentChannelAccessCard(memo)     // 3×ToggleRow,保存走 useResource 的 mutate
├─ AgentActionsCard(memo)           // 4 个 ConfirmActionDialog(启/停/重启/删除)+ TransferOwnershipDialog
└─ RuntimeConfigCard(memo, only-render when agent loaded)
   └─ AcpConfigEditor(mode="edit")  // 860 行表单收编于此;provider×piMode 分支内部
      ├─ ProviderSelect / PiModeSelect
      ├─ PiManagedFields / PiSelfFields(usePiModelOptions)/ CustomFields
      └─ model refresh(useAgentModelsRefresh)
```

状态归属:`agent` 实体在 `useAgentResource`;表单草稿在 `useAcpConfigDraft` 编辑器内部;pi 模型缓存在 hook;`users` 解析收敛为 store 级 `useUserNames()`(顺带修 pageSize 漂移与 B-9)。

**machine-profile(目标 ≈350 行)**

```
MachineProfilePage
├─ useMachineResource(machineId)     // machine + agents 一并 reload(即现 reload())
├─ MachineUpgradeBanner              // 内含 3s 轮询(usePollWhen),消费 upgradeStatus
├─ MachineIdentityCard(memo) / MachineTokenCard(+ 2 个 ConfirmActionDialog + 复制命令)
├─ MachineAccessCard → ManageAccessSheet   // IAM + etag 逻辑收进 useMachineIamPolicy
├─ MachineProvidersCard / MachineAgentRoster
└─ AddAgentSheet → AddAgentForm(key=重新打开)
   └─ AcpConfigEditor(mode="create") // 650 行 sheet 收编,20 条校验归 isValid 单一来源
```

**chat-conversation(目标 ≈400 行;chat 建议最后动,先补测试)**

```
ChatConversationPage               // props 路由/内嵌双身份 → 内嵌走独立 Wrapper
├─ ConversationHeader(memo)        // peer 头像/徽标/成员摘要 + 4 个入口按钮
├─ useConversationData(convId)     // init、watcher 启停、GetChannel 兜底、成员、已读
├─ MessageArea
│  ├─ useMessageScroller(scrollRef,{onLoadOlder,onLoadNewer,jump,deepLink})
│  │                               // 吸收 10 ref/10+ effect/方向判定/token 机制——纯机械搬移
│  ├─ HistorySentinels / ScrollDownButton
│  └─ MessageList(现有 memo,保留)
├─ Composer(自治组件:input/uploads/mentions/asTask;暴露 onSend)
│  ├─ useComposerDrafts(convId)    // 草稿缓存(修复 B-1:连 uploads 一起按会话隔离)
│  ├─ AttachmentChips / MentionPopup
├─ ThreadPanel / TasksPanel / 3×ChatDrawerSheet / MentionDetailSheet(现状保留)
└─ store:sendChannelMessage 承接乐观发送(D-5),组件内 5 处 setState 归零
```

### 7.3 迁移步骤与回归保障(每步可单独合并)

1. **纯函数先走**:`conversation-type.ts`、`foldCustomEnv/toOptionalBigInt/stringifyConfig`、`userTitle` 抽取——`pnpm type-check` + 现有 1600 行 profile 测试直接护航,零行为风险(0.5–1 天)。
2. **弹窗抽象第二**:`ConfirmActionDialog` + `TransferOwnershipDialog` 替换 8 个弹窗;两个 profile 测试已断言 transfer 调用与错误 toast(`agent-profile.test.tsx:789`、`machine-profile.test.tsx:413`),改完跑 `pnpm test`(1–2 天)。
3. **hook 化数据层**:`useResource` 落地后替换 4 处 load;用 test-server(`scripts/test-server.sh run`)手工回归"快速切换实体不串台"(0.5 天 hook + 0.5 天替换)。
4. **chat 先补页面级测试再动刀**:覆盖 init watcher 启停、深链三种、发送-含上传、jump 窗口(1 天)。这是目前唯一无页面测试的文件,是重构前置条件。
5. **ACP 表单合一**(最大件,3–4 天):先在 agent-profile 上把编辑器抽出(`AcpConfigEditor`),测试全绿后 machine AddAgentForm 第二次复用;`isConfigDirty`/`canSaveFor`/`enqueueSave` 语义逐条对拍现有测试。
6. **chat 滚动状态机搬移**(2–3 天):`useMessageScroller` 为机械搬移;为新 hook 写"快速滚动/程序化滚动不误触 load"的单测,再删页面内原件。
7. 每步固定收尾:**`pnpm --dir frontend biome:check && pnpm type-check && pnpm test`**,UI 类改动加 `pnpm --dir frontend check`(layering/i18n 扫描)。

---

## 8. 本模块重构优先级清单(按性价比排序)

| 优先级 | 事项 | 对应发现 | 预估工作量 |
|---|---|---|---|
| **P0-1** | 修 chat 上传跨会话串台 + 草稿恢复遗漏 uploads | B-1 | 0.5–1d |
| **P0-2** | 修发送失败丢正文文本 | B-3 | 0.25d |
| **P0-3** | 修陈旧 mention + mention map 随正文修剪 | B-2 | 0.5d |
| **P0-4** | `lib/conversation-type.ts` 收敛 5 处魔法数;删除死 prop `onClose`(或实现它) | §2 D 组、C-2 | 0.5d |
| **P0-5** | `getSetting(llm_agent_config)` 走 setting slice,删两处复制 RPC | §2 D 组、C-4 | 0.5d |
| **P1-1** | `ConfirmActionDialog` + `TransferOwnershipDialog` 抽象(−~350 行弹窗样板) | §2 D 组 | 1–2d |
| **P1-2** | `useResource` hook + 消除 layout/page 双请求 + 所有 load 加取消 | B-4、D-1 | 1–1.5d |
| **P1-3** | `usePiModelOptions` 合一(缓存+防抖统一),`fetchPiModels` 双份删除 | §2 D 组 | 1d |
| **P1-4** | `AcpConfigEditor` 抽出(agent 页先行,machine 复用)+ `useAcpConfigDraft` | D-1、D-2 | 3–4d |
| **P1-5** | machine AddAgentSheet 独立组件化(key 模式替代 resetAddForm) | P-2、D-6 | 1–2d |
| **P2-1** | chat 页面级测试补齐 → `useMessageScroller` 机械搬移 | §7.3-4/6 | 1d + 2–3d |
| **P2-2** | 两个 profile 页按区块拆 memo 子组件(Identity/Actions/Access 卡等) | P-1 | 1–2d/页 |
| **P2-3** | chat 乐观发送下沉 store slice(组件内 5 处 setState 归零) | D-5 | 1d |
| **P2-4** | 消息列表虚拟化(仅在实测长会话卡顿后) | P-4 | 2d(可延后) |
| 清理 | 删除零调用的 `refreshAgentProviders`;`savedTimer`/copy 定时器统一清理 | C-1、B-6、C-3 | 0.5d |

**总量约 2–3 人周。** 建议的执行顺序即 P0(≈2.5 天,全是低风险高确定性的 bugfix 与收敛)→ P1(结构收益主力,两个 profile 页可先动,测试现成)→ P2(chat 必须先补测试,其余可随版本节奏分批)。

---

**结语**:这份代码库的 store 层(per-key 订阅、等值跳变、watcher 生命周期)质量明显高于页面层;三个巨型文件的可维护性问题几乎全部可以用"已存在但未被贯彻的模式"解决——`useAvatarEditor`/`profile-common` 证明了抽取是既定方向,`CreateUserSheet` 的 key 模式、`stores/setting.ts` 的 slice、`stores/agent.ts` 的 per-caller 不缓存策略都有清晰注释。重构不需要引入新框架(react-query 暂无必要),而是把 4 个 hook + 3 个共享组件补齐,再把三棵页面树按 §7.2 归位。