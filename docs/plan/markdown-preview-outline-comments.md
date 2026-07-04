# Markdown 文件预览 + Outline + 锚点评论 设计文档

## Context

当前前端对附件的处理只有「下载」一种（`FileCard` → `commandServiceClient.downloadFile` → blob 下载），没有任何预览。用户/agent 上传的 markdown 文件无法在产品内阅读，更无法就文档某一段内容展开讨论。

需求演化为三部分：

1. **专注阅读**：用一个**遮蔽整个页面**的浮层预览 markdown，渲染美观（与聊天消息中的 markdown 完全一致），不被输入框/thread 内容打断阅读节奏。
2. **Outline**：基于标题生成目录，点击跳转，方便长文阅读。
3. **锚点评论**：开启评论模式后右侧出现评论 aside；用户在文档中**框选文本**写评论，该评论连同**所框选的引用文本**与**所在段落锚点（§ X.Y 标题）**一起作为一条 thread 消息发送到「该附件所属消息」的 thread 里。评论既在浮层的评论 aside 中以紧凑卡片展示，也在常规 thread 面板中作为结构化消息展示。

文件过大（>10MB）不支持预览，需提示用户改为下载。

## 关键设计决策（已与用户确认）

1. **渲染栈**：复用现有 `markstream-react`，不引入 react-markdown 等第二套栈。
2. **预览形态**：遮蔽整个页面的全屏浮层（专注阅读），不再是早期设想的右侧 420px aside。
3. **评论落点**：评论作为 thread 回复发送到「附件所属消息」的 thread；该消息即 thread root（若附件本身挂在某条 thread 回复上，则回退到该回复的 `threadRoot`）。
4. **评论数据模型**：扩展 `Attachment` 增加锚点字段，而非新增顶层 `AttachmentComment` 消息字段——附件已 JSON 序列化进 BYTEA，**无需 DB 迁移**。
5. **大小阈值**：10MB，依据 `attachment.sizeBytes` 在打开预览前判定，过大则禁用预览入口并提示下载。

## 关键决策：复用 `markstream-react`

调研结论：**继续使用 `markstream-react`，不引入第二个 markdown 栈。**

- 已在 bundle 中：`main.tsx` 全局引入 `markstream-react/index.css`，`src/lib/markdown.tsx` 已注册 Shiki 代码块自定义组件，`tailwind.css` 中已有 `.markstream-chat` 作用域样式（425–517 行）。
- 已被 `MessageRow`（聊天气泡）与 `FinalSummary`（静态文档）使用，**静态内容用 `final` flag 即可**（`FinalSummary` 已验证），预览复用后渲染外观与聊天消息 1:1 一致。
- 自带 `batchRendering` / `deferNodesUntilVisible` / `maxLiveNodes`，对接近 10MB 的大文档有虚拟化/分批渲染优势，是 react-markdown 没有的。
- 引入 react-markdown + remark/rehype 会重复一套语法高亮与样式，且预览外观与聊天不一致。零收益。

唯一的渲染入口仍是默认导出的 `<MarkdownRender>`，预览中以 `customId="md-preview"`、`final`、`fade` 调用，包在 `.markstream-chat` wrapper 内复用既有样式。

## 数据模型：扩展 `Attachment`（无需 DB 迁移）

一条评论 = 一条 thread 回复（`ChatMessage` + `thread_root`），其 `attachments` 携带**一个锚点附件**：指向被评论的 markdown 文件，并附带段落锚点与引用文本。`content` 为评论文本。

`Attachment` 已被 JSON 序列化进 BYTEA 列（`store/chat_message.go` 的 `marshalAttachments`），**新增字段自动持久化，无需 schema 迁移**。这比新增一个 `AttachmentComment` 顶层消息字段（需要新增列、改 3 个请求结构）更高效，符合项目「少即是好、无迁移」的取向。

`proto/v1/v1/command.proto` — `Attachment`（当前 252–259）新增三个可选字段，空值表示普通整文件附件：

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

### 1. `backend/manager/api/v1/channel_file_service.go` — `resolveAttachments`（296–327）

当前重建 `Attachment` 时只回填 `Id/Name/MimeType/SizeBytes`，会**丢弃** caller 传入的锚点字段。改为在重建时**保留** `a.SectionAnchor / a.SectionId / a.QuotedText`（这三项是 caller 语义，文件行不是其来源）。文件元数据仍以文件行为准。

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

### 2. `backend/manager/api/v1/channel.go` — `SendMessage`（266–365）

当前**未**调用 `resolveAttachments`（只有 agent 路径 `PostMessage` 调用），用户附件 id 不经会话归属校验直接落库（既有安全缺口）。改为在持久化前调用 `resolveAttachments(ctx, convID, req.Msg.Attachments)`，把返回值传给 `CreateChatMessageBumpVersion` / `CreateTaskMessageBumpVersion` 的 `Attachments`。一举两得：既补上用户路径的文件归属校验，又让锚点字段被规范化保留。

```go
attachments, err := s.resolveAttachments(ctx, convID, req.Msg.Attachments)
if err != nil {
    return nil, err
}
// ... 把 attachments 传给 CreateChatMessageBumpVersion / CreateTaskMessageBumpVersion
```

### 3. store / 序列化

无需改动：`[]*v1pb.Attachment` 直接 marshal 进 BYTEA；`storeToV1ChatMessage` 透传 `Attachments`。新增字段随既有序列化路径自动往返。

改完按 CLAUDE.md：`buf format -w proto && buf lint proto && cd proto && buf generate`，再 `gofmt` / `golangci-lint run --allow-parallel-runners` / `go build`。

## 前端改动

### 新建 store slice — `frontend/src/stores/preview.ts`

```ts
interface PreviewSlice {
  activePreview: {
    conversation: string;       // "conversations/{id}"
    conversationId: string;     // bare id
    rootMessageId: string;      // 评论落点的 thread root（= 附件所属消息的 threadRoot ?? 自身 id）
    attachment: Attachment;     // 被预览的文件
    content: string;            // 解码后的 markdown 文本
    status: "loading" | "ready" | "error" | "too-large";
    error?: string;
  } | null;
  openFilePreview(conversation: string, rootMessageId: string, attachment: Attachment): Promise<void>;
  closeFilePreview(): void;
}
```

- `openFilePreview`：若 `attachment.sizeBytes > MAX_MARKDOWN_PREVIEW_BYTES`（10MB）→ 直接 `status: "too-large"` 并打开浮层（浮层内显示「文件过大，不支持预览，请下载」），**不发起下载**。否则 `commandServiceClient.downloadFile({ id })` → `new TextDecoder().decode(data)` → `status: "ready"`。错误时 `status: "error"`。
- 在 `stores/index.ts` 注册 slice；在 `stores/types.ts` 加 `PreviewSlice`。

### 新建工具 — `frontend/src/lib/markdown-file.ts`

- `MAX_MARKDOWN_PREVIEW_BYTES = 10 * 1024 * 1024`。
- `isMarkdownAttachment(att)`：`/\.(md|markdown|mdx)$/i.test(name) || mimeType === "text/markdown" || mimeType === "text/x-markdown"`。
- `slugify(text)`：标题文本 → 安全 slug，用于 `id`。
- `buildOutline(container: HTMLElement)`：`container.querySelectorAll("h1,h2,h3,h4,h5,h6")` → 给每个标题分配 `id = md-${index}-${slug(text)}`；按层级计数器生成编号（`1`, `1.1`, `2`, `2.1`…）；返回 `{ level, text, id, number }[]`。展示时用正则剥掉标题文本开头的既有编号 `^\d+(\.\d+)*\s+`，避免 `§ 2.1 2.1 Server`。
- `anchorForSelection(container, selection)`：用 `compareDocumentPosition` 在 `container` 内找到选区之前的最后一个标题，返回 `{ sectionId, sectionAnchor: "§ {number} {text}", quotedText }`；`quotedText` 截断到 ~500 字。

### `frontend/src/components/chat/file-card.tsx`

新增可选 props：`onPreview?: () => void`、`previewDisabledReason?: string`。

- 当 `onPreview` 存在：卡片主体点击 → `onPreview()`；右侧保留一个独立的小下载图标按钮（`Download`）走原 `handleDownload`。`previewDisabledReason` 存在时主体禁用并以此作为 tooltip，提示「文件过大（>10MB），不支持预览，请下载」。
- `onPreview` 不存在（非 markdown 或上层未接线）：行为完全不变（整卡点击下载），保持对非 markdown 附件的向后兼容。

### `frontend/src/components/chat/message-row.tsx` + `thread-panel.tsx`

新增 prop `onPreviewAttachment?: (att: Attachment, rootMessageId: string) => void`。

- `message-row.tsx`：把 `msg.threadRoot ?? msg.id` 作为 rootMessageId 传给回调；在附件 map 中分支：
  - `att.sectionAnchor` 非空 → 渲染 `<AttachmentCommentCard variant="inline">`（评论作为 thread 回复的结构化展示）。
  - 否则 `<FileCard onPreview={isMarkdownAttachment(att) ? () => onPreviewAttachment(att, effRoot) : undefined} previewDisabledReason={isMarkdown(att) && att.sizeBytes > MAX ? "…" : undefined} />`。
- `thread-panel.tsx` `RootContext`：同样给 `FileCard` 接 `onPreview`（root 的 `msg.id` 即 rootMessageId）；replies 经 `MessageRow` 同上。
- `chat-conversation.tsx`（约 600–887）：实现 `handlePreviewAttachment = (att, rootId) => openFilePreview(conversationName, rootId, att)`，传入 `<MessageRow>`。

### 新建浮层组件 — `frontend/src/components/preview/markdown-preview-overlay.tsx`

通过 layer 系统 portal 到 overlay 层（参考 `components/ui/sheet.tsx` 用 `getLayerRoot("overlay")`，z-index 2500），`fixed inset-0 bg-background`，遮蔽整页用于专注阅读。结构：

- **顶栏（~56px，`shrink-0 border-b`）**：文件名 + `formatBytes(size)`；右侧按钮组：Outline 切换、Comments 切换、Download、Close（Esc 也关闭）。
- **主体（`flex flex-1 min-h-0`）**：
  - 左侧 Outline 抽屉（切换显隐，`w-60 shrink-0 border-r`）：标题列表，点击 `document.getElementById(id)?.scrollIntoView({ block: "start" })`。
  - 中间 markdown 滚动列（`flex-1 overflow-y-auto`，`max-w-4xl mx-auto`）：
    ```tsx
    <div className="markstream-chat">
      <MarkdownRender customId="md-preview" content={content} final fade
        batchRendering deferNodesUntilVisible maxLiveNodes={4000} />
    </div>
    ```
    `useEffect` 在 `content`/`status` 就绪后跑 `buildOutline` 设置 outline state，并给标题注入 id。
  - 右侧 Comments aside（切换显隐，`w-full max-w-80 shrink-0 border-l`，参考用户给出的结构但用项目 design tokens：`border-control-border`、`bg-background`、`text-main` 等，不用 brutal-black）：见下。
- `status === "too-large"` / `"error"` / `"loading"` 时主体区显示对应占位（too-large 时给出「文件过大，不支持预览」提示 + Download 按钮）。

### 新建评论 aside — `frontend/src/components/preview/comments-aside.tsx`

结构对齐用户参考 HTML（header `Comments · {filename}` / 滚动列表 / 底部 composer），样式改用项目 tokens。

- **评论列表**：来源 `threadByRoot[rootMessageId].messages`，过滤 `m.attachments?.some(a => a.sectionAnchor && a.id === file.id)`。每条用 `AttachmentCommentCard`（compact 变体）：头像 + 发送人 + 时间、锚点 chip（`§ 2.1 …`，带 `MapPin` 图标）、引用文本（斜体 + 左竖线）、评论正文（`m.content`）。
- **进入评论模式时**：若 `threadByRoot[rootMessageId]` 未加载，调用 `openThread(conversation, rootMessageId)` 拉取并启动轮询（复用现有 thread slice，评论随 thread 消息实时更新）。
- **框选 → 评论**：评论模式开启时，监听主体容器的 `mouseup` / `selectionchange`；当 `window.getSelection()` 在容器内且非空，调用 `anchorForSelection(container, selection)` 得到 `{ sectionId, sectionAnchor, quotedText }`，在选区附近浮出「添加评论」小按钮（或直接把引用填进 composer 并聚焦）。composer textarea 输入正文，提交时：

  ```ts
  sendThreadMessage(conversationId, rootMessageId, body, [], [
    create(AttachmentSchema, {
      id: file.id, name: file.name, mimeType: file.mimeType, sizeBytes: file.sizeBytes,
      sectionAnchor, sectionId, quotedText,
    }),
  ]);
  ```

  发送后清空 composer；新评论通过 thread 轮询/乐观更新出现在列表中。

### 新建 `frontend/src/components/preview/attachment-comment-card.tsx`

共享卡片，`variant: "inline" | "compact"`：渲染锚点 chip（`MapPin` + `sectionAnchor`）、引用块（`sectionAnchor` 下的 `quotedText`，斜体 + 左竖线）、正文。

- `inline` 用于 thread 面板内的 `MessageRow`（评论作为一条 thread 回复展示）。
- `compact` 用于浮层评论 aside 的列表项（再加头像/发送人/时间）。
- 点击锚点 chip 可调用 `onJumpToSection(sectionId)`：浮层内滚动到对应标题；thread 面板内可打开预览到该段落（作为后续增强）。

### 挂载浮层

`frontend/src/app/layouts/dashboard-layout.tsx` 末尾挂一个 `<MarkdownPreviewOverlay />`（单例，读 `activePreview`）。chat 是 dashboard 路由，覆盖到位。

### i18n / 样式

- `frontend/src/locales` 增加预览/评论相关文案 key（outline、comments、too-large 提示等）。
- markdown 主体复用 `.markstream-chat`（`tailwind.css` 425–517）；如需预览专属微调（如更大行宽）在该作用域内追加，不污染聊天气泡。

## 受影响 / 新增文件清单

**Proto**：`proto/v1/v1/command.proto`（扩展 `Attachment`）→ `buf generate`。

**Backend**：
- `backend/manager/api/v1/channel_file_service.go`（`resolveAttachments` 保留锚点字段）
- `backend/manager/api/v1/channel.go`（`SendMessage` 调用 `resolveAttachments`）

**Frontend 新增**：
- `frontend/src/stores/preview.ts`
- `frontend/src/lib/markdown-file.ts`
- `frontend/src/components/preview/markdown-preview-overlay.tsx`
- `frontend/src/components/preview/outline-panel.tsx`（或并入浮层）
- `frontend/src/components/preview/comments-aside.tsx`
- `frontend/src/components/preview/attachment-comment-card.tsx`

**Frontend 修改**：
- `frontend/src/stores/index.ts`、`frontend/src/stores/types.ts`（注册 `PreviewSlice`）
- `frontend/src/components/chat/file-card.tsx`（`onPreview` / `previewDisabledReason`）
- `frontend/src/components/chat/message-row.tsx`（`onPreviewAttachment` + 锚点附件分支渲染）
- `frontend/src/components/chat/thread-panel.tsx`（`RootContext` 接 `onPreview`、replies 分支渲染）
- `frontend/src/pages/dashboard/chat-conversation.tsx`（`handlePreviewAttachment`，下传 `MessageRow`；浮层经 dashboard 布局单例挂载，本文件无需直接挂）
- `frontend/src/app/layouts/dashboard-layout.tsx`（挂 `<MarkdownPreviewOverlay />`）
- `frontend/src/assets/css/tailwind.css`（`.markstream-chat` 预览微调，如需）
- `frontend/src/locales/*`（文案）

## 实施分期（同一方案内）

- **Phase 1（前端 only，无 proto 改动）**：浮层 + Outline + 10MB 守卫 + `FileCard` 预览入口 + `preview` slice。可独立交付「美观预览 + 目录」。
- **Phase 2（proto + 后端 + 前端）**：`Attachment` 锚点字段、`resolveAttachments` / `SendMessage` 改动、评论模式（框选 + aside + composer + 发送 + 结构化渲染）。交付「锚点评论进 thread」。

## Verification

1. **后端**：`buf lint proto && go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`；`golangci-lint run --allow-parallel-runners`。新增/扩展单测：`resolveAttachments` 保留锚点字段、拒绝跨会话文件 id；`SendMessage` 路径附件经归属校验。
2. **前端**：`pnpm --dir frontend biome:check && pnpm --dir frontend type-check && pnpm --dir frontend test`。为 `lib/markdown-file.ts` 的 `buildOutline` / `anchorForSelection` / `isMarkdownAttachment` 写单测。
3. **端到端**：
   - 启动 `go run ./backend/manager/bin/server/main.go --port 8181 --debug` + `pnpm --dir frontend dev`。
   - 在 channel 里上传一个 `.md` 文件 → `FileCard` 出现预览入口；点击 → 浮层全屏打开，markdown 美观渲染，代码块高亮与聊天一致。
   - 切换 Outline → 标题列表正确，点击跳转。
   - 上传一个 >10MB 的 `.md` → 预览入口禁用并提示「文件过大，不支持预览」；下载仍可用。
   - 开启 Comments → 右侧 aside 出现；框选文档一段文本 → 出现「添加评论」→ 输入正文发送 → 列表出现新评论卡片（锚点 + 引用 + 正文）。
   - 关闭浮层，在主界面打开该消息的 thread → 该评论作为一条 thread 回复可见，带锚点 chip 与引用块。
   - agent 通过 CLI `file upload` 上传 `.md` 后，前端对其附件同样可预览/评论。

## Open questions / 后续增强

- **选区重高亮**：当前评论只存 `quotedText` + `sectionAnchor`，重新打开预览时不重高亮原文中对应片段。后续可在锚点字段加 `range`（如字符偏移或 DOM 路径 + offset），实现重高亮。
- **锚点跳转跨场景**：thread 面板内点击锚点 chip 直接打开预览并滚到对应段落（当前 Phase 2 仅在浮层内跳转）。
- **图片/PDF 预览**：本方案只覆盖 markdown；`FileCard` 对图片/PDF 的预览可后续复用同一浮层壳。