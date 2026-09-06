# Markdown 文件预览 + Outline + 锚点评论 设计文档

> 状态:2026-09-06 已对照当前代码核对更新。主要变化:两期方案均已落地并经后续重构——markdown 渲染栈全仓从 markstream-react 换成 streamdown(`lib/markdown.tsx` 的 `MarkdownRenderer`,`variant="preview"`),三个预览浮层共享 `file-preview-shell.tsx` 外壳,评论 aside 抽成共享 `comments-panel.tsx`,跨场景锚点跳转(`scrollToAnchorId`/`scrollToQuote`)与图片预览浮层已实现。

## Context

当前前端对附件的处理只有「下载」一种（`FileCard` → `commandServiceClient.downloadFile` → blob 下载），没有任何预览。用户/agent 上传的 markdown 文件无法在产品内阅读，更无法就文档某一段内容展开讨论。

需求演化为三部分：

1. **专注阅读**：用一个**遮蔽整个页面**的浮层预览 markdown，渲染美观（与聊天消息中的 markdown 完全一致），不被输入框/thread 内容打断阅读节奏。
2. **Outline**：基于标题生成目录，点击跳转，方便长文阅读。
3. **锚点评论**：开启评论模式后右侧出现评论 aside；用户在文档中**框选文本**写评论，该评论连同**所框选的引用文本**与**所在段落锚点（§ X.Y 标题）**一起作为一条 thread 消息发送到「该附件所属消息」的 thread 里。评论既在浮层的评论 aside 中以紧凑卡片展示，也在常规 thread 面板中作为结构化消息展示。

文件过大（>10MB）不支持预览，需提示用户改为下载。

## 关键设计决策（已与用户确认）

1. **渲染栈**：设计时复用当时的 `markstream-react`（此决策为历史记录；全仓此后已迁移到 `streamdown`，见下节说明）。
2. **预览形态**：遮蔽整个页面的全屏浮层（专注阅读），不再是早期设想的右侧 420px aside。
3. **评论落点**：评论作为 thread 回复发送到「附件所属消息」的 thread；该消息即 thread root（若附件本身挂在某条 thread 回复上，则回退到该回复的 `threadRoot`）。
4. **评论数据模型**：扩展 `Attachment` 增加锚点字段，而非新增顶层 `AttachmentComment` 消息字段——附件已 JSON 序列化进 BYTEA，**无需 DB 迁移**。
5. **大小阈值**：10MB，依据 `attachment.sizeBytes` 在打开预览前判定，过大则禁用预览入口并提示下载。

## 关键决策：复用 `markstream-react`（历史记录，已被取代）

> **2026-09-06 更新**：该决策已失效——全仓 markdown 渲染已从 `markstream-react` 迁移到 **`streamdown`**。当前现状：
> - `frontend/package.json` 依赖 `streamdown`（2.6.0）+ `@streamdown/code`，`main.tsx` 全局引入 `streamdown/styles.css`；`markstream-react` 已不在依赖中。
> - 唯一渲染入口是 `frontend/src/lib/markdown.tsx` 的 `<MarkdownRenderer>`（`variant: "chat" | "preview" | "workspace" | "command" | "summary"`，内部包 `streamdown` 的 `Streamdown` 并带 mention chip 支持）。
> - `.markstream-chat` 作用域样式已从 `tailwind.css` 移除，现由 `.markdown-content`（chat 变体再加 `markdown-content-chat`）承载样式。
> - 预览浮层以 `<MarkdownRenderer content={...} variant="preview" />` 渲染，外观与聊天消息一致。
>
> 以下为当时的调研记录，仅作背景。

调研结论：**继续使用 `markstream-react`，不引入第二个 markdown 栈。**

- 已在 bundle 中：`main.tsx` 全局引入 `markstream-react/index.css`，`src/lib/markdown.tsx` 已注册 Shiki 代码块自定义组件，`tailwind.css` 中已有 `.markstream-chat` 作用域样式（425–517 行）。
- 已被 `MessageRow`（聊天气泡）与 `FinalSummary`（静态文档）使用，**静态内容用 `final` flag 即可**（`FinalSummary` 已验证），预览复用后渲染外观与聊天消息 1:1 一致。
- 自带 `batchRendering` / `deferNodesUntilVisible` / `maxLiveNodes`，对接近 10MB 的大文档有虚拟化/分批渲染优势，是 react-markdown 没有的。
- 引入 react-markdown + remark/rehype 会重复一套语法高亮与样式，且预览外观与聊天不一致。零收益。

当时的渲染入口是默认导出的 `<MarkdownRender>`，预览中以 `customId="md-preview"`、`final`、`fade` 调用，包在 `.markstream-chat` wrapper 内复用既有样式。

## 数据模型：扩展 `Attachment`（无需 DB 迁移）

一条评论 = 一条 thread 回复（`ChatMessage` + `thread_root`），其 `attachments` 携带**一个锚点附件**：指向被评论的 markdown 文件，并附带段落锚点与引用文本。`content` 为评论文本。

`Attachment` 已被 JSON 序列化进 BYTEA 列（`store/chat_message.go` 的 `marshalAttachments`），**新增字段自动持久化，无需 schema 迁移**。这比新增一个 `AttachmentComment` 顶层消息字段（需要新增列、改 3 个请求结构）更高效，符合项目「少即是好、无迁移」的取向。

`proto/v1/v1/command.proto` — `Attachment` 新增三个可选字段，空值表示普通整文件附件（已落地，见该文件 `message Attachment`）:

```proto
message Attachment {
  string id = 1;
  string name = 2;
  string mime_type = 3;
  int64 size_bytes = 4;
  // 锚点引用字段：当本附件代表"对文件某一段的评论锚点"而非整文件上传时设置。
  // 普通整文件附件留空。
  string section_anchor = 5;  // 展示串，如 "§ 2.1 Server (server/)"
  string section_id = 6;      // 文件内标题的稳定 id，用于点击锚点跳转回预览
  string quoted_text = 7;     // 被框选的引用文本
}
```

`SendMessageRequest` / `PostMessageRequest` 无需改动 —— 锚点信息随 `attachments` 透传。

## 后端改动

（本节设计均已落地。）

### 1. `backend/manager/api/v1/channel_file_service.go` — `resolveAttachments`

当前重建 `Attachment` 时只回填 `Id/Name/MimeType/SizeBytes`，会**丢弃** caller 传入的锚点字段。改为在重建时**保留** `a.SectionAnchor / a.SectionId / a.QuotedText`（这三项是 caller 语义，文件行不是其来源）。文件元数据仍以文件行为准。（已实现：`resolveAttachments` 现为 `CommandService` 方法，约在 352 行，锚点字段保留在 ~380 行。）

```go
resolved = append(resolved, &v1pb.Attachment{
    Id:            f.ID.String(),
    Name:          f.OriginalName,
    MimeType:      f.MimeType,
    SizeBytes:     f.SizeBytes,
    SectionAnchor: a.SectionAnchor,
    SectionId:     a.SectionId,
    QuotedText:    a.QuotedText,
})
```

### 2. `backend/manager/api/v1/channel_message.go` — `SendMessage`

设计时 `SendMessage` 还在 `channel.go`（266–365）且**未**调用 `resolveAttachments`（只有 agent 路径 `PostMessage` 调用），用户附件 id 不经会话归属校验直接落库（既有安全缺口）。改为在持久化前调用 `resolveAttachments(ctx, convID, req.Msg.Attachments)`，把返回值传给 `CreateChatMessageBumpVersion` / `CreateTaskMessageBumpVersion` 的 `Attachments`。一举两得：既补上用户路径的文件归属校验，又让锚点字段被规范化保留。（已实现；此后 `SendMessage` 移到了独立文件 `backend/manager/api/v1/channel_message.go`，`resolveAttachments` 调用在其 88 行附近。）

```go
attachments, err := s.resolveAttachments(ctx, convID, req.Msg.Attachments)
if err != nil {
    return nil, err
}
// ... 把 attachments 传给 CreateChatMessageBumpVersion / CreateTaskMessageBumpVersion
```

### 3. store / 序列化

无需改动：`[]*v1pb.Attachment` 直接经 `store/chat_message.go` 的 `marshalAttachments` marshal 进 BYTEA；`storeToV1ChatMessage`（`backend/manager/api/v1/command_reaction.go:130`）透传 `Attachments`。新增字段随既有序列化路径自动往返。（已实现。）

改完按 CLAUDE.md：`buf format -w proto && buf lint proto && cd proto && buf generate`，再 `gofmt` / `golangci-lint run --allow-parallel-runners` / `go build`。

## 前端改动

### 新建 store slice — `frontend/src/stores/preview.ts`

（已实现；落地后 slice 又按 html 预览需求扩展了 `kind` 字段与跨场景跳转参数，现签名如下：）

```ts
interface PreviewSlice {
  activePreview: {
    kind: "markdown" | "html";  // 由 isHtmlAttachment 判定,决定渲染器与大小阈值
    conversation: string;       // "conversations/{id}"
    conversationId: string;     // bare id
    rootMessageId: string;      // 评论落点的 thread root（= 附件所属消息的 threadRoot ?? 自身 id）
    attachment: Attachment;     // 被预览的文件
    content: string;            // 解码后的 markdown 文本
    status: "loading" | "ready" | "error" | "too-large";
    scrollToAnchorId?: string;  // 跨场景锚点跳转(markdown=标题 DOM id;html="html:y:{y}")
    scrollToQuote?: string;     // html 跳转时配套的引用文本
    error?: string;
  } | null;
  openFilePreview(
    conversation: string,
    rootMessageId: string,
    attachment: Attachment,
    scrollToAnchorId?: string,
    scrollToQuote?: string
  ): Promise<void>;
  closeFilePreview(): void;
}
```

- `openFilePreview`：若 `(attachment.sizeBytes ?? 0n) > MAX_MARKDOWN_PREVIEW_BYTES`（`10n * 1024n * 1024n`，bigint）→ 直接 `status: "too-large"` 并打开浮层（浮层内显示「文件过大，不支持预览，请下载」），**不发起下载**。否则 `commandServiceClient.downloadFile({ id })` → `new TextDecoder().decode(data)` → `status: "ready"`。错误时 `status: "error"`。
- 在 `stores/index.ts` 注册 slice；slice 接口定义在 `stores/preview.ts` 本文件内（`stores/types.ts` 引入）。

### 新建工具 — `frontend/src/lib/markdown-file.ts`

（已实现，配套 `markdown-file.test.ts`。）

- `MAX_MARKDOWN_PREVIEW_BYTES = 10n * 1024n * 1024n`（bigint，与 `sizeBytes` 的 bigint 直接比较）。
- `isMarkdownAttachment(att)`：`/\.(md|markdown|mdx)$/i.test(name) || mimeType === "text/markdown" || mimeType === "text/x-markdown"`。
- `slugify(text)`：标题文本 → 安全 slug，用于 `id`。
- `buildOutline(container: HTMLElement)`：`container.querySelectorAll("h1,h2,h3,h4,h5,h6")` → 给每个标题分配 `id = md-${index}-${slug(text)}`；按层级计数器生成编号（`1`, `1.1`, `2`, `2.1`…，编号相对文档内最浅标题层级）；返回 `{ level, text, id, number }[]`（`OutlineItem`）。展示时用正则剥掉标题文本开头的既有编号 `^\d+(\.\d+)*\s+`，避免 `§ 2.1 2.1 Server`。
- `anchorForSelection(container, selection, outline)`：落地签名多一个 `outline` 参数（须为同一容器的 `buildOutline` 结果，用其按 DOM id 反查编号/文本）；用 `compareDocumentPosition` 在 `container` 内找到选区之前的最近标题，返回 `{ sectionId, sectionAnchor: "§ {number} {text}", quotedText }`；`quotedText` trim 后截断到 500 字；选区折叠、在容器外、或位于第一个标题之前时返回 `null`。

### `frontend/src/components/chat/file-card.tsx`

新增可选 props：`onPreview?: () => void`、`previewDisabledReason?: string`。

- 当 `onPreview` 存在：卡片主体点击 → `onPreview()`；右侧保留一个独立的小下载图标按钮（`Download`）走原 `handleDownload`。`previewDisabledReason` 存在时主体禁用并以此作为 tooltip，提示「文件过大（>10MB），不支持预览，请下载」。
- `onPreview` 不存在（非 markdown 或上层未接线）：行为完全不变（整卡点击下载），保持对非 markdown 附件的向后兼容。

### `frontend/src/components/chat/message-row.tsx` + `thread-panel.tsx`

新增 prop `onPreviewAttachment?: (att: Attachment, rootMessageId: string) => void`。

- `message-row.tsx`：把 `msg.threadRoot ?? msg.id` 作为 rootMessageId 传给回调；在附件 map 中分支：
  - `att.sectionAnchor` 非空 → 渲染 `<AttachmentCommentCard variant="inline">`（评论作为 thread 回复的结构化展示，`onJumpToSection(att, sectionId, rootId)`）。
  - 否则 `<FileCard onPreview={isMarkdownAttachment(att) || isHtmlAttachment(att) ? () => onPreviewAttachment(att, effRoot) : undefined} previewDisabledReason={对应类型超阈值 ? "…" : undefined} />`（html 分支与阈值随 html 预览方案加入）。
- `thread-panel.tsx`：以 `onPreviewAttachment` prop 直接下传（root 与 replies 的 `MessageRow` 各自接线；设计稿里的 `RootContext` 已不存在）。另接 `onJumpToSection` 与 `onPreviewImage`。
- `chat-conversation.tsx`（`handlePreviewAttachment` 约 680 行）：`openFilePreview(conversationName, rootId, att)`，连同 `handleJumpToSection`（透传 `scrollToAnchorId`/`scrollToQuote`）一并传入 `<MessageRow>`。

### 新建浮层组件 — `frontend/src/components/preview/markdown-preview-overlay.tsx`

（已实现；浮层外壳后来抽成共享的 `file-preview-shell.tsx`——portal 进 overlay 层（`LAYER_SURFACE_CLASS`）+ `fixed inset-0` 面 + h-14 顶栏 + Esc 关闭，本浮层只提供差异化主体。）

结构：

- **顶栏（h-14，由 `FilePreviewShell` 提供）**：文件名 + `formatBytes(size)`；右侧按钮组：Outline 切换、Comments 切换、Download、Close（Esc 也关闭）。
- **主体（`flex flex-1 min-h-0`）**：
  - 左侧 Outline 抽屉（切换显隐，`w-60 shrink-0 border-r`，实现为浮层内的私有 `OutlineList` 组件——设计稿里独立 `outline-panel.tsx` 未建，已并入浮层）：标题列表，点击 `document.getElementById(id)?.scrollIntoView({ block: "start" })`。
  - 中间 markdown 滚动列（`flex-1 overflow-y-auto`）：
    ```tsx
    <MarkdownRenderer content={active.content} variant="preview"
      className="mx-auto max-w-4xl px-6 py-8" />
    ```
    （设计稿的 `customId="md-preview"` / `batchRendering` / `maxLiveNodes` 参数随 markstream→streamdown 迁移取消。）
    `useEffect` 在 `status === "ready"` 后经双重 rAF 等渲染稳定再跑 `buildOutline` 设置 outline state，并处理 `scrollToAnchorId` 跨场景跳转。
  - 右侧 Comments aside（切换显隐，`CommentsAside`）：见下。
- `status === "too-large"` / `"error"` / `"loading"` 时主体区显示 `PreviewPlaceholder` 占位（too-large 时给出「文件过大，不支持预览」提示 + Download 按钮）。

### 新建评论 aside — `frontend/src/components/preview/comments-aside.tsx`

（已实现；2026-09-06 重构后列表 + composer 公共实现下沉到 `frontend/src/components/preview/comments-panel.tsx` 的 `CommentsPanel` + `usePreviewComments`，`CommentsAside` 只是 markdown 侧薄适配器：pendingAnchor 来自文档 DOM 选区（`anchorForSelection`），跳转走 `scrollIntoView`。）

结构对齐用户参考 HTML（header `Comments · {filename}` / 滚动列表 / 底部 composer），样式改用项目 tokens。

- **评论列表**：来源 `usePreviewComments`（按 `threadByRoot[rootMessageId]` 过滤 `m.attachments?.some(a => a.sectionAnchor && a.id === file.id)`）。每条用 `AttachmentCommentCard`（compact 变体）：头像 + 发送人 + 时间、锚点 chip（`§ 2.1 …`，带 `MapPin` 图标）、引用文本（斜体 + 左竖线）、评论正文（`m.content`）。
- **进入评论模式时**：`usePreviewComments` 调用 `loadThreadMessages(conversation, rootMessageId)` 拉取线程快照（不用 `openThread`——那会把 thread 面板弹到浮层后面）；评论随线程消息更新。
- **框选 → 评论**：评论模式开启时，aside 监听 `document` 的 `mouseup`；当 `window.getSelection()` 在主体容器内且非空，调用 `anchorForSelection(container, selection, outline)` 得到 `{ sectionId, sectionAnchor, quotedText }` 作为 `pendingAnchor`，浮出「添加评论」入口把引用填进 composer 并聚焦（`focusKey` 递增）。composer 提交时：

  ```ts
  sendThreadMessage(conversationId, rootMessageId, body, [], [
    create(AttachmentSchema, {
      id: file.id, name: file.name, mimeType: file.mimeType, sizeBytes: file.sizeBytes,
      sectionAnchor, sectionId, quotedText,
    }),
  ]);
  ```

  发送后清空 composer 与 `pendingAnchor`；新评论通过线程快照更新出现在列表中。

### 新建 `frontend/src/components/preview/attachment-comment-card.tsx`

共享卡片，`variant: "inline" | "compact"`：渲染锚点 chip（`MapPin` + `sectionAnchor`）、引用块（`sectionAnchor` 下的 `quotedText`，斜体 + 左竖线）、正文。

- `inline` 用于 thread 面板内的 `MessageRow`（评论作为一条 thread 回复展示）。
- `compact` 用于浮层评论 aside 的列表项（再加头像/发送人/时间）。
- 点击锚点 chip 可调用 `onJumpToSection(sectionId)`：浮层内滚动到对应标题；thread 面板内可打开预览到该段落（作为后续增强）。

### 挂载浮层

（已实现；落地形式是 dashboard 布局里的三个 lazy gate。）`frontend/src/app/layouts/dashboard-layout.tsx` 挂 `MarkdownPreviewGate` / `HtmlPreviewGate` / `ImagePreviewGate`，各自由 `activePreview?.kind`（image 走独立 `image-preview` slice）决定是否渲染对应浮层单例。chat 是 dashboard 路由，覆盖到位。

### i18n / 样式

- `frontend/src/locales` 增加预览/评论相关文案 key（`preview.outline` / `preview.comments` / `preview.too-large` / `preview.html-add-comment` 等）。
- （历史）设计稿写的是复用 `.markstream-chat`（`tailwind.css` 425–517）；该作用域已随 markstream→streamdown 迁移移除，markdown 主体现用 `.markdown-content` 类（`lib/markdown.tsx` 的 `variantClassName`），预览专属微调（`max-w-4xl px-6 py-8`）以 `className` 传给 `MarkdownRenderer`。

## 受影响 / 新增文件清单

**Proto**：`proto/v1/v1/command.proto`（扩展 `Attachment`）→ `buf generate`。（已完成。）

**Backend**：
- `backend/manager/api/v1/channel_file_service.go`（`resolveAttachments` 保留锚点字段——已实现）
- `backend/manager/api/v1/channel_message.go`（`SendMessage` 调用 `resolveAttachments`；设计时在 `channel.go`，后拆分至此——已实现）

**Frontend 新增**：
- `frontend/src/stores/preview.ts`
- `frontend/src/lib/markdown-file.ts`（+ `markdown-file.test.ts`）
- `frontend/src/components/preview/markdown-preview-overlay.tsx`
- （原计划的 `outline-panel.tsx` 未单独建文件，outline 列表并入浮层的私有 `OutlineList` 组件）
- `frontend/src/components/preview/comments-aside.tsx`
- `frontend/src/components/preview/attachment-comment-card.tsx`
- （后续重构产物）`frontend/src/components/preview/file-preview-shell.tsx`、`frontend/src/components/preview/comments-panel.tsx`（另见 html-preview 设计文档）

**Frontend 修改**：
- `frontend/src/stores/index.ts`、`frontend/src/stores/types.ts`（注册 `PreviewSlice`）
- `frontend/src/components/chat/file-card.tsx`（`onPreview` / `previewDisabledReason`）
- `frontend/src/components/chat/message-row.tsx`（`onPreviewAttachment` + 锚点附件分支渲染 `AttachmentCommentCard variant="inline"`）
- `frontend/src/components/chat/thread-panel.tsx`（`onPreviewAttachment` 下传）
- `frontend/src/pages/dashboard/chat-conversation.tsx`（`handlePreviewAttachment` + `handleJumpToSection`，下传 `MessageRow`；浮层经 dashboard 布局单例挂载，本文件无需直接挂）
- `frontend/src/app/layouts/dashboard-layout.tsx`（挂预览浮层 gate）
- `frontend/src/assets/css/tailwind.css`（历史：`.markstream-chat` 作用域已随 streamdown 迁移移除）
- `frontend/src/locales/*`（文案）

## 实施分期（同一方案内）

（两期均已交付。）

- **Phase 1（前端 only，无 proto 改动）**：浮层 + Outline + 10MB 守卫 + `FileCard` 预览入口 + `preview` slice。可独立交付「美观预览 + 目录」。
- **Phase 2（proto + 后端 + 前端）**：`Attachment` 锚点字段、`resolveAttachments` / `SendMessage` 改动、评论模式（框选 + aside + composer + 发送 + 结构化渲染）。交付「锚点评论进 thread」。

## Verification

1. **后端**：`buf lint proto && go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`；`golangci-lint run --allow-parallel-runners`。计划中"`resolveAttachments` 保留锚点字段 / 拒绝跨会话文件 id"的单测在仓库中**未见落地**（`channel_send_message_test.go` 只覆盖 `validateSendMessageContent` 内容校验），属遗留缺口。
2. **前端**：`pnpm --dir frontend biome:check && pnpm --dir frontend type-check && pnpm --dir frontend test`。`lib/markdown-file.ts` 的 `buildOutline` / `anchorForSelection` / `isMarkdownAttachment` 单测已存在（`markdown-file.test.ts`）。
3. **端到端**：
   - 启动 `go run ./backend/manager/bin/server/main.go --port 8181 --debug` + `pnpm --dir frontend dev`。
   - 在 channel 里上传一个 `.md` 文件 → `FileCard` 出现预览入口；点击 → 浮层全屏打开，markdown 美观渲染，代码块高亮与聊天一致。
   - 切换 Outline → 标题列表正确，点击跳转。
   - 上传一个 >10MB 的 `.md` → 预览入口禁用并提示「文件过大，不支持预览」；下载仍可用。
   - 开启 Comments → 右侧 aside 出现；框选文档一段文本 → 出现「添加评论」→ 输入正文发送 → 列表出现新评论卡片（锚点 + 引用 + 正文）。
   - 关闭浮层，在主界面打开该消息的 thread → 该评论作为一条 thread 回复可见，带锚点 chip 与引用块。
   - agent 通过 CLI `file upload` 上传 `.md` 后，前端对其附件同样可预览/评论。

## Open questions / 后续增强

- **选区重高亮**（仍未实现）：markdown 评论只存 `quotedText` + `sectionAnchor`，重新打开预览时不重高亮原文中对应片段。后续可在锚点字段加 `range`（如字符偏移或 DOM 路径 + offset），实现重高亮。（HTML 预览侧已有「quote + nearY 定位 + 高亮矩形」的定位实现，见 html-preview 设计文档。）
- **锚点跳转跨场景**（已实现）：thread 面板/activity 内点击锚点 chip 通过 `handleJumpToSection` 调 `openFilePreview(..., scrollToAnchorId, scrollToQuote)`，浮层打开后滚动到对应段落——`stores/preview.ts` 的 `scrollToAnchorId`/`scrollToQuote` 字段与两个浮层各自的定位逻辑（markdown `scrollIntoView`、html `locate` + `scroll-to`）承载。
- **图片/PDF 预览**：图片预览已实现（`ImagePreviewOverlay` + `stores/image-preview.ts` lightbox，`lib/image-file.ts` 的 `isImageAttachment` 判定，chat 中经 `onPreviewImage` 接线）；PDF 预览仍未实现。