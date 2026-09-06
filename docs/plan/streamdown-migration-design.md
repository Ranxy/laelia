# 前端 Markdown 渲染栈迁移至 Streamdown 设计与分布式执行方案

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：Streamdown 迁移已全部实施完成（落地提交 `599c92c`），本文档由"迁移设计方案"改写为"已实现结果记录"，旧栈 `markstream-react` / `stream-markdown` 已删除。

## 1. 文档状态

- 状态：**已实施完成**。原 P0–P9 方案已执行完毕，本文档已改写为对当前代码的事实描述；与代码冲突时以代码为准
- 结果：前端的 `markstream-react`、`stream-markdown` 已删除，Markdown 渲染统一由 `streamdown` 承担
- 实际依赖版本：`streamdown@2.6.0` + `@streamdown/code@1.1.1`（精确版本，见 `frontend/package.json`）
- 落地提交：`599c92c chore: migrate markdown parser to streamdown`
- 适用范围：`frontend/` 中所有 Markdown 渲染、代码块、mention、Markdown 文件预览和相关测试
- 关联文档：`docs/plan/markdown-preview-outline-comments.md`
- 原迁移原则（适配层优先、渲染器决策优先于关联文档中"继续使用 markstream-react"的历史决策）已按计划落地

## 2. 摘要（已实现形态）

当前 Markdown 渲染链路负责聊天消息、历史消息懒加载、代码块、`@mention` 自定义节点、Markdown 文件目录与锚点、Workspace 预览、Command 输出和 Final Summary，形态为：

```text
业务组件
    ↓
项目 Markdown 适配层（frontend/src/lib/markdown.tsx）
    ↓
Streamdown 2.6.0 + @streamdown/code 1.1.1
```

业务组件只 import `@/lib/markdown`，不直接 import `streamdown`。第三方 API、代码块实现、安全策略、翻译文案和未来的渲染器替换都限制在适配层内。

原方案的 Phase 划分仅作为执行历史保留（见第 5–7 节）；以下各节描述的是当前实现，不再是计划。

## 3. 当前实现盘点

### 3.1 依赖

`frontend/package.json` 当前包含：

```json
{
  "streamdown": "2.6.0",
  "@streamdown/code": "1.1.1"
}
```

`markstream-react` 与 `stream-markdown` 已从依赖（`frontend/package.json`、`frontend/pnpm-lock.yaml`）和全仓代码中删除，`frontend/src` 中已无任何 `markstream` 残留。

实际使用的 Streamdown 2.6.0 API（已对照 `node_modules/streamdown/dist/index.d.ts` 核实）：

- `Streamdown` React 组件；
- `mode="static"` / `mode="streaming"`；
- `isAnimating`；
- `components`；
- `remarkPlugins` / `rehypePlugins`（`remarkPlugins` 已标注 deprecated，官方推荐 `remarkPluginsBefore` / `remarkPluginsAfter`）；
- `plugins`；
- `allowedTags`；
- `literalTagContent`；
- `controls`；
- `lineNumbers`；
- `linkSafety`；
- `parseIncompleteMarkdown`；
- `animated`；
- `codeBlockMaxHeight`；
- `translations`；
- `streamdown/styles.css`。

### 3.2 生产代码入口

| 文件 | 当前用途 |
|---|---|
| `frontend/src/lib/markdown.tsx` | 适配层：`MarkdownRenderer` / `FinalSummary` / `MarkdownVariant`，Streamdown 调用、mention 与 code 插件配置 |
| `frontend/src/components/chat/message-row.tsx` | 聊天 Markdown：`MemoMarkdown` → `LazyMarkdown` → `MarkdownRenderer variant="chat"` |
| `frontend/src/components/chat/lazy-markdown.tsx` | 历史消息延迟渲染（业务级懒加载，保留） |
| `frontend/src/components/preview/markdown-preview-overlay.tsx` | Markdown 文件全屏预览：`variant="preview"`，`contentRef` + `buildOutline()` + `scrollToAnchorId` |
| `frontend/src/components/workspace/workspace-file-panel.tsx` | Workspace Markdown：`variant="workspace"` |
| `frontend/src/components/command-events/command-event-inspector.tsx` | Command 输出预览：`variant="command"` |
| `frontend/src/pages/dashboard/command-list.tsx` / `command-detail.tsx` | Final Summary：`<FinalSummary content={...} />` |
| `frontend/src/main.tsx` | 引入 `streamdown/styles.css` |
| `frontend/src/assets/css/tailwind.css` | `.markdown-content` 项目级样式；`@source` 引入 streamdown 与 `@streamdown/code` 的 dist |
| `frontend/vite.config.ts` | `manualChunks` 将 streamdown / `@streamdown/code` 归入懒加载 Markdown chunk |

### 3.3 当前业务行为

#### 聊天消息

`MessageRow` 使用 `MemoMarkdown` 和 `LazyMarkdown`：

- off-screen 历史消息先显示纯文本 fallback；
- 进入 `IntersectionObserver`（root margin `600px 0px`）后才渲染 Markdown；观察器注册延迟一帧，让"滚动到底部"的 effect 先执行；
- `eager` 用于当前或小规模会话；
- fallback 期间将消息行 `overflow-anchor` 置为 `none`，替换为真实 Markdown 后延迟两帧恢复，避免滚动跳动；
- 流式渲染管线已退役（`message-row.tsx` 注释原文："The streaming pipeline is retired"），行内始终渲染已 committed 的内容，因此所有聊天 Markdown 均使用 Streamdown 静态模式（当前代码中没有任何 `mode="streaming"` 调用方）。

#### Mention

`frontend/src/components/chat/mentions.ts` 的 `mentionTagMarkdown()` 将文本改写为自定义标签（`label` 为可选的显示名属性）：

```html
<mention type="user" id="users/alice" name="alice" label="...">@alice</mention>
```

适配层通过 `allowedTags={{ mention: ["type", "id", "name", "label"] }}`、`literalTagContent=["mention"]`、`components={ mention: MentionChip }` 注入渲染，仅在 `mentionAware` 时启用。`MentionChip` 渲染 `data-mtype` / `data-mid` / `data-mname`，支持键盘 Enter/Space 激活；`message-row.tsx` 通过 `closest("[data-mtype]")` 做事件委托点击。`contentWithMentionTags()` 的文本改写协议与迁移前保持一致。

#### Markdown 文件目录和评论

`frontend/src/lib/markdown-file.ts` 的 `buildOutline()` 在渲染后的 DOM 上扫描 `h1` 到 `h6`，自己分配：

```text
md-${i}-${slugify(text)}
```

并负责目录编号、评论 section anchor 和跨场景跳转。heading id 不依赖 Streamdown 自动生成；`buildOutline()` 仍是唯一的项目 ID 分配入口（`markdown-preview-overlay.tsx` 消费）。

## 4. 已实现适配层 API

适配层位于 `frontend/src/lib/markdown.tsx`，与原方案推荐的 API 一致：

```tsx
export type MarkdownVariant =
  | "chat"
  | "preview"
  | "workspace"
  | "command"
  | "summary";

export interface MarkdownRendererProps {
  content: string;
  variant?: MarkdownVariant;
  className?: string;
  mentionAware?: boolean;
  mode?: "static" | "streaming";
  isAnimating?: boolean;
}

export function MarkdownRenderer(
  props: MarkdownRendererProps
): React.ReactElement;

export function FinalSummary(props: {
  content: string;
  className?: string;
}): React.ReactElement;
```

适配层当前配置（代码为准）：

- `plugins: { code }`（`@streamdown/code`）；
- `controls`: `code` 开启 copy、关闭 download；`table` 显式关闭 `fullscreen` —— Streamdown 的表格全屏控件会 portal 到 `document.body` 的 z-50 层，落在应用 z-2500 预览层之下（见 `markdown.tsx` 内注释）；
- `lineNumbers: false`；
- 自定义 `translations`（`copyCode` / `copied` / `downloadFile`，英文字面量 —— Streamdown 文案不经过项目 i18n 体系）；
- variant 到 className 的映射：`chat` 为 `markdown-content markdown-content-chat`，其余为 `markdown-content`；
- mention 相关 props 仅在 `mentionAware` 时传入；
- `isAnimating` 仅在 `mode === "streaming"` 时透传给 Streamdown。

不存在全局 custom component registry（旧 `setCustomComponents` 已删除），mention 通过每次调用的 props 注入。旧的 markstream 专属 props（`customId`、`final`、`fade`、`smoothStreaming`、`batchRendering`、`deferNodesUntilVisible`、`customHtmlTags`）已全部从业务代码中移除。

### 4.3 场景默认值（实际生效）

| 场景 | `mode` | 动画 | mention |
|---|---|---|---|
| chat / preview / workspace / command / summary | `static`（默认值，当前无调用方传 `streaming`） | 关闭 | 仅 chat 按调用者开启（`mentionAware`） |

## 5. 执行规则与文件所有权（已实施，仅存历史）

原 §5 的执行者边界与文件所有权建议已按计划执行完毕，无后续操作意义，折叠为一条记录：

- 适配层收敛在 `frontend/src/lib/markdown.tsx`；
- 依赖与 lockfile（`frontend/package.json`、`frontend/pnpm-lock.yaml`）由集成阶段统一变更。

## 6. Phase 详细计划（已全部实施）

原 P0–P9 的详细计划已执行完毕，本节折叠为各 Phase 的落地记录与差异说明。

- **P0 基线**：只读阶段，无产物保留要求。
- **P1 API Spike**：spike 为临时产物，未保留；结论直接落入适配层实现。
- **P2 适配层**：已实现于 `frontend/src/lib/markdown.tsx`（见 §4）。
- **P3 回归测试**：与原计划有一处差异 —— 独立 fixture 目录 `frontend/src/components/markdown/fixtures/` **未创建**（该目录不存在）；回归验证由 colocated 测试承担：`frontend/src/lib/markdown.test.ts`（静态渲染 + mention 路径两条用例）、`frontend/src/components/chat/message-row.test.tsx`（mock renderer 下校验 mention 改写协议）等。统一 mock 约定已落地：页面级测试通过 `vi.mock("@/lib/markdown")` 将 `MarkdownRenderer` mock 为直出 content，见 `message-row.test.tsx`、`chat-composer.test.tsx`、`thread-panel.test.tsx`、`message-row.image-visibility.test.tsx`、`command-event-inspector.test.tsx`、`src/pages/dashboard/chat.test.tsx`、`src/pages/dashboard/chat-conversation.test.tsx`。
- **P4 mention 适配**：已实现（见 §3.3 Mention），属性契约 `data-mtype` / `data-mid` / `data-mname`、键盘激活、事件委托均保留。
- **P5 代码块适配**：采用官方 `@streamdown/code` 插件，`controls.code = { copy: true, download: false }`，`lineNumbers: false`，未自研代码块组件。
- **P6 聊天消息迁移**：`message-row.tsx` 已改用适配层（`variant="chat"`），`MemoMarkdown` / `LazyMarkdown` / eager 分层 / fallback / overflow-anchor / mention 事件委托 / Copy Markdown（`MessageContextMenu` + `onCopyMarkdown`，复制原始 `msg.content`）全部保留。
- **P7 静态场景迁移**：preview（`variant="preview"`，保留 `contentRef` / `buildOutline()` / `scrollToAnchorId` / comments aside）、workspace（`variant="workspace"`）、command inspector（`variant="command"`）、Final Summary（`command-list.tsx` / `command-detail.tsx` 继续调用 `<FinalSummary content={...} />`，外部 API 未变）均已完成。
- **P8 CSS / 构建 / 性能 / 安全**：`main.tsx` 引入 `streamdown/styles.css`（旧 `markstream-react/index.css` import 已删除）；`tailwind.css` 顶部加入 `@source "../../../node_modules/streamdown/dist/*.js"` 与 `@source "../../../node_modules/@streamdown/code/dist/*.js"`，`.markstream-chat` 样式改为项目级 `.markdown-content`；`vite.config.ts` 的 `manualChunks` 将 streamdown / `@streamdown/code` 归入懒加载 Markdown chunk 并留有说明注释。
- **P9 删除旧栈**：已完成。`rg 'markstream|stream-markdown|setCustomComponents|MarkdownCodeBlockNode'` 在 `frontend/src`、`frontend/package.json`、`frontend/pnpm-lock.yaml` 中零匹配。

## 7. 调度与人员配置（历史，已折叠）

原 §7 的 Phase 依赖调度表、最小人员配置和跨工作包接口建议仅具执行历史意义；接口均已固化在 `frontend/src/lib/markdown.tsx`（`MarkdownRendererProps`、`MarkdownVariant`、`mentionAware` 语义、static/streaming 默认策略），无需再次协调。

## 8. 完成标准核对（当前状态）

### 8.1 依赖和代码清理 — 全部达成

- [x] 生产代码不再 import `markstream-react` / `stream-markdown`。
- [x] 测试统一 mock `@/lib/markdown` 适配层，不再 mock 第三方渲染库。
- [x] 不存在 `setCustomComponents`、`MarkdownCodeBlockNode`。
- [x] 业务组件只依赖 `@/lib/markdown` 适配层。
- [x] `.markstream-chat` 与 markstream 旧注释已清理（现为 `.markdown-content`）。

### 8.2 功能 — 已实现（以现有实现与测试为准）

- [x] chat Markdown 正常（`message-row.tsx` → 适配层）。
- [x] channel / thread mention 正常（`mentionAware` 路径 + `mention-badge` / 事件委托）。
- [x] 代码块高亮和复制正常（`@streamdown/code`，copy 开启）。
- [x] Markdown 文件预览、Workspace Markdown、Command 输出预览、Final Summary 正常（见 §3.2 各入口）。
- [x] 标题目录、heading ID（`md-${i}-${slug}`）、评论锚点正常（`buildOutline()` 保留）。

### 8.3 性能 — 已按方案落地

- [x] 聊天使用 static 模式；`LazyMarkdown` 业务级懒加载保留。
- [x] Streamdown 与 code 插件经 `manualChunks` 归入懒加载 Markdown chunk，不进初始入口（`vite.config.ts` 注释与实现）。
- [x] fallback 替换与历史 prepend 的滚动稳定由 `overflow-anchor` 策略保障（`lazy-markdown.tsx`、`use-message-scroller.ts`）。

## 9. 风险清单（已处置记录）

原风险及处置结果：

- **Mention 被 sanitize 丢失**：已通过 `allowedTags` + `literalTagContent` + `markdown.test.ts` mention 路径用例覆盖，未放开任意 HTML。
- **标题 DOM 或 ID 行为变化**：`buildOutline()` 仍自行分配 `md-${i}-${slug}`，未依赖 Streamdown 的 slug 规则。
- **代码插件 bundle / 性能回退**：`@streamdown/code` 与 streamdown 由 `manualChunks` 归入懒加载 chunk；`LazyMarkdown` 保留。
- **CSS 视觉变化**：统一为项目级 `.markdown-content` + `streamdown/styles.css`。
- **实施中发现并处置的新问题**：Streamdown 表格全屏控件 portal 层级低于应用预览层 → 适配层显式 `controls.table.fullscreen: false`（保留表格复制控件）。
- **已消除的历史中风险**：未闭合 Markdown 中间态、复制尾部换行、`rehype-raw` 行为差异等均随旧栈删除而终结；当前以 Streamdown 2.6.0 实际行为为准。

## 10. 提交记录（历史）

迁移以单一提交落地：`599c92c chore: migrate markdown parser to streamdown`。原 §10 的提交拆分建议已无操作意义，折叠保留。

## 11. 最终决策（已全部落地）

1. Streamdown 已是唯一前端 Markdown 渲染栈。
2. 业务组件不直接依赖 Streamdown，统一通过 `frontend/src/lib/markdown.tsx` 适配层调用。
3. `LazyMarkdown` 保留，继续解决聊天列表的业务级渲染门控和滚动稳定性问题。
4. `mentions.ts` 的匹配和文本改写协议保持不变，渲染迁移通过 `allowedTags`、`literalTagContent`、`components` 完成。
5. 代码块使用官方 `@streamdown/code` 插件，未自研代码块组件。
6. heading ID 仍由 `buildOutline()` 生成（`md-${i}-${slug}`），不依赖第三方 slug 规则。
7. 所有当前调用方均为 `mode="static"`（默认值）；streaming 模式能力保留在适配层 API 中备用。
8. 共享 CSS、Vite 分包与依赖变更已随迁移一次性完成。
9. 旧依赖 `markstream-react` / `stream-markdown` 已在迁移完成后删除，无回退路径残留。