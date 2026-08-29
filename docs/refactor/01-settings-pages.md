# 设置类页面(settings-*.tsx)深度代码审查报告

> **⚙ 实施进度标注(批 3 收口后)**
- ✅ 已完成:P0-1/2 前半(七连修 `5bc9f90`:MCP key 失焦、`common.no-data`、`common.deleting`、删除按钮 5 处)+ ProviderSheet 重置;错误出口统一(`389ce97`:全部裸 message → describeError/showErrorToast);死 i18n key 清除。identity-providers 测试仍缺。
- ⏳ 未完成:useResourceList 页面迁移与 CRUD 脚手架(§7 全套)、directory store(users/agents/machines 已由 Query 纵切吸收 `e7aca3a`,设置页仍直连——归 Phase 3 页面拆分阶段)、B4 editTarget 清除、B6/B7/B8。

> 审查范围:`frontend/src/pages/dashboard/settings-*.tsx` 全部 14 个源文件(整文件精读,非抽样),并交叉阅读了 `components/settings-page.tsx`、`components/member-picker.tsx`、`components/profile-common.tsx`、`components/ui/{sheet,field-row,alert-dialog}.tsx`、`stores/{setting,user,mcp,api-provider,index,permissions}.ts`、`lib/{connect-errors,toast,permissions,web-push,command-status}.ts`、`connect/index.ts`、`locales/{en-US,zh-CN}.json`、router 注册与全部 14 个对应测试文件。
> 验证手段:`pnpm type-check`(0 错误)、`pnpm biome:lint`(无告警)实际运行;i18n key 全量交叉扫描脚本;十余轮针对性 grep。技术栈前提:React 19 + Zustand 5 + ConnectRPC(proto-es)+ Tailwind 4 + Biome,无 react-query,数据获取为自定义 store + 各页自建 load。

---

## 0. 总体印象

这一组页面是典型的「同一个人、同一时期、按同一模板手工复制」的 CRUD 管理页族:7 个资源页(groups、roles、iam、api-providers、mcp-servers、identity-providers、profile)+ 7 个配置表单页(general、smtp、storage、agents、notifications、audit、menu)。

**做对的事**:
- `pnpm type-check` / `pnpm biome:lint` 全绿;
- 路由级 lazy-load(`router/routes/dashboard.tsx:303-421` 每页独立 `import()`);
- 除 identity-providers 外每页都有测试(14 个测试文件共 3,411 行);
- 共享件已开始浮现:`SettingsPage`/`PageLoading`/`PermissionNotice`(components/settings-page.tsx)、`FieldRow`、`MemberPicker`、`Sheet` 宽度档位。

**核心问题**:共享件只抽了「皮」(页框、字段行、选择器),没有抽「骨」——加载序列、CRUD 状态机、表格+抽屉模板、权限门、错误渲染、成员解析。于是同一套 ~200 行模板在 5–7 个文件里各存一份,并各自发生受控漂移;漂移本身又制造了一批真 bug(§4 中 B1/B2/B4/B5/B15 均为漂移产物)。

**规模**:14 个源文件 7,251 行 + 14 个测试文件 3,411 行;其中 7 个 CRUD 类大页(iam 917、mcp-servers 886、api-providers 810、groups 740、roles 703、profile 617、identity-providers 565)合计 **5,238 行**。

---

## 1. 重复代码量化分析

### 1.1 模板结构出现次数总表(全部有 file:line 锚点)

| # | 模板块 | 出现次数 | 典型锚点(每次均为同构代码) |
|---|---|---|---|
| R1 | `createOpen/createForm/creating` + `editOpen/editTarget/editForm/saving` + `deleteOpen/deleteTarget/deleting` 三组 useState(15–16 行/次) | 5× | mcp 170–185;api-providers 134–145;groups 101–116;roles 84–100;idp 110–123 |
| R2 | `const load = useCallback(async () => { setLoading(true); …RPC… toast(load-failed) … finally setLoading(false) })` | 7× | iam 120–143;mcp 187–225;api-providers 147–167;groups 123–144;roles 102–116;idp 125–139;audit 61–87(变体) |
| R3 | `useEffect(() => { if (canList) load(); else setLoading(false); }, [canList, load])` 权限门+加载 | 4× 完全同构 | api-providers 169–172;groups 198–201;roles 118–121;idp 141–144(iam 145–152 为变体) |
| R4 | `create()` 整函数:校验(toast title 报错)→ setCreating → RPC → toast(created) → close → `load()` | 4× | mcp 249–281;api-providers 174–233;groups 217–258;idp 182–228 |
| R5 | `save()` 整函数(同 R4 + updateMask) | 4× | mcp 283–318;api-providers 235–307;groups 260–310;idp 230–265 |
| R6 | `remove()` 整函数:RPC → toast(deleted) → close → target=null → `load()` | 4×(roles 252–270;iam 无删除) | mcp 320–343;api-providers 309–332;groups 312–333;idp 267–290 |
| R6b | 删除确认 `<AlertDialog>` 块(18–32 行,仅 i18n namespace 不同) | 6× | mcp 514–535;api-providers 449–470;groups 512–533;roles 605–636;idp 545–562;iam 894–914(「放弃修改」变体) |
| R7 | 表格空态行 `<TableCell colSpan={…} className="text-center … py-8/12">` | 6× | iam 454–462;mcp 632–641;api-providers 408–417;groups 469–478;roles 308–316;idp 432–435 |
| R8 | Sheet 页眉/页脚 + `onOpenChange={(v) => !v && onClose()}` + 保存按钮 `submitting ? t("common.saving") : t("common.save")` | 6 页 10 处 | mcp 693–700/878–885;api-providers 575–581/802–806;groups 577–581/732–737;idp 505–522/524–543;roles 500–518 |
| R9 | 「成员徽章 + × + MemberPicker」成员编辑器(约 45 行,除类型几乎逐行相同) | 2× 完全同构 + 1 变体 | **mcp 831–876 ≈ api-providers 756–800(逐行相同)**;iam 858–872 为第三变体 |
| R10 | `usedMembers` Set 派生(`useMemo(() => new Set(form.members))`) | 3× | mcp 677;api-providers 507;groups 571–574(非 memo 变体) |
| R11 | `memberLabel(member, users, groups)` 成员名解析函数 | 4× | **mcp 112–125 与 api-providers 108–121 逐字节相同**;iam 202–212(store 变体);machine-profile 746(漂移变体) |
| R12 | `emptyForm()` / `entityToForm()` 实体↔表单双向映射 | 5× | mcp 81–110;api-providers 80–106;groups 72–86;idp 55–87;roles 70–72 |
| R13 | 校验失败用 `toastManager.add({type:"error", title: …})` 而非字段级错误 | 8 处 | mcp 231–247;api-providers 175–197、237–268;groups 218–231、262–275;idp 169–180 |
| R13b | 本文件私有 `Field({label,hint,children})`(与已有 `ui/field-row.tsx` 平行造轮子) | 4× | settings-profile 587–609;smtp 204–226;storage 214–236;reminder-detail(同类) |
| R14 | `isMasked(secret)` | 2× 逐字节相同 | smtp 33–35;storage 35–37 |
| R15 | `displayName(user)` | 2× | member-picker 8–10;groups 88–90 |
| R16 | permission 四连 `useHasPermission` + `if (!canList) return <PermissionNotice/>` | 6× | api-providers 125–127/334–338;groups 94–95/335–337;roles 76–79/272–274;idp 102–105/292–300;iam 86–87/419–421 |
| R17 | 「Title → resource_id」slug 生成(两种不同实现) | 2× | roles `slugify` 56–61;idp 内联 188–193 |
| R18 | 行内按钮 `saving ? <Loader2/> : <Save/>` 图标+文案对 | 8+ 处 | general 279–284/385–390;profile 359–364;smtp 189–195;storage 199–205;notifications 153–157;roles 593–598 |

### 1.2 量化结论

- 7 个 CRUD 类页面合计 **5,238 行**;`pageSize: 1000` 的「全量拉取」调用在 settings 源文件中共 **17 处**。
- 逐块累计(R1–R18),机械重复约占 7 页总量的 **25–35%**,即 **1,300–1,800 行**是「同一模板的第 N 份拷贝」,且每次复制都伴随微漂移(错误出口、mask 风格、文案、清理时机各不一致,见 §4)。
- 若按 §7 建立统一脚手架,每页剩余的只有「列定义、表单字段、校验规则、RPC 调用」等真业务,估计 7 页合计可压缩到 **2,600–3,200 行**,即**净减 2,000–2,600 行(约 40–50%)**。
- 测试同样在为重复付税:14 个测试文件 3,411 行,**每个都各自 `vi.mock("@/connect", …)` 重塑整个 client 表面**(例:settings-iam.test.tsx:9–30、settings-groups.test.tsx 同构)。数据层统一后,测试可下沉为对 `useResourceList`/`useCrudDialog` 的单测 + 每页薄冒烟。

---

## 2. 设计问题

| # | 位置 | 严重度 | 问题与证据 | 重构建议 |
|---|---|---|---|---|
| D1 | stores/mcp.ts:11–44、stores/api-provider.ts:13–32 vs settings-mcp-servers.tsx:157–165、settings-api-providers.tsx:129–131 | **中** | **两套数据通道并存**:同一资源被两条路径重复实现——`mcpServers`/`apiProviders` store 切片(含 loading/silent 语义,供 agent-mcp.tsx:87、agent-profile.tsx:342、machine-profile.tsx:237 使用)与设置页的裸 `useState + client 直连`。两条读取路径、两份缓存语义、互不失效。 | 统一到一个资源 store/缓存层(§7);设置页 CRUD 成功后调用 `store.refresh(resource)` |
| D2 | groups 97–116;roles 81–100;idp 107–125;iam 92–118 | **中** | **这些资源根本没有 store 层**:每页自建 `useState + useCallback(load)`,「加载列表+loading+错误」每页造一遍轮子,无跨页缓存(roles→iam→groups 连续导航则重复 `listUsers/listGroups`) | 抽 `useResourceList`(含请求序号/AbortController)或最小缓存 slice(directory store) |
| D3 | mcp 231–247;api-providers 175–197;idp 169–180 等 | 中 | **表单处理零基建**:无 react-hook-form/zod(已验证 package.json),每个 `xxxForm` 手工 `{...form, field: v}` 展开,校验用「toast title 充当错误提示」,无字段级错误定位;idp 页甚至用字段 label 冒充错误文案(idp 170–179 `title: t("settings.identity-providers.field-title")`) | 校验返回 `Record<field, string>`,经统一 `FieldRow error` 渲染;轻量校验描述即可,不必引大表单库 |
| D4 | settings-iam.tsx:84–118 | 中 | 单组件 17 个 `useState`、两个 Sheet + 一个 AlertDialog + 全部业务函数内联在 917 行组件里;策略编辑逻辑(buildEditedPolicy 270–294 / buildEditedPolicyForRole 296–332)与视图混杂 | 下沉 `useIamPolicy`(load/save/etag 冲突重试)与 `<AssignRolesSheet>`、`<RoleMembersSheet>` 子组件;策略编辑是纯逻辑,极易单测 |
| D5 | settings-profile 343–353;smtp 115–127;storage 120–132 | 低 | `SettingsPage` 脚手架已存在且注释明说「以前每个页面重复实现这三块」(settings-page.tsx:34–59),但这 3 页仍在手写同一页框(scroll 容器 + hidden 标题 + PageLoading 组合),是抽象后的漏网之鱼 | 这 3 页改挂 `SettingsPage`(给 SettingsPage 加 `contentWidth` prop 以兼容 profile 的 max-w-2xl) |
| D6 | 全组:26 处 `err instanceof Error ? err.message : String(err)`(grep 实数)vs 33 处 `describeError` | 中 | **错误渲染双轨制**:`describeError`(lib/connect-errors.ts:37–50,可解出后端 IAM 的 `PermissionDeniedDetail`)已存在,但 26 处裸 message 并行;同文件内部也混用:roles 的 create/edit 用裸 message(210、246)而 delete 用 describeError(262–266)。权限被拒时一半页面只给一句裸英文 RPC 错误 | 收敛为唯一 `describeError` 出口(toast/inline 一致);可加 Biome no-restricted-syntax 规则兜底 |
| D7 | mcp 271/308/333(`void load()`)vs api-providers 223/297/322、groups 248/300/323 等 14 处(裸 `load()`) | 低 | 写成功后刷新的两种风格混用,后者是 floating promise | 统一 `void` 前缀;脚手架内收敛为 `refresh()` |
| D8 | api-providers 288(`"base_url"` snake_case)vs mcp 299(camelCase + oneof 名)、roles 219–239(按差异最小 mask) | 低 | updateMask 风格漂移,同一约定两种写法,后来者无据可依 | 在 store action 一处封 RPC + mask,页面不裸写 mask |
| D9 | settings-iam 54、settings-roles 42(`roleIDFromName` 来自 `@/lib/command-status`) | 低 | IAM 语义工具住在 command-status 里,依赖方向怪异 | 迁至 `lib/iam.ts`(或保留 re-export 并注明) |

---

## 3. 潜在 bug 与脆弱点(按严重度,均经二次核对)

| # | 位置 | 严重度 | 问题 | 代码证据 | 重构建议 |
|---|---|---|---|---|---|
| B1 | settings-mcp-servers.tsx:756–758 | **高** | **Header 名称输入框每敲一键失焦**。列表 `key={`${h.name}-${i}`}` 用「内容+下标」,在 Name 输入框打字时 key 每次变化 → React 卸载/重建该 `<div>` 及其中的 `<Input>` → DOM 元素被替换,焦点丢失;只能粘贴、不能逐字输入 header 名 | ```tsx {form.headers.map((h, i) => (<div key={`${h.name}-${i}`} …><Input value={h.name} onChange={(e) => updateHeader(i, { name: e.target.value })} …``` | key 改为纯 `i`(或挂稳定 id);内容仅可用于展示层,不可作 key |
| B2 | settings-api-providers.tsx:502–511(fetchKey/models/fetching/fetchError 为组件级 state,位于 Base UI Portal 之外,见 ui/sheet.tsx:103 Portal 只卸载 children)、513–536(fetchModels)、538–559(toggleModel);且 openEdit(379–383)无重置 | **高** | **ProviderSheet 组件级 state 跨 open/close 残留 → 跨提供商数据污染**。`<ProviderSheet open={editOpen}>`(434–447)常驻挂载。编辑 A(deepseek,输入 key、抓到模型列表)→ 关闭 → 编辑 B(openrouter)时:旧 models 列表仍显示、fetchKey 仍保留;此时点 B 列表里残留条目,`toggleModel` 会把 **A 抓到的 model.id + A 的 apiKey** 写进 B 的 entries 并随保存提交——静默数据污染 | 打开时重置 fetchKey/models/fetchError;或按 frontend/AGENTS.md「外层壳 + 内层表单 + stable-entity-ref + key」模式重写为内层组件持有 state |
| B3 | settings-iam.tsx:460;settings-roles.tsx:314;locales/en-US.json `common.no-data` | **高**(用户可见文案错) | `common.no-data` 的值是 **"No agents yet."**,被 IAM 绑定表与角色表空态直接渲染——管理员看到空 IAM 表时页面显示「No agents yet.」 | 改用独立 key(如 `common.no-records`)或各页自有文案;同时修正该共享 key 值(machines.tsx:152 也引用) |
| B4 | settings-identity-providers.tsx:524(edit Sheet `onOpenChange={setEditOpen}` 无重置)+ 332–347(placeholder/hint 依赖 editTarget) | 中 | **editTarget 关闭后永不清空**。任何一次关闭编辑抽屉后再点「创建」,`renderFormFields` 捕获的 `editTarget` 仍非 null → 创建表单的 clientSecret 字段错误显示「保留原值」placeholder 与 **secret-kept 提示**(342–346),误导管理员以为已有关联 secret | onOpenChange 里清 editTarget(对齐其它页);或与 B2 一起用 key-remount 模式根治 |
| B5 | mcp 531;api-providers 466;groups 529;roles 632;idp 558 | 中 | **删除按钮 loading 文案是「Saving…」**:`deleting ? t("common.saving") : t("common.delete")`;`common.deleting` 在 en/zh 两个 locale 均不存在(已验证);idp 页一律显示 `"…"` | 新增 `common.deleting`/`common.removing` 并统一删除确认按钮文案 |
| B6 | settings-audit.tsx:61–107 + 275–285 | 中 | **请求竞态无序列保护**。`load` 依赖 `[method, actor, status, t]`,防抖 250ms 后触发(94–107);慢的旧响应可晚于新响应返回并覆盖结果;「Load more」`setLogs(prev => [...prev, ...res])`(70–71)与并发刷新交错时会拼出「旧筛选条件下的第二页」混合列表。无 AbortController、无 requestId | `load` 加请求序号(ref 自增)或 AbortController;append 前校验序号一致 |
| B7 | settings-mcp-servers.tsx:166–168(初始 tab)、187–229(load)+ 225(依赖 `[isAdmin, t]`) | 中 | **isAdmin 翻转引发的双载竞态 + 初始 tab 陈旧**。`useState<McpTab>(isAdmin ? "workspace" : "my")` 只在首渲染求值,会话/权限晚到时管理员初始落在 "my";随后 isAdmin 变化 → `load` 身份变更 → effect 重跑 → 旧 load(非 admin 分支)与新 load(admin 分支)并发,两次 `setWorkspaceServers` 到达顺序无保证 | tab 初始值改为 effect 派生;load 加序号保护(与 B6 同一基础设施) |
| B8 | settings-iam.tsx:148 `fetchUsers({ pageSize: 1000 })` + stores/user.ts:49–55 | 中 | **借 fetchUsers 的副作用改写全局 users 切片且静默失败**:catch 分支 `set({ users: [] })` 无任何提示——失败时 iam 绑定成员全部退化为原始 resource name 且无错误指示;同时会**清空其它页面共享的 users 缓存**(user-list/roster 正在展示的全局数组被替换/清空) | 绑定成员解析走本地一次性 fetch 或 directory store,不写全局 users;失败要 toast |
| B9 | settings-mcp-servers.tsx / settings-api-providers.tsx(CRUD 后仅 `load()` 本页)vs agent-mcp.tsx:87、agent-profile.tsx:342、machine-profile.tsx:237 | 中 | **设置页写操作后不失效 stores 的 `mcpServers`/`apiProviders` 切片** → agent/machine 编辑表单下拉显示陈旧列表,直到它们自身 silent 轮询才自愈 | CRUD 成功后调用对应 store refresh;或经 directory store 统一失效 |
| B10 | settings-groups.tsx:278–284、203–204 | 低 | ① updateMask 脏判断用 `JSON.stringify(members)` 深比较:顺序敏感,重排成员即触发全量 members 写;② `hasOwner` 只看 `role === OWNER`,**member 为空串的占位行也算 owner**,可绕过「至少一个 owner」校验 | 用 set 语义比较(按 member 聚合);owner 校验同时校验 member 非空 |
| B11 | settings-groups.tsx:154–177(refsByGroup)+ save()/remove() | 低 | `refsByGroup` 引用缓存**编辑后不失效**:改组成员后,已展开的 references 仍是旧数据直到刷新;删除组后 map 残留(无害但脏) | save()/remove() 时 `refsByGroup.delete(name)` |
| B12 | settings-audit.tsx:30–36(buildFilter) | 低 | 用户输入直接拼进 `method = "${method}"` 的 CEL 字符串,输入含引号即可构造任意表达式——鉴权在服务端、风险低,但属注入面,且引号会导致难懂的 400 | 转义 `"`/`\`,或改后端结构化参数 |
| B13 | settings-profile.tsx:128–140 | 低 | 表单种子 effect 依赖 `[currentUser]`,注释自认「唯一变动来自本页保存」——但 human-detail.tsx:109 证明其它页面也会 `fetchCurrentUser`,stores/auth.ts:130 的会话刷新同样触发;切页返回时进行中的编辑可能被吞。注释是脆弱契约 | 种子逻辑改为「仅当表单未脏时回填」(保存 saved 副本比较),或 openEntityRef 锁定 |
| B14 | settings-mcp-servers.tsx:506–509 | 低 | edit 关闭即 `setEditTarget(null)`,200ms 关闭动画中标题显示 "Edit MCP server: "(空)——正是 frontend/AGENTS.md 明文警告的反模式 | 采用 AGENTS.md 的 openEntityRef 冻结模式(§7 ResourceSheet 内置解决) |
| B15 | settings-iam.tsx:724–729 | 低 | 死分支:`(memberLabel(a) ?? a).localeCompare(memberLabel(b) ?? b)`——memberLabel 永不返回 nullish,`?? a` 是无效防御,误导读者 | 删除 `?? a` |
| B16 | settings-identity-providers.tsx:209、245(`as never`)、164(`authStyle: 1` 裸数字) | 低 | `config: buildOAuthConfig(...) as never` 抹掉 proto 类型检查;魔法值内联 | 用生成的 `Oauth2ConfigSchema.create(...)` + `AuthStyle.IN_PARAMS`,删 `as never` |

**反向确认(排除项)**:settings-* 14 个文件内**没有**任何 `eslint-disable` 依赖数组黑洞(全部 grep 为零;黑洞集中在 agent-profile/machine-profile/chat 域);没有遗留定时器/监听器;settings-profile 的 notification effect 正确使用 `cancelled` 旗标(143–182),是本组最规范的 effect 样板。

---

## 4. 性能问题

整体判断:低频管理页,无致命性能问题;真问题是**重复拉取 + 无缓存**,及若干 O(n²) 小热点。

| # | 位置 | 严重度 | 问题 | 建议 |
|---|---|---|---|---|
| P1 | `pageSize: 1000` × 17 处(全 settings 源文件) | 中 | users/groups/roles/providers 每进一页全量重拉,无 TTL、无去重(roles→iam→groups 连续导航 = 重复 listUsers/listGroups ×3) | directory store 按「会话 + 资源」缓存,提供 `invalidate('users')`;默认拉满并缓存 |
| P2 | settings-iam.tsx:731(users.find / 成员行)、833(group.members 循环内 users.find) | 低 | 行内 `users.find` O(n)/行嵌套于成员渲染,总 O(n·m);同文件 169–184 明明已建 Map 却未复用 | 下沉/复用 `userByName`、`groupMap` Map |
| P3 | 全组 | — | **未发现整体订阅问题**:grep 证明没有任何 settings 页使用无选择器 `useAppStore()`;`useHasPermission` 返回布尔、粒度正确(stores/permissions.ts:15–19)。值得肯定 | 保持;抽象层内继续强制 selector 化 |
| P4 | components/member-picker.tsx:83–107 | 低 | 1000 用户无虚拟化直接渲染(max-h-64 滚动);搜索 keystroke 重算 memo,当前量级可接受 | 用户量增长后接 ui/combobox 的 portal 化+虚拟列表能力 |
| P5 | iam 120–152/440–442;roles 118–121/288–290;idp 141–144/428–431 | 中(体验) | 每次写操作后 `load()` 重新 `setLoading(true)`,把整张表替换为 `<PageLoading/>`(iam)或整块替换为旋转文案(idp),即「保存一次闪屏一次」 | loading 拆分:`initialLoading`(全页)与 `refreshing`(静默,表保留) |

---

## 5. 死代码 / 历史债务(grep 已验证,非臆断)

| # | 位置 | 严重度 | 结论 | 验证方式 |
|---|---|---|---|---|
| X1 | locales:`settings.agentTeams.create-description`、`settings.agentTeams.create-failed`、`sidebar.settings-agent-teams` | 低 | **确认未使用**:en/zh 各 1,324 key、无互缺;以上 3 个 settings 系 key 全仓无 `t("...")` 引用(agent-teams-manager.tsx:53,83,92 等使用的是其它 `settings.agentTeams.*` key)。注:activity/reminders/tasks 的 `filter-*` key 表面「未匹配」实为模板字符串动态构造(reminder-list.tsx:158、command-list.tsx:154、activity-list.tsx:255),**不是**死 key | 全量 key × 源码引用正则交叉扫描 + 动态构造模式人工复核 |
| X2 | settings-identity-providers.tsx | 低 | **唯一没有测试的设置页**(`ls settings-identity-providers*` 仅一个文件),而它恰是含 secret 回填/掩码语义、最易回归的表单 | 目录列举 |
| X3 | settings-smtp.tsx(全文 226 行)vs settings-storage.tsx(全文 236 行) | 低 | 两页约 90% 同构:仅字段与 schema 不同,连同各自 `Field`、`isMasked`、EMPTY 表与保存回填逻辑,是历史复制;后端掩码语义一致(`****` 前缀 = 不修改,smtp 31–35 / storage 33–37 注释),抽象无阻碍 | 双文件对照 |
| X4 | settings-general.tsx:63–73 vs 91–102;116–194 | 低 | profile→form 映射在 load effect 与 `applyProfile` 各写一遍;4 个 toggle handler(Signup/EmailVerification/UserCreateMachine/Domain)是同一段 19 行代码 × 4 的抄写,各配独立 `savingXxx` state | 同文件对照 |
| X5 | settings-iam.tsx:68–72 注释 | 低 | `NON_GRANTABLE_WORKSPACE_ROLE_IDS` 是为历史角色(chat-membership 标记、已删除的 agentEditor/reviewer)保留的防御代码,现仅剩 workspaceMember 一项。可保留,但建议注释标注「可随后端清理删除」 | 注释自述 |
| X6 | settings-agents.tsx:318 `text-amber-600 dark:text-amber-400` | 低 | 违反 frontend/AGENTS.md「禁用裸色值」规范(应 `text-warning` 语义 token) | 规范比对 |

---

## 6. 本模块重构优先级清单(按性价比排序)

| 优先级 | 事项 | 性价比 | 预估成本 | 收益 |
|---|---|---|---|---|
| **P0-1** | 修 B1(header 动态 key 失焦)、B3(`common.no-data` 文案)、B5(补 `common.deleting`) | 极高 | 0.5 天内 | 3 个用户可见 bug 直接消除 |
| **P0-2** | 修 B2(ProviderSheet open 时重置 fetchKey/models/fetchError,顺带 B4 清 editTarget) | 高 | 0.5 天 | 消除跨提供商数据污染这类「静默损坏」级风险 |
| **P0-3** | 错误出口统一 `describeError`(D6),收敛 26 处裸 message | 高 | 0.5 天 | 权限拒绝有可读文案;为后续抽象定基调 |
| **P1-1** | 抽 `useResourceList`(请求序号 + AbortController + initialLoading/refreshing 分离)替换 7 页 load,根治 B6/B7/P5 | 高 | 1–2 天 | 竞态类整类消灭;每页 -30~40 行 |
| **P1-2** | 抽 directory store(users/groups/roles 会话级缓存 + `invalidate()`) | 高 | 1–2 天 | 跨页重复请求消失;B8/B9 收口 |
| **P1-3** | 抽 `<ConfirmDeleteDialog>` + `lib/members.ts`(memberLabel/displayName/isMasked 下沉) | 高 | 1 天 | 6 处 AlertDialog + 4 处 memberLabel + 2 处 isMasked 一次清空(约 -250 行) |
| **P2** | `useCrudDialog` + `<ResourceSheet>` + `<ResourceTable>` 脚手架落地,按 groups → roles → api-providers → mcp-servers → idp → iam(含 etag 特化)顺序逐页迁移,每页单独 PR + 测试随迁 | 中高 | 1.5–2 周 | -2,000~2,600 行;表单校验/脏检查/close 动画模板一次性做对 |
| **P2-b** | profile/smtp/storage 换挂 `SettingsPage`;smtp+storage 合并为 SecretConfigForm(X3) | 中 | 1 天 | 框架统一;两页 -150 行 |
| **P3** | audit 竞态序号 + CEL 转义(B6/B12);idp 删 `as never`(B16);补 identity-providers 测试(X2);死 i18n key 清理(X1);`amber-600` → `text-warning`(X6) | 低 | 1–2 天 | 尾部归零 |

---

## 7. 统一抽象方案草案

### 7.1 目录结构

```
src/features/settings/                # 或维持 pages/dashboard/settings-*.tsx 薄壳也可
  shared/
    use-resource-list.ts       # 列表获取:请求序号防竞态、initialLoading vs refreshing、错误转 toast(descibeError)、可选缓存 key
    use-crud-dialog.ts         # create/edit/delete 状态机:open/target/form/submitting;open 时重置;close 动画期实体冻结(openEntityRef)
    confirm-delete-dialog.tsx  # 统一删除确认(common.deleting 文案、submitting 状态、destructive 按钮)
    resource-sheet.tsx         # 外层壳(openEntityRef 冻结实体 + key remount)+ Header/Body/Footer 模板 + isDirty 门控 Update
    resource-table.tsx         # 列配置 + 空态(colSpan 自动计算)+ refreshing 静默刷新
    member-editor.tsx          # 成员徽章 + × + MemberPicker(mcp/api-providers/iam 三处收编)
    secret-helpers.ts          # isMasked 与「掩码回传 = 不修改」语义
  lib/
    members.ts                 # memberLabel(member, users, groups) 唯一实现(iam/mcp/api-providers/machine-profile 收编)
    slug.ts                    # slugify 唯一实现(roles/idp 收编)
  store:
    stores/directory.ts        # users + groups + roles 会话级缓存,TTL + invalidate(resource)
    stores/refresh.ts          # 统一 post-mutate 失效入口(替代各页散落的 load())
```

### 7.2 核心 Hook API(草案)

```ts
// use-resource-list.ts —— 权限门 + 防竞态列表获取
const list = useResourceList<Role, ListRolesResponse>({
  enabled: canList,                    // 未授权时脚手架渲染 PermissionNotice
  fetch: (signal) => roleServiceClient.listRoles({}, { signal }),
  toItems: (res) => res.roles ?? [],
  failureKey: "settings.roles.load-failed",
});
// list = { items, initialLoading, refreshing, reload(), lastError }

// use-crud-dialog.ts —— CRUD 抽屉/确认框状态机
const crud = useCrudDialog<Role, RoleForm>({
  toForm: roleToForm,
  emptyForm,
  create: (form) => roleServiceClient.createRole({ role: {...} }),
  update: (target, form) => roleServiceClient.updateRole({ ... , updateMask }),
  remove: (target) => roleServiceClient.deleteRole({ name: target.name }),
  done: () => { list.reload(); directory.invalidate("roles"); },
});
// crud = { createOpen, editTarget, editOpen, deleteTarget, deleteOpen,
//          openCreate(), openEdit(role), openDelete(role), close(), submitting… }
```

页面剩余物 = 列定义 + `<RoleForm>`(校验函数返回 `Record<field, string>`)+ 业务特化(iam 的 etag 乐观锁、mcp 的三 tab、groups 的懒加载 references、provider 的模型抓取)。单页预计从 600–900 行降到 250–400 行。

### 7.3 store 边界

- **CRUD 资源**(roles/groups/iam/api-providers/mcp-servers/idp):读走 `useResourceList`(页级)或 directory(全局资源 users/groups/roles);写永远经 store 的 mutation/refresh 入口,使 `stores/mcp.ts`、`stores/api-provider.ts` 与设置页共享同一真相(agent/machine 表单自动一致,根治 B9)。
- **配置型 setting**(smtp/s3/general/agents 的 userMcpConfig):保留 `stores/setting.ts` 现有 `fetch/update + paths` 模式——该层设计是好的;仅需把 general 页 4 个重复 toggle 收敛为 `useOptimisticToggle(patch, paths)`(乐观更新 + 失败回滚,现有 handler 已内联该语义)。
- **鉴权**:页面级 `canList/canCreate/...` 由 hook 接收,`PermissionNotice` 由脚手架统一渲染。

### 7.4 迁移路径(5 步,每步可独立合入)

1. **基建先行**:落 `shared/` 四原语(useResourceList、ConfirmDeleteDialog、lib/members.ts、lib/slug.ts)+ directory store;不迁移任何页面;现有测试全绿。
2. **先迁 groups**(无特殊交互,但已有懒加载 references 需要保留):验证 ResourceTable + ResourceSheet + useCrudDialog 组合;settings-groups.test.tsx 改造为迁移范本。
3. **模板迁移** roles → api-providers → mcp-servers → idp(api-providers/mcp-servers 携带 MemberEditor);每页一个 commit;迁移中顺手修掉 B1、B2、B4、B5。
4. **iam 特化迁移**:保留 etag 冲突重试语义,把 `useIamPolicy` 下沉为页级 hook;同时修 B8(不再写全局 users 切片)。
5. **尾部统一**:profile/smtp/storage 挂 SettingsPage + 收敛 `Field`;删除死 i18n key(X1)与注释级债务;补 idp 测试与 `useResourceList` 竞态单测。

> **是否引 react-query?** 不必。本模块痛点是「无序、无缓存一致性」而非「缺缓存策略」;一个 ~60 行的 `useResourceList` + directory 失效入口即可与现有 Zustand 栈对齐。若未来更多模块需要,再整体评估,不建议只为这组页面单点引入。

---

## 附:本报告证据的可复核命令

```bash
# 重复 helper 计数
cd frontend/src && grep -rn "function memberLabel" --include="*.tsx" .          # 4 处(含 machine-profile)
grep -rn "err instanceof Error ? err.message : String(err)" pages/dashboard/settings-*.tsx | wc -l   # 26
grep -rn "describeError" pages/dashboard/settings-*.tsx | wc -l                  # 33
grep -rn "pageSize: 1000" pages/dashboard/settings-*.tsx | wc -l                 # 17

# i18n 死 key(对 en-US 全量 key 与源码引用交叉扫描):
#   settings.agentTeams.create-description / settings.agentTeams.create-failed / sidebar.settings-agent-teams 未使用;
#   en=zh=1324 两侧无缺失。

# 结构锚点(每页 load/create/save/remove/AlertDialog 行号见 §1.1)
grep -n "const load = useCallback\|const create = \|const save = \|const remove = " frontend/src/pages/dashboard/settings-*.tsx
```