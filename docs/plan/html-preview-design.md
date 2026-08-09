# HTML 文件预览 + 选区评论 设计文档

## Context

当前 chat / activity / agent workspace 三个场景已经支持 markdown 预览:

- **chat / activity**:`FileCard` 预览入口 → `openFilePreview`(全屏浮层 + `markstream-react` 渲染 + outline + 锚点评论)。
- **agent workspace**:文件面板内联渲染(`workspace-file-panel.tsx`)。

HTML 文件此前只能下载(chat/activity)或按纯文本展示(workspace),无法像 markdown 一样预览,更无法在渲染后的页面上评论。

本方案的目标:

1. **chat / activity**:全屏浮层预览 HTML 文件(与 markdown 预览浮层同构);
2. **选区评论**:用户在渲染后的 HTML 上**框选文本**写评论,评论作为 thread 回复携带锚点附件(复用 markdown 评论的数据模型与渲染卡片);
3. **agent workspace**:文件面板内联渲染 HTML(与 markdown 内联一致,无评论);
4. **不需要大纲**(用户已确认);
5. **零新增前端依赖**:采用原生 `<iframe srcDoc>` + 自研桥接脚本,不使用 react-frame-component(用户已确认,见"关键设计决策 1")。

## 关键设计决策

### 1. 渲染栈:原生 `<iframe srcDoc>` + 客户端注入桥接脚本(不用 react-frame-component)

预览内容是把附件文本解码后直接作为 `srcDoc` 设置的字符串,react-frame-component 提供的 iframe 生命周期/children 同步等 React 封装不是必需项;跨 frame 通信需要的是注入到文档内的桥接脚本,由我们自己维护。

```ts
// lib/html-file.ts
const HTML_NAME_RE = /\.(html?|xhtml)$/i;
const HTML_MIME = new Set(["text/html", "application/xhtml+xml"]);
export const MAX_HTML_PREVIEW_BYTES = 10n * 1024n * 1024n; // 与 markdown 预览同阈值

export function isHtmlAttachment(att: Attachment): boolean {
  if (att.mimeType && HTML_MIME.has(att.mimeType)) return true;
  return HTML_NAME_RE.test(att.name ?? "");
}
export function isHtmlPreviewable(att: Attachment): boolean {
  return isHtmlAttachment(att) && (att.sizeBytes ?? 0n) <= MAX_HTML_PREVIEW_BYTES;
}

// buildHtmlPreviewDoc 把下载的 HTML 文本包装成可安全渲染的 srcDoc:
//   - 完整文档(<html>…</html>):在 </head> 前注入桥接 <script>(无 </head>
//     但有 <body> 时插到 <body 标签后,均保留原 tag 大小写);
//   - 片段(如 "<h1>Hi</h1>"):包一层
//     <!doctype html><html><head><meta charset="utf-8">…</head><body>…</body></html>;
//   - 文档未声明字符集时补 <meta charset="utf-8">,保证沙箱渲染与 UTF-8 解码一致。
export function buildHtmlPreviewDoc(content: string, nonce: string): string;
```

桥接脚本(`htmlBridgeSource`,常量字符串,随 `buildHtmlPreviewDoc` 注入)是 iframe 与父页面之间唯一的通信通道。每次打开预览生成一个 `nonce` 写死在脚本闭包里(占位符 `__AC_BRIDGE_NONCE__` 在构建时替换)。

### 2. sandbox 权限:仅 `allow-scripts`

- 允许文档内 JS 运行(否则现代网页无法渲染);
- **opaque origin**:无法读写父页面、cookie/localStorage/sessionStorage;
- 表单提交被浏览器阻止(`allow-forms` 不加),桥接内仍对 `submit` 事件 `preventDefault` 双保险;
- `window.open` / `target=_blank` 被阻止(`allow-popups` 不加)——由桥接的链接拦截接管(见决策 4);
- 页面内 `<script>` 照常执行,可加载绝对 URL 的外部资源。

**已知限制**(文档化):`srcDoc` 的 base URL 是 `about:srcdoc`,HTML 内的**相对路径资源**(`src="./x.png"`、`href="page.html"`)无法解析;使用绝对 URL 或 data: URI 的资源不受影响。这与附件上传场景一致(附件本身没有 URL 上下文),可接受。

### 3. 选区交互:iframe 内原生文本选择 + 桥接上报

不做父页面覆盖层框选,而是让用户在 iframe 内像浏览普通网页一样**原生拖选**文本(支持双击/三击选词选段),桥接脚本监听 `mouseup` 与 `selectionchange`(200ms 防抖)上报选区:

- `text`:归一化(连续空白折叠)后的选中文本,截断到 500 字;
- `x/y/w/h`:选区 bounding rect 的**内容坐标**(rect + `scrollX/scrollY`)。

父页面在选区下方浮出"添加评论"按钮,点击后把引用填入评论面板 composer。

优势:交互与 Laelia markdown 评论完全一致;quote 是用户选中的**精确文本**;无需覆盖层、滚轮转发与矩形反查。代价:选区跨出 iframe 边界时可能不完整(极边缘场景,可接受)。

### 4. 链接处理:桥接捕获阶段拦截,父页面新标签打开

```js
document.addEventListener("click", (e) => {
  const a = e.target?.closest?.("a[href]");
  if (!a) return;
  const href = a.getAttribute("href");
  if (typeof href !== "string" || !href.trim()) return;
  e.preventDefault();
  e.stopPropagation();
  post({ type: "link-clicked", href: href.slice(0, 4097), text: normalize(a.textContent || "").slice(0, 200) });
}, true);
```

父页面收到 `link-clicked` 后 `window.open(href, "_blank", "noopener,noreferrer")`。这样预览不会因点击链接而导航走(导航走后桥接脚本丢失,预览变白页),所有出站 href 截断到 4097 字并视为不可信数据。

### 5. 评论数据模型:复用 Attachment 锚点字段,零 proto / 后端改动

markdown 评论已把锚点数据塞进 `Attachment` 的三个可选字段,后端 `resolveAttachments` 已保留这些字段、`SendMessage` 已调用 `resolveAttachments`。HTML 评论直接复用:

| 字段 | markdown 含义 | html 含义 |
|---|---|---|
| `section_anchor` | `§ 2.1 Server (server/)` 展示串 | 引用文本前 60 字(展示 chip) |
| `section_id` | 标题 DOM id | 锚点定位串 `html:y:{contentY}`,`html:` 前缀用于区分 |
| `quoted_text` | 选区文本(≤500) | 归一化选区文本(≤500) |

锚点定位采用"**引用文本 + 选区内容坐标 y**"方案:重新打开预览时父页面发 `locate {quote, nearY}`,桥接脚本按归一化 `textContent` 匹配元素、剔除祖先冗余、多命中取最接近 y 的候选,回复内容坐标(见"桥接脚本协议")。HTML 是静态附件,内容不变时定位稳定;即使 DOM 因脚本非确定性变化,quote 文本匹配 + nearY 也能兜底。

因此 **proto、后端、DB 均零改动**;`AttachmentCommentCard` 与 thread 消息渲染原样复用。

### 6. 评论 UI:与 markdown 预览一致(右侧 aside 按需展开)

- 顶栏"评论"按钮切换右侧 `w-80` aside,结构复刻 markdown 的 `CommentsAside`(`html-comments-aside.tsx`);
- aside 打开时,iframe 内选区 → 浮出"添加评论"按钮 → 引用填入 composer(带可移除的引用 chip);
- 评论列表 = thread 回复(过滤 `sectionAnchor !== "" && id === attachment.id`),复用 `AttachmentCommentCard`,支持回复与 Enter 发送键位;
- 点击评论卡片锚点 chip → `locate` + `scroll-to`,iframe 滚动到锚点并显示 2s 渐隐的 accent 高亮矩形;
- 评论标记:iframe 之上 `absolute` 层画小 pin(位置 = 内容坐标 − 当前 scroll,由桥接 `state` 消息驱动刷新)。

### 7. 大小限制与入口

- chat/activity:10 MiB,与 markdown 一致。`sizeBytes` 超限不触发下载,浮层显示 too-large 提示 + 下载按钮(复用现有 store 逻辑);
- workspace:后端 `policy.go` 的 `textExtensions` 已含 `.html`(1MB 文本上限),`workspace.Read` 已按文本返回 → **后端零改动**;前端面板按扩展名(`.html/.htm/.xhtml`)加 html 分支。

## 桥接脚本协议

### 消息总览

所有消息统一结构,双端都校验:

```js
{ slockAcBridge: 1, nonce, documentEpoch, type, ... }
```

| 方向 | type | payload | 触发 |
|---|---|---|---|
| P→C | `activate-document` | `documentEpoch`(随机串,≤200 字符) | iframe `load` 后由父页面下发(targetOrigin `"*"`——opaque origin 无法指定具体 origin) |
| C→P | `state` | `scrollX/scrollY/docWidth/docHeight/viewportWidth/Height` | 激活后首次;scroll / resize / `MutationObserver` / `ResizeObserver` / DOMContentLoaded / load,rAF 节流 |
| C→P | `selection` | `text, x, y, w, h`(内容坐标) | `mouseup` / `selectionchange`(200ms 防抖),选区非空且非折叠 |
| C→P | `selection-cleared` | — | 选区折叠(此前上报过选区时) |
| P→C | `scroll-to` | `x, y`(内容坐标,平滑滚动) | 点击评论卡片 / 锚点跳转 / 跨场景锚点跳转 |
| P→C | `locate` | `requestId, quote, nearY` | 浮层打开时对每条评论重定位;跳转前定位 |
| C→P | `located` | `requestId, x, y, w, h`(内容坐标;未命中 `x = -1`) | locate 回复 |
| C→P | `link-clicked` | `href, text` | 捕获阶段拦截 `a[href]` 点击 |
| C→P | `esc` | — | iframe 内按 Esc(父页面关闭浮层) |

### 安全模型

1. 桥接脚本首行检查:`window.parent === window` 或 nonce 为空时直接退出;
2. **未收到 `activate-document`(epoch 校验通过)前不发送任何消息**——即使 HTML 自我导航到别的站点,新文档拿不到 epoch,无法冒充原文档发言;
3. 入站消息校验 `slockAcBridge === 1` + `nonce` 匹配;`activate-document` 额外校验 epoch 为 ≤200 字符的字符串;其余消息要求 epoch 与已激活值一致;
4. 所有出站 payload 截断:`text/quote ≤ 500`、`href ≤ 4097`、坐标取有限数值;
5. 父页面侧校验 `e.source === iframeRef.current?.contentWindow` + nonce + epoch;收到的文本只作纯文本展示,坐标做数值化处理;
6. `locate` 带 3s 超时,防恶意文档挂起 UI。

### 锚点定位算法(locateQuote)

1. `TreeWalker`(SHOW_ELEMENT)遍历文档,上限 5000 节点;归一化 `textContent` 后包含 needle 的元素记为候选;
2. **最小容器化**:剔除"包含其他候选"的祖先,得到每个独立出现位置的唯一元素;
3. 重复引用时按 `|中心内容Y − nearY|` 取分,无 nearY 时按文本长度取分(最短的容器);跳过零尺寸元素;
4. 命中回复内容坐标,未命中 `located {x: -1}`。

### 锚点编解码

```ts
export const HTML_ANCHOR_PREFIX = "html:y:";
export const MAX_HTML_QUOTE_CHARS = 500;
export const MAX_HTML_ANCHOR_LABEL_CHARS = 60;

// 选区 → 锚点:quote 归一化截断;y 取选区内容中心
export function htmlAnchorForSelection(quotedText: string, y: number): CommentAnchor | null;
// section_id = "html:y:{round(y)}"; section_anchor = quote.slice(0, 60)

// 锚点 → 定位参数:从评论 section_id 还原存储的 contentY
export function parseHtmlAnchor(sectionId: string): { y: number } | null;
```

## 选区 → 评论完整链路

1. 用户在 iframe 内拖选文本;桥接 `mouseup` / `selectionchange` 上报 `selection`(文本 + 内容坐标);
2. 父页面(仅评论面板开启时处理)把选区转成 `pendingAnchor`(`htmlAnchorForSelection`),在选区下方浮出"添加评论"按钮;
3. 点击按钮 → composer 聚焦(`focusKey` 递增),aside 顶部出现引用 chip(60 字 label + 完整引用 + 移除按钮);
4. 发送:`sendThreadMessage(conversationId, rootMessageId, text, [], [Attachment{ id, name, mimeType, sizeBytes, sectionAnchor, sectionId, quotedText }])`——评论作为 thread 回复携带锚点附件;
5. 发送成功后清除 `pendingAnchor`;用户清除选区或按 X 也可清除。

## 评论锚点定位与标记

- **aside 打开时**:对每条既有评论 `locate {quote, nearY}`(nearY 取评论存储的 y),命中后记录 `located` map 并在 iframe 上画 pin;
- **点击评论卡片**:`AttachmentCommentCard.onJumpToSection` → `locate` → `scroll-to` + 2s 高亮矩形;
- **跨场景锚点跳转**(在 thread/activity 中点击评论锚点 chip 打开预览):store 的 `activePreview` 携带 `scrollToAnchorId`(html 锚点串)与 `scrollToQuote`(引用文本);iframe ready 后 `parseHtmlAnchor` → `locate(quote, y)` → `scroll-to` + 高亮;无 quote 时退化为 `scrollTo(0, y)`。

## 前端实现

### 新增文件

1. **`frontend/src/lib/html-file.ts`**:`isHtmlAttachment` / `isHtmlPreviewable` / `MAX_HTML_PREVIEW_BYTES` / `randomId`(secure context 下 `crypto.randomUUID`,否则时间+随机数兜底)/ `buildHtmlPreviewDoc` / 锚点编解码 / `htmlBridgeSource` 桥接脚本常量。
2. **`frontend/src/lib/html-file.test.ts`**:13 个用例(扩展名/mime/大小边界、`buildHtmlPreviewDoc` 各种文档形态、锚点 round-trip)。
3. **`frontend/src/components/preview/html-preview-overlay.tsx`**:与 `markdown-preview-overlay.tsx` 同构的浮层——顶栏(文件名/大小/评论开关/下载/关闭,无大纲)+ iframe 主体 + 标记/高亮层 + "添加评论"按钮 + 右侧 aside。负责 nonce/epoch 生成、`activate-document`、消息校验分发、`locate` 请求表(3s 超时)、跨场景锚点跳转。Esc 关闭(父页面 keydown + iframe 内 Esc 经桥接转发)。
4. **`frontend/src/components/preview/html-comments-aside.tsx`**:评论列表 + composer + `useHtmlComments` hook(按 root 线程过滤锚点评论)。与 `comments-aside.tsx` 的差异:pendingAnchor 来自桥接消息而非本地 DOM 选区;跳转走 `locate` + `scroll-to` 而非 `scrollIntoView`。
5. **`frontend/src/components/workspace/html-file-view.tsx`**:workspace 内联 iframe,复用 `buildHtmlPreviewDoc`;激活桥接,仅处理 `link-clicked`(无评论,与 markdown 内联一致)。

### 修改文件

1. **`frontend/src/stores/types.ts` / `stores/preview.ts`**:`activePreview` 增加 `kind: "markdown" | "html"`;`openFilePreview` 按 `isHtmlAttachment` 决定 kind,大小阈值按 kind 取;`scrollToSectionId` 更名为 `scrollToAnchorId`,新增 `scrollToQuote`(html 跨场景跳转用)。
2. **`frontend/src/app/layouts/dashboard-layout.tsx`**:新增 `HtmlPreviewGate`(lazy,`activePreview?.kind === "html"`),与 markdown gate 并列;markdown gate 改为检查 `kind === "markdown"`。
3. **`frontend/src/components/chat/message-row.tsx`**:`previewable = isMarkdownAttachment(att) || isHtmlAttachment(att)`;`tooLarge` 按对应类型取阈值。`file-card.tsx` 无需改动(`onPreview` 已是通用入口)。
4. **`frontend/src/pages/dashboard/chat-conversation.tsx` / `activity-detail.tsx`**:`handleJumpToSection` 把 `att.quotedText` 作为 `scrollToQuote` 透传给 `openFilePreview`。
5. **`frontend/src/components/workspace/workspace-file-panel.tsx`**:`isHtmlFile(name)`(扩展名集合 `.html/.htm/.xhtml`)分支 → `<HtmlFileView>`。
6. **`frontend/src/locales/en-US.json` / `zh-CN.json`**:新增 `preview.html-add-comment`("Add comment" / "添加评论");其余文案全部复用现有 `preview.*` / `comments-*`。
7. **顺带合规改动**:三个浮层(`html`/`markdown`/`image` preview overlay)把裸 `z-10` 换为共享层原语 `LAYER_SURFACE_CLASS`(来自 `src/components/ui/layer.ts`,值即 `"z-10"`,输出无变化),通过 `check-react-layering.mjs` 门禁。

### 零改动

proto、后端、DB(`Attachment` 锚点字段与 `resolveAttachments`/`SendMessage` 已就绪;workspace policy 已含 `.html`)。

## 验证

1. **单测**(`pnpm --dir frontend test`):33 个文件 / 126 个用例全部通过,含 `html-file.test.ts` 的 13 个用例。
2. **静态检查**:`pnpm --dir frontend biome:check`(212 文件,无问题)、`pnpm --dir frontend type-check`(exit 0)。
3. **layering 门禁**:`node scripts/check-react-layering.mjs` 全部通过。
4. **手动 E2E**(真实浏览器):chat 上传 `.html` → 浮层 iframe 渲染、页面 JS 正常执行、链接新标签打开且预览不导航走、Esc 关闭;开启评论 → 拖选文本 → 添加评论 → thread 中评论带锚点 chip、从 thread 点锚点可重开预览并滚动高亮;>10MiB 文件 too-large 提示;workspace 内联渲染正常。**用户已实测确认无问题。**

## 已知限制与后续增强

- `about:srcdoc` 下相对路径资源不可解析(决策 2 已文档化);
- 选区跨 iframe 边界时可能不完整(极边缘场景);
- 未存储选区文本偏移,不做"精确重高亮"(仅 quote + nearY 定位);后续可在 `selection` 消息中附加偏移并在 `locate` 中做精确匹配;
- workspace 内联视图为只读,无评论(与 markdown 内联一致)。

## 受影响 / 新增文件清单

**新增**:
- `frontend/src/lib/html-file.ts`(含桥接脚本常量)
- `frontend/src/lib/html-file.test.ts`
- `frontend/src/components/preview/html-preview-overlay.tsx`
- `frontend/src/components/preview/html-comments-aside.tsx`
- `frontend/src/components/workspace/html-file-view.tsx`

**修改**:
- `frontend/src/stores/preview.ts`、`frontend/src/stores/types.ts`
- `frontend/src/app/layouts/dashboard-layout.tsx`
- `frontend/src/components/chat/message-row.tsx`
- `frontend/src/components/preview/markdown-preview-overlay.tsx`、`image-preview-overlay.tsx`(LAYER_SURFACE_CLASS 合规)
- `frontend/src/components/workspace/workspace-file-panel.tsx`
- `frontend/src/pages/dashboard/chat-conversation.tsx`、`activity-detail.tsx`
- `frontend/src/locales/en-US.json`、`zh-CN.json`

**零改动**:proto、后端、DB。
