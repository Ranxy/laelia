# 前端 Markdown 渲染栈迁移至 Streamdown 设计与分布式执行方案

## 1. 文档状态

- 状态：设计方案，尚未开始实施
- 目标：将前端的 `markstream-react`、`stream-markdown` 替换为 Streamdown
- 目标依赖版本：`streamdown@2.6.0`；实施时应重新确认兼容的插件版本并使用精确版本
- 适用范围：`frontend/` 中所有 Markdown 渲染、代码块、mention、Markdown 文件预览和相关测试
- 关联文档：`docs/plan/markdown-preview-outline-comments.md`
- 迁移原则：本方案中关于 Markdown 渲染器的决策优先于关联文档中“继续使用 markstream-react”的历史决策

## 2. 摘要

这不是一次简单的 import 替换。当前 Markdown 渲染链路同时负责聊天消息、历史消息懒加载、代码块、`@mention` 自定义节点、Markdown 文件目录与锚点、Workspace 预览、Command 输出和 Final Summary。

推荐先建立项目自己的 Markdown 适配层，再分批迁移消费者，最后删除旧依赖：

```text
业务组件
    ↓
项目 Markdown 适配层
    ↓
Streamdown + 可选官方插件
```

业务组件不应直接 import `streamdown`。这样可以把第三方 API、代码块实现、安全策略、主题和未来的渲染器替换限制在适配层内。

本方案将实施拆为以下可分布执行的 Phase：

```text
P0 基线与契约冻结
 ├─ P1 Streamdown API Spike
 ├─ P2 Markdown 适配层骨架
 └─ P3 渲染回归 fixture 与测试基础设施
       ↓
P4 mention 适配（依赖 P1 + P2）
P5 代码块适配（依赖 P1 + P2）
       ↓
P6 聊天消息迁移（依赖 P4 + P5）
P7 静态场景迁移（依赖 P2；推荐 P4/P5 完成后集成）
       ↓
P8 CSS、构建、性能与安全集成
       ↓
P9 删除旧栈、全量验证与发布准备
```

P1、P2、P3 可以并行；P4 与 P5 可以并行；P6 与 P7 在各自前置条件满足后可以由不同执行者并行，但最终只能由 P8 统一处理共享 CSS、依赖和构建配置。

## 3. 当前实现盘点

### 3.1 依赖

`frontend/package.json` 当前包含：

```json
{
  "markstream-react": "^0.0.55",
  "stream-markdown": "^0.0.16"
}
```

`stream-markdown` 是旧渲染链的底层依赖。迁移完成后两个包都应删除，不能只删除 `markstream-react`。

Streamdown 2.6.0 的实际能力包括：

- `Streamdown` React 组件；
- `mode="static"` / `mode="streaming"`；
- `isAnimating`；
- `components`；
- `remarkPlugins` / `rehypePlugins`；
- `plugins`；
- `allowedTags`；
- `literalTagContent`；
- `controls`；
- `lineNumbers`；
- `linkSafety`；
- `parseIncompleteMarkdown`；
- `animated`；
- `codeBlockMaxHeight`；
- `streamdown/styles.css`。

Streamdown 是 React 18/19 兼容的、针对 AI 流式 Markdown 优化的渲染器，但不是 `markstream-react` 的 API 兼容替代品。

### 3.2 生产代码入口

| 文件 | 当前用途 | 迁移重点 |
|---|---|---|
| `frontend/src/lib/markdown.tsx` | 全局 custom component 注册、`FinalSummary` | 改造成适配层，移除全局 registry |
| `frontend/src/components/chat/message-row.tsx` | 聊天 Markdown | mention、memo、lazy、性能 |
| `frontend/src/components/chat/lazy-markdown.tsx` | 历史消息延迟渲染 | 保留业务懒加载并重新测量 |
| `frontend/src/components/preview/markdown-preview-overlay.tsx` | Markdown 文件全屏预览 | 标题 ID、目录、评论锚点 |
| `frontend/src/components/workspace/workspace-file-panel.tsx` | Workspace Markdown | 静态渲染、安全策略 |
| `frontend/src/components/command-events/command-event-inspector.tsx` | Command 输出预览 | 大文本、代码块 |
| `frontend/src/main.tsx` | 引入旧 CSS | 替换为 Streamdown CSS |
| `frontend/src/assets/css/tailwind.css` | `.markstream-chat` 样式 | 改为项目级 Markdown 样式 |
| `frontend/vite.config.ts` | 旧包构建注释和分包说明 | 清理并重新验证分包 |

### 3.3 现有业务行为

#### 聊天消息

`MessageRow` 使用 `MemoMarkdown` 和 `LazyMarkdown`：

- off-screen 历史消息先显示纯文本 fallback；
- 进入 `IntersectionObserver` root margin 后才渲染 Markdown；
- `eager` 用于当前或小规模会话；
- 通过 `overflow-anchor` 处理 fallback 替换造成的滚动跳动；
- 当前消息内容已经是 committed content，旧代码注释说明实时 streaming pipeline 已退出。

因此聊天消息默认应使用 Streamdown 的静态模式，而不是因为新库支持 streaming 就自动开启 streaming 模式。

#### Mention

`frontend/src/components/chat/mentions.ts` 将文本改写为自定义标签：

```html
<mention type="user" id="users/alice" name="alice">@alice</mention>
```

旧实现通过全局 `setCustomComponents` 将 `mention` 映射到 `MentionChip`。该文本改写逻辑应保持不变，只迁移标签解析和组件注入方式。

#### Markdown 文件目录和评论

`frontend/src/lib/markdown-file.ts` 的 `buildOutline()` 在渲染后的 DOM 上扫描 `h1` 到 `h6`，自己分配：

```text
md-${index}-${slug}
```

并负责目录编号、评论 section anchor 和跨场景跳转。迁移后不能依赖 Streamdown 自动生成 heading id；`buildOutline()` 仍应是唯一的项目 ID 分配入口。

## 4. 目标架构

### 4.1 适配层 API

建议将 `frontend/src/lib/markdown.tsx` 改造成稳定的项目级 API；是否进一步移动到 `src/components/markdown/` 可在实现阶段决定，但不要在同一批次中同时进行无必要的目录重命名。

推荐 API：

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

适配层负责：

- Streamdown 组件调用；
- variant 到 Streamdown props 的映射；
- mention 的 `components`、`allowedTags`、`literalTagContent`；
- 代码插件和代码块控制；
- 链接安全策略；
- 公共 className；
- 静态/流式模式默认值；
- Streamdown 翻译文案；
- 主题和 CSS 约束。

业务组件不应再传递旧的：

```text
customId
final
fade
smoothStreaming
batchRendering
deferNodesUntilVisible
customHtmlTags
```

如果其中某个行为确实需要保留，应由适配层用新的 Streamdown API 或项目逻辑实现，而不是把旧 API 名字继续泄漏到业务组件。

### 4.2 不使用全局 custom component registry

旧方式：

```tsx
setCustomComponents({ ... });
```

目标方式：

```tsx
<Streamdown
  components={components}
  allowedTags={allowedTags}
  literalTagContent={literalTagContent}
>
  {content}
</Streamdown>
```

这样可以避免模块导入顺序、HMR、测试隔离和多个渲染实例之间的全局状态问题。

### 4.3 场景默认值

| 场景 | `mode` | 动画 | mention | HTML 策略 |
|---|---|---:|---:|---|
| chat | `static` | 关闭 | 按调用者开启 | 只允许 `mention` |
| 正在真正流式输出的消息 | `streaming` | 开启 | 按调用者开启 | 只允许 `mention` |
| Markdown preview | `static` | 关闭 | 关闭 | 默认 sanitize |
| Workspace | `static` | 关闭 | 关闭 | 默认 sanitize |
| Command preview | `static` | 关闭 | 关闭 | 默认 sanitize |
| Final Summary | `static` | 关闭 | 关闭 | 默认 sanitize |

## 5. 分布式执行规则

### 5.1 执行者边界

每个 Phase 应由一个执行者负责其文件范围，避免多个执行者同时修改同一个目标文件。

- 可以并行：不同文件、不同包、不同测试 fixture。
- 不应并行：同一个 `package.json`、`pnpm-lock.yaml`、`tailwind.css`、`vite.config.ts`、`lib/markdown.tsx` 的多次编辑。
- 依赖安装和 lockfile 更新只能由集成执行者完成。
- 共享适配层 API 确认前，不应让消费者迁移者自行猜测 props。
- 每个 Phase 结束时必须提交或交付清单、测试结果和未解决风险；是否创建 git commit 由上层流程决定。

### 5.2 文件所有权建议

| 工作包 | 主要文件所有权 |
|---|---|
| P0 | 只读分析、`docs/` 中的基线记录 |
| P1 | 临时目录或独立 spike 文件；不得修改生产入口 |
| P2 | `frontend/src/lib/markdown.tsx` 及适配层新文件 |
| P3 | `frontend/src/components/markdown/fixtures/`、适配层测试工具 |
| P4 | `frontend/src/components/chat/mentions.ts`、mention 集成测试；如需适配层改动须回交 P2 |
| P5 | 代码块组件、代码块测试、插件配置；适配层共享配置须回交 P2 |
| P6 | `message-row.tsx`、chat/thread 相关测试、必要的 `lazy-markdown.tsx` 注释 |
| P7 | preview overlay、workspace panel、command inspector、对应测试 |
| P8 | `main.tsx`、`tailwind.css`、`vite.config.ts`、构建和性能报告 |
| P9 | `package.json`、`pnpm-lock.yaml`、全局残留清理和最终验证 |

## 6. Phase 详细计划

## Phase P0：基线与契约冻结

### 目标

在任何生产代码迁移前，记录旧渲染器行为、性能和构建边界，冻结不能随意改变的业务契约。

### 可执行任务

1. 运行现有前端验证：

   ```bash
   pnpm --dir frontend biome:check
   pnpm --dir frontend type-check
   pnpm --dir frontend test
   pnpm --dir frontend build
   ```

2. 记录当前生产构建的 chunk 列表和大小。
3. 记录聊天入口、Markdown preview、Workspace preview 的初始加载行为。
4. 保存代表性 Markdown fixture：普通文本、标题、列表、表格、代码、未闭合 fence、HTML、中文、mention、超长文本。
5. 记录以下不可回归契约：
   - mention 的 `data-mtype`、`data-mid`、`data-mname`；
   - heading ID 格式；
   - `buildOutline()` 的编号和顺序；
   - chat 的 lazy fallback 和滚动锚定；
   - Copy Markdown 仍复制原始 Markdown；
   - workspace 和 preview 不执行不可信脚本。

### 输入

当前工作区代码、现有测试、浏览器手工验证环境。

### 输出

- 基线验证结果；
- 构建产物基线；
- Markdown fixture 清单；
- 不可回归契约列表。

### 验收

基线命令全部执行；如果现有代码已有失败，必须区分“迁移前已有失败”和“迁移引入失败”。

### 回滚

只读 Phase，无生产代码回滚需求。

## Phase P1：Streamdown API Spike

### 目标

通过最小独立实验确认 Streamdown 2.6.0 的实际 API，不让多个执行者根据猜测实现。

### 可并行任务

#### P1-A：基础渲染实验

验证：

- `Streamdown` 静态渲染；
- GFM 表格、task list、删除线；
- 标题和 DOM 结构；
- 未闭合 Markdown；
- `mode="streaming"` 和 `isAnimating`。

#### P1-B：自定义 HTML / mention 实验

验证：

- `components` 自定义 `mention`；
- `allowedTags` 属性保留；
- `literalTagContent`；
- sanitize 对未授权 HTML 的处理；
- mention 子内容中的 Markdown 是否被当作纯文本。

#### P1-C：代码插件实验

验证：

- `@streamdown/code` 的兼容版本；
- 支持的语言；
- light/dark theme；
- copy control；
- 行号默认值；
- 未闭合 code fence；
- 代码插件对 bundle 的影响。

#### P1-D：安全和链接实验

验证：

- `javascript:`、`data:`、`http:`、`https:`、`mailto:`；
- `linkSafety` 行为；
- 图片和 HTML；
- 外部链接的 `target` / `rel`；
- 是否需要项目自定义 modal。

### 输入

Streamdown 官方类型声明、README、官方文档和 P0 fixture。

### 输出

一份 API spike 记录，至少包含：

- 可用 props 和实际 DOM；
- mention 配置；
- code plugin 版本和配置；
- 安全默认值；
- 已知不能等价迁移的旧能力；
- 推荐的适配层 API 映射。

### 验收

P2、P4、P5 的执行者可以直接引用 spike 结论，不需要重新猜测 Streamdown API。

### 回滚

删除临时 spike 文件即可，不修改生产代码。

## Phase P2：项目 Markdown 适配层骨架

### 目标

创建新的项目级 Markdown API，使后续消费者与 Streamdown 解耦。

### 文件范围

- `frontend/src/lib/markdown.tsx`；或适配层新文件；
- 适配层相关类型和配置文件；
- 不修改 chat、preview、workspace、command 消费者。

### 实施内容

1. 引入 `Streamdown`。
2. 实现 `MarkdownRenderer`。
3. 实现 `MarkdownVariant` 和统一 props。
4. 建立基础 className 约定：`.markdown-content`。
5. 实现 static/streaming 默认映射。
6. 暂时可以不接入最终 code plugin，但必须为 plugin 注入留出明确位置。
7. 预留 mention 配置入口，不在本 Phase 猜测最终属性结构。
8. 保持 `FinalSummary` 的外部 API 不变。
9. 不再使用 `setCustomComponents`。

### 输出

适配层可以独立渲染普通 Markdown，并且不要求业务组件知道 Streamdown 细节。

### 验收

- 适配层单元测试通过；
- TypeScript 通过；
- 适配层可以被测试 mock；
- 仍未删除旧依赖；
- 现有消费者行为不被修改。

### 回滚

删除适配层新增代码并恢复旧 `lib/markdown.tsx`；由于消费者尚未迁移，回滚范围小。

## Phase P3：回归 Fixture 与测试基础设施

### 目标

建立不依赖具体页面的 Markdown 回归测试，为 P4-P9 提供统一验证工具。

### 可并行任务

#### P3-A：fixture

新增或整理：

```text
frontend/src/components/markdown/fixtures/
├── basic.md
├── headings.md
├── lists-and-tables.md
├── code.md
├── mentions.md
├── unsafe-html.md
├── incomplete-stream.md
├── cjk.md
└── large.md
```

#### P3-B：适配层测试工具

提供统一 mock/stub，避免各页面测试继续重复 mock 第三方包：

```tsx
vi.mock("@/lib/markdown", () => ({
  MarkdownRenderer: ({ content }: { content: string }) => <>{content}</>,
  FinalSummary: ({ content }: { content: string }) => <>{content}</>,
}));
```

实际路径以 P2 最终 API 为准。

#### P3-C：DOM 回归测试

验证 Streamdown 渲染后的：

- 标题数量和 tag；
- 段落、列表、表格；
- fenced code；
- HTML sanitize；
- fixture 中的特殊字符。

### 输出

通用 Markdown renderer 测试、fixture 和测试 mock 约定。

### 验收

测试可以在不 mount 整个 Dashboard 的情况下验证 Markdown 适配层的关键行为。

### 回滚

只删除新增 fixture 和测试工具，不影响生产代码。

## Phase P4：Mention 适配

### 前置依赖

- P1-B 完成；
- P2 适配层骨架完成；
- P3 的测试工具可用。

### 文件范围

- `frontend/src/components/chat/mentions.ts`：原则上保持匹配算法不变，只更新必要注释；
- `frontend/src/lib/markdown.tsx` 或适配层 mention 配置；
- mention 集成测试；
- 不修改 `message-row.tsx` 的调用迁移部分，由 P6 负责。

### 实施内容

使用 Streamdown：

```tsx
const mentionOptions = {
  allowedTags: {
    mention: ["type", "id", "name", "label"],
  },
  literalTagContent: ["mention"],
  components: {
    mention: MentionChip,
  },
};
```

`MentionChip` 改为接受 HTML 属性，而不是依赖 markstream 的私有 `node.attrs` 结构：

```tsx
interface MentionChipProps {
  type?: string;
  id?: string;
  name?: string;
  label?: string;
  children?: React.ReactNode;
}
```

保持以下现有行为：

- `contentWithMentionTags()` 的输入输出协议；
- mention 的边界匹配；
- 多次出现同一 mention；
- `data-mtype`、`data-mid`、`data-mname`；
- 键盘 Enter/Space 激活；
- 事件委托点击。

### 必须测试

1. mention 在普通段落中；
2. mention 在粗体和列表中；
3. mention 多次出现；
4. handle 和 display name 不同；
5. 代码块中的 `@name` 不被替换；
6. email 不被误识别；
7. 未授权标签被 sanitize；
8. mention 属性不丢失；
9. mention 子文本不再次解析 Markdown；
10. 事件委托数据和键盘行为不变。

### 输出

适配层支持 mention 的合并实现，P6 可以直接使用 `mentionAware`。

### 验收

mention 集成测试通过；没有放宽任意 HTML 的 sanitize 边界。

### 回滚

P6 尚未集成前，可以删除 mention 配置并继续使用旧消费者；`mentions.ts` 的纯字符串算法不应回滚或重写。

## Phase P5：代码块适配

### 前置依赖

- P1-A、P1-C 完成；
- P2 适配层骨架完成。

### 文件范围

- 适配层 code plugin 配置；
- 如有需要，新增项目代码块组件；
- 代码块 fixture 和测试；
- 不修改消息消费者，由 P6 负责。

### 首选方案

优先使用官方 `@streamdown/code` 插件，并通过 Streamdown controls 配置：

```tsx
<Streamdown
  plugins={{ code }}
  controls={{
    code: {
      copy: true,
      download: false,
    },
  }}
  lineNumbers={false}
/>
```

实际 props 以 P1-C 的验证为准。

### 需要明确的产品行为

- 是否显示行号；
- 是否显示下载按钮；
- 是否允许最大高度和滚动；
- 是否保留深浅色主题；
- 复制是否保留尾部换行；
- 未知语言的 fallback；
- 未闭合 fence 的显示；
- 是否需要折叠；
- 是否需要沿用旧 `.markstream-chat pre` 的视觉样式。

如果官方插件不能满足复制、布局或主题要求，再实现项目自己的代码块组件。不要直接复制 `MarkdownCodeBlockNode` 的私有实现。

### 性能要求

必须检查：

- 代码插件是否进入初始入口 chunk；
- Shiki 语言数据是否过大；
- 100 条消息中包含多个代码块时的 mount 时间；
- 未知语言是否触发异常或同步阻塞。

### 输出

适配层可以渲染代码块、复制代码，并与项目主题一致。

### 验收

JavaScript、TypeScript、Go、JSON、Shell 至少各有一个 fixture；复制、未知语言、未闭合 fence、深浅色主题测试通过。

### 回滚

可以暂时关闭 code plugin，回退到 Streamdown 的基础 code 输出；不能重新引入 markstream 的代码块私有组件。

## Phase P6：聊天消息迁移

### 前置依赖

- P4 mention 适配完成；
- P5 code block 适配完成；
- P3 测试工具完成。

### 文件范围

- `frontend/src/components/chat/message-row.tsx`；
- `frontend/src/components/chat/lazy-markdown.tsx` 的注释和必要适配；
- chat/thread/channel 相关测试；
- 不修改 preview、workspace、command 页面。

### 实施内容

将旧调用替换为项目 API：

```tsx
<MarkdownRenderer
  content={content}
  variant="chat"
  mentionAware={mentionAware}
  mode="static"
/>
```

保留：

- `MemoMarkdown`；
- `LazyMarkdown`；
- eager/non-eager 分层；
- IntersectionObserver root 和 root margin；
- fallback 纯文本；
- overflow anchor 处理；
- mention 事件委托；
- raw Markdown copy。

删除或改名：

- `MarkdownRender` 命名；
- `customId`；
- `customHtmlTags`；
- 对 markstream 全局 registry 的注释；
- 已不再存在的 streaming state 说明。

### 必须验证

- DM；
- channel；
- thread；
- reply preview；
- 当前消息 eager 渲染；
- 大历史非 eager 渲染；
- IntersectionObserver 不存在时的 jsdom fallback；
- prepend history 时滚动位置；
- fallback 替换时滚动位置；
- mention 点击和键盘操作；
- attachments、图片、task/tool 状态不受影响；
- `MemoMarkdown` 不因行级无关状态更新而重复创建内容。

### 输出

聊天和 thread 完全改用项目 Markdown 适配层，业务代码不再依赖旧 renderer。

### 验收

chat 相关测试通过，浏览器中主聊天、频道和 thread 均完成手工验收；P0 的性能基线没有明显恶化。

### 回滚

保留 P2 适配层和旧依赖时，可以将 `MessageRow` 恢复为旧调用；不得通过恢复全局 registry 来解决单个测试问题。

## Phase P7：静态场景迁移

### 前置依赖

- P2 完成；
- 推荐 P4、P5 完成；
- P6 可与本 Phase 并行，但不能共享修改同一文件。

### 可并行工作包

#### P7-A：Markdown preview

文件：

- `frontend/src/components/preview/markdown-preview-overlay.tsx`；
- preview 相关测试。

使用：

```tsx
<MarkdownRenderer
  content={active.content}
  variant="preview"
  mode="static"
/>
```

保留：

- `contentRef`；
- `buildOutline()`；
- `scrollToAnchorId`；
- comments aside；
- selection anchor。

删除旧 renderer 专属 props：

```text
customId
final
fade
batchRendering
deferNodesUntilVisible
```

#### P7-B：Workspace Markdown

文件：

- `frontend/src/components/workspace/workspace-file-panel.tsx`；
- workspace 相关测试。

使用 `variant="workspace"`、`mode="static"`。默认保留 sanitize，不因为文件来自 Workspace 就信任任意 HTML。

#### P7-C：Command 输出预览

文件：

- `frontend/src/components/command-events/command-event-inspector.tsx`；
- command inspector 相关测试。

重点验证数百 KB 输出、长单行、大量代码块、未闭合 Markdown。

#### P7-D：Final Summary

文件：

- `frontend/src/lib/markdown.tsx` 或适配层；
- `command-list`、`command-detail` 只在需要更新 import 时修改；
- 对应 summary 测试。

保持外部调用：

```tsx
<FinalSummary content={summary} />
```

### 共同验收

- 标题数量、顺序和 tag 正确；
- `buildOutline()` 仍生成 `md-${index}-${slug}`；
- 重复标题 ID 不冲突；
- 目录跳转正常；
- comment selection 仍定位最近 heading；
- cross-scenario anchor 正常；
- workspace 不执行脚本；
- command 大输出不破坏布局；
- summary 样式和 chat Markdown 统一。

### 回滚

每个 P7 子工作包可独立回退到旧 renderer，前提是旧依赖尚未在 P9 删除。

## Phase P8：CSS、构建、性能与安全集成

### 前置依赖

- P6、P7 的生产消费者迁移完成；
- P4、P5 的配置已稳定。

### 文件范围

- `frontend/src/main.tsx`；
- `frontend/src/assets/css/tailwind.css`；
- `frontend/vite.config.ts`；
- 依赖和构建验证报告；
- 不在本 Phase 修改业务 Markdown 调用点。

### CSS 迁移

将：

```tsx
import "markstream-react/index.css";
```

替换为：

```tsx
import "streamdown/styles.css";
```

将 `.markstream-chat` 改为项目级命名，例如：

```text
.markdown-content
```

清理所有 `markstream` 相关 CSS 注释和选择器。

Streamdown 使用 Tailwind utility 时，需要在 Tailwind v4 CSS 中加入实际正确相对路径的 source，例如：

```css
@source "../../../node_modules/streamdown/dist/*.js";
```

如果安装官方代码插件，还需要加入其 dist source。路径必须根据 CSS 文件位置确认，不能直接照抄 Next.js 文档示例。

### 主题策略

优先不为了 Streamdown 默认控件大范围改造项目主题变量。对于不需要的高级控件关闭它们；如果启用官方 controls，再决定是否提供 Streamdown/shadcn CSS 变量映射。

### 构建策略

1. 删除 Vite 中关于 `markstream-react` / `stream-markdown` 的旧注释。
2. 不要立即把 Streamdown 强制放入手工 vendor chunk。
3. 先观察默认 Rolldown/Rollup 分包。
4. 确认 Markdown preview 仍然按需加载。
5. 确认代码 plugin 没有意外进入初始入口。
6. 只有真实构建数据证明必要时，才调整 `manualChunks`。

### 性能验证

与 P0 基线比较：

- 首屏可交互时间；
- chat 入口 JS 体积；
- preview 首次打开延迟；
- 100 条消息初始 mount；
- 100 KB 和 500 KB Markdown 解析；
- 多代码块消息；
- 历史滚动掉帧；
- lazy fallback 替换造成的滚动位移。

### 安全验证

至少验证：

- `javascript:` 不可执行；
- 未授权 HTML 被过滤；
- 脚本、事件属性、iframe 不执行；
- mention 只允许规定属性；
- workspace 内容不执行脚本；
- 外部链接符合项目确认/打开策略；
- 图片策略符合产品预期。

### 输出

统一样式、构建、性能和安全报告。

### 验收

生产构建成功；初始入口和 Markdown preview chunk 符合预期；安全 fixture 通过；性能在团队设定阈值内。

## Phase P9：删除旧栈、全量验证与发布准备

### 前置依赖

- P8 完成并通过集成门禁；
- 所有生产调用点和测试已迁移；
- 已确认无回退旧 renderer 的需要。

### 文件范围

- `frontend/package.json`；
- `frontend/pnpm-lock.yaml`；
- 全仓 Markdown 相关引用；
- 发布验证记录。

### 实施内容

1. 删除：

   ```text
   markstream-react
   stream-markdown
   ```

2. 删除旧 CSS import。
3. 删除 `setCustomComponents`、`MarkdownCodeBlockNode` 和旧 registry 测试。
4. 更新所有测试 mock，使其 mock 项目适配层而不是第三方库。
5. 全局搜索残留：

   ```bash
   rg -n --hidden -S \
     'markstream-react|stream-markdown|markstream|MarkdownRender|setCustomComponents|MarkdownCodeBlockNode' \
     frontend \
     --glob '!node_modules' \
     --glob '!dist'
   ```

6. 检查 lockfile 中是否还存在旧包及其仅由旧包引入的传递依赖。
7. 更新相关设计文档中的历史 renderer 描述。

### 最终验证命令

按项目约定执行：

```bash
pnpm --dir frontend biome:check
pnpm --dir frontend lint
pnpm --dir frontend type-check
pnpm --dir frontend test
pnpm --dir frontend build
```

如 `biome:check` 已覆盖格式和 lint，仍应保留项目规定的独立命令作为最终记录。

### 最终手工验收

- 主聊天；
- Channel；
- DM；
- Thread；
- Markdown preview；
- Workspace Markdown；
- Command output preview；
- Final Summary；
- mention 点击和键盘操作；
- 代码复制；
- 目录和评论锚点；
- 深浅色主题；
- 中文和中英文混排；
- 外部链接和危险 HTML。

### 回滚

P9 是高影响但可回滚的依赖删除阶段。若发布前发现问题：

1. 恢复 `package.json` 和 lockfile 中旧依赖；
2. 恢复旧 CSS import；
3. 将适配层保留在代码中，但把消费者切回旧实现；
4. 不使用 destructive git reset；
5. 修复后重新通过 P8 集成门禁，再执行 P9。

## 7. Phase 依赖和并行调度

### 7.1 推荐调度

| 时间段 | 可并行工作 | 不能开始的工作 |
|---|---|---|
| T0 | P0 | 其他实现工作依赖 P0 的契约和 fixture |
| T1 | P1-A/B/C/D、P2、P3 | P4/P5 等待 P1/P2 |
| T2 | P4、P5 | P6 等待两者；P7 可先做非生产 fixture，但不应集成 |
| T3 | P6、P7-A/B/C/D | P8 等待所有消费者完成 |
| T4 | P8 | P9 等待构建、安全、性能通过 |
| T5 | P9 | 发布前不得再有未验证的旧包残留 |

### 7.2 最小人员配置

#### 1 人

严格按 P0 → P1 → P2 → P3 → P4/P5 → P6/P7 → P8 → P9 顺序执行。

#### 2 人

- 执行者 A：P1、P2、P4、P5、P8/P9；
- 执行者 B：P3、P6、P7；
- P8 之前由 A 统一收敛共享配置。

#### 3 人以上

- 执行者 A：Streamdown API、适配层和依赖；
- 执行者 B：mention、代码块和 Markdown fixture；
- 执行者 C：chat/lazy/message 流程；
- 执行者 D：preview/workspace/command 静态场景；
- 由集成负责人统一执行 P8/P9。

### 7.3 跨工作包接口

P2 必须先公布：

```ts
MarkdownRendererProps
MarkdownVariant
mentionAware 的语义
static/streaming 默认策略
```

P4 必须交付：

```text
mention 允许的标签和属性
MentionChip 的 DOM 数据属性契约
mention 集成测试结果
```

P5 必须交付：

```text
code plugin 版本
代码块 controls 配置
主题和行号策略
bundle 影响
```

P6/P7 不应重新解释这些接口，只消费已冻结的适配层 API。

## 8. 统一测试与完成标准

### 8.1 依赖和代码清理

- [ ] 生产代码不再 import `markstream-react`。
- [ ] 生产代码不再 import `stream-markdown`。
- [ ] 测试不再 mock `markstream-react`。
- [ ] 不存在 `setCustomComponents`。
- [ ] 不存在 `MarkdownCodeBlockNode`。
- [ ] 业务组件只依赖项目 Markdown 适配层。
- [ ] `.markstream-chat` 和 `markstream` 旧注释已清理。

### 8.2 功能

- [ ] chat Markdown 正常。
- [ ] channel mention 正常。
- [ ] thread mention 正常。
- [ ] 代码块高亮和复制正常。
- [ ] 表格、task list、blockquote 正常。
- [ ] Markdown 文件预览正常。
- [ ] Workspace Markdown 正常。
- [ ] Command 输出预览正常。
- [ ] Final Summary 正常。
- [ ] 标题目录、heading ID、评论锚点正常。
- [ ] 外部链接策略符合预期。
- [ ] 不安全 HTML 被过滤。

### 8.3 性能

- [ ] chat 首屏没有超出 P0 设定阈值。
- [ ] Markdown preview 仍按需加载。
- [ ] 历史消息 lazy rendering 仍生效。
- [ ] 代码插件没有意外进入初始入口 chunk。
- [ ] 大文本和多代码块不造成不可接受的主线程阻塞。
- [ ] fallback 替换和历史 prepend 不造成明显滚动跳动。

## 9. 风险清单

### 高风险

#### Mention 被 sanitize 丢失

缓解：严格 `allowedTags`、`literalTagContent`、真实 renderer 集成测试，不放开任意 HTML。

#### 标题 DOM 或 ID 行为变化

缓解：继续使用 `buildOutline()` 自己分配 ID，增加真实 DOM 回归测试。

#### 代码插件造成 bundle 和性能回退

缓解：P1-C 先测 chunk，P6 保留 LazyMarkdown，P8 做与基线的长文本和多代码块比较。

#### CSS 视觉变化

缓解：统一 `.markdown-content`，用 fixture 和浏览器验收分别检查 chat、preview、workspace、summary。

### 中风险

- 未闭合 Markdown 的中间状态与旧实现不同；
- 复制行为的尾部换行或按钮 DOM 变化；
- 外部链接 target/rel 或安全确认变化；
- CJK 粗体、删除线、自动链接边界变化；
- Streamdown 默认最大 code/table 高度与旧布局不同；
- `rehype-raw` 和 sanitize 对历史内容中的 HTML 行为变化。

## 10. 提交和交付建议

建议按以下逻辑拆分提交或交付单元，每个单元都应可独立验证：

```text
refactor(frontend): add streamdown markdown adapter
 test(frontend): add markdown renderer fixtures and contract tests
refactor(frontend): migrate markdown mention rendering
refactor(frontend): migrate markdown code blocks
refactor(frontend): migrate chat markdown rendering
refactor(frontend): migrate static markdown previews
refactor(frontend): update markdown styles and build splitting
refactor(frontend): remove markstream dependencies
```

不要在旧消费者还未迁移时删除旧依赖；也不要把所有 Phase 压成一个无法定位回归原因的超大提交。

## 11. 最终决策

1. 采用 Streamdown 作为唯一前端 Markdown 渲染栈。
2. 业务组件不直接依赖 Streamdown，统一通过项目 Markdown 适配层调用。
3. 保留 `LazyMarkdown`，因为它解决的是聊天列表的业务级渲染门控和滚动稳定性问题。
4. 保持 `mentions.ts` 的匹配和文本改写协议，使用 Streamdown 的 `allowedTags`、`literalTagContent` 和 `components` 迁移渲染。
5. 优先使用官方 code plugin；只有官方能力无法满足产品要求时才实现项目级代码块组件。
6. 继续由 `buildOutline()` 自己生成 heading ID，不依赖第三方 slug 规则。
7. 静态内容使用 `mode="static"`；只有真正实时接收中的消息使用 streaming 模式。
8. 在 P8 前不统一修改共享 CSS、Vite 分包和依赖；这些变更由集成阶段一次性完成。
9. 在 P9 之前保留旧依赖作为可回退路径；完成全量验证后再删除 `markstream-react` 和 `stream-markdown`。
