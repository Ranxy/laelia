# Laelia PWA 支持设计文档

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：第 3 节记录的 PWA 方案（manifest/图标/App Shell 预缓存/启动即注册/离线降级/服务端缓存头）均已落地，第 1 节“现状”表已更新为当前事实，并修正了若干文件路径与实现细节。

> 目标：在“用户自托管、拥有自己域名”的部署模型下，为 Laelia 提供完整的 PWA 能力：可安装（installable）、应用壳（App Shell）离线可用、以及已有的 Web Push 通知能力与离线降级体验的整合。

---

## 1. 背景与现状梳理（基于代码，2026-09-06 核对）

| 事实 | 代码/文件依据 |
| --- | --- |
| 前端是 React 19 + Vite 8 + TypeScript 的 SPA，构建产物在 `frontend/dist` | `frontend/package.json`、`frontend/vite.config.ts` |
| 前端被嵌入 Go Manager 二进制中（`-tags embed_frontend`），由 Echo 以 SPA fallback 方式同源服务 | `backend/manager/server/server_frontend_embed.go`、`scripts/build_laelia.sh` |
| 前端资源路径是**绝对路径**（`/assets/...`、`/sw.js`），假设部署在域名**根路径** | `frontend/dist/index.html`、`server_frontend_embed.go` |
| Service Worker 源码是 `frontend/sw/sw.ts`（Workbox 预缓存 + Web Push 推送逻辑合并在同一个 SW），构建输出为 `dist/sw.js`；`frontend/public/` 下不再有手写 `sw.js` | `frontend/sw/sw.ts`、`frontend/vite.config.ts`（`VitePWA` `injectManifest`） |
| SW **应用启动即注册**（仅生产构建），`web-push.ts` 复用同一注册 | `frontend/src/lib/pwa.ts` 的 `registerServiceWorker()`（`main.tsx` 调用）、`frontend/src/lib/web-push.ts` 的 `getRegistration()` |
| `manifest.webmanifest`、PWA 图标（`icon-192/512`、`icon-maskable-512`、`apple-touch-icon`）、App Shell 预缓存、离线降级（`OfflineBanner`）**均已存在** | `frontend/public/manifest.webmanifest`、`frontend/public/icons/`、`frontend/src/components/offline-banner.tsx`、`frontend/index.html` |
| 后端已有 `securityHeadersMiddleware()`，设置了 `Strict-Transport-Security`、`X-Frame-Options: DENY` 等；另有 `pwaStaticHeadersMiddleware()` 处理 PWA 静态资源的 MIME/缓存头 | `backend/manager/server/echo_routes.go`、`backend/manager/server/server_frontend_embed.go` |
| 已具备 Web Push 服务端能力（VAPID 密钥、订阅管理、推送构造） | `backend/manager/component/webpush/`、`backend/manager/store/web_push_subscription.go` |
| 部署文档明确推荐 HTTPS 反向代理（Caddy/Nginx），machine 需要 HTTP/2，并注明 **PWA 需要 HTTPS** | `docs/deploy.md` §5（`docs/deploy_zh.md` 同步） |
| 实例存在 `BRANDING_LOGO` 设置项枚举，首次启动会播种一条空值设置，但**功能仍未实现**（无字段语义/无前端使用） | `proto/store/store/setting.proto` 的 `SettingName.BRANDING_LOGO`、`backend/manager/server/init.go` |

### 关键结论

1. 原状：Laelia 具备 PWA 的“一半”（Web Push 通知），缺可安装性与离线能力。**现状：缺口已补齐**（manifest + 图标 + App Shell 预缓存 + 离线降级，见第 3 节）。
2. 原状：SW 是“通知专用、按需注册”。结论要求改为**应用启动即注册**并合并预缓存逻辑。**现状：已实现**——`frontend/sw/sw.ts` 单一 SW 同时承载预缓存与推送，`frontend/src/lib/pwa.ts` 启动即注册。
3. 由于部署在用户自己的域名根路径，manifest、SW、图标都是**同源静态资源**，没有跨域问题，实现上比“前端/API 分域”部署简单。
4. 这是实时聊天/Agent 协作应用，核心数据都在服务端。**全离线读写不现实也不需要**，合理的 PWA 目标是：可安装 + 应用壳秒开 + 断网时给出友好降级而非白屏/报错。

---

## 2. 目标与非目标

### 目标

- **可安装（Installable）**：提供 `manifest.webmanifest` + 192/512 图标 + maskable 图标，满足 Chrome/Edge/Safari 的安装条件。
- **应用壳预缓存（App Shell offline）**：把 `index.html` 与带 hash 的静态资源（JS/CSS/字体等）预缓存，让二次打开/弱网时壳子秒开。
- **离线降级**：断网时给出“离线/无法连接”的可理解界面，而不是白屏或无限 loading；在线后自动恢复。
- **统一 SW**：把现有 Web Push 逻辑与预缓存逻辑合并到同一个 Service Worker，并改为应用启动即注册。
- **更新策略**：发布新版本后浏览器能拿到新 SW 和新资源，旧缓存被清理，避免“永远旧版”。
- **与自托管模型兼容**：零配置、同源、根路径部署；不引入必须的外部 CDN/第三方服务。
- **保持安全边界**：**不缓存任何 API 响应/用户数据**，不把敏感内容写进 Cache Storage；沿用现有 SameSite/CORS 安全模型。

### 非目标（本期不做，除非确认要做）

- 消息/频道/任务的**本地离线读写**与后台同步（需要 IndexedDB 数据层 + 冲突处理 + 安全审计，工作量显著增大，单独立项）。
- 完全脱离服务器的“纯离线使用”。
- 子路径（sub-path）部署支持（当前前端本身就是绝对路径，假设根路径部署；若确有子路径需求，需另立改造项）。
- 实例级品牌自定义（manifest 名称/图标跟随工作区设置）。目前 `BRANDING_LOGO` 只是枚举占位，本期用固定“Laelia”品牌资源，后续可平滑接入。

---

## 3. 已确认决策与实现状态

以下决策已与需求方确认并落地实现：

### 决策 1：离线能力
**A** —— 可安装 + App Shell 预缓存 + 离线降级提示。已实现：
- `main.tsx` 启动即注册 `/sw.js`；
- SW 对导航请求做离线 fallback 到预缓存的 `index.html`；
- 新增 `OfflineBanner` 顶部提示条（`navigator.onLine` + `online/offline` 事件）。

### 决策 2：PWA 工具链
**方案 X** —— `vite-plugin-pwa`（Workbox `injectManifest`）。已实现：
- `frontend/sw/sw.ts` 作为 SW 源（保留原有 Web Push 逻辑 + Workbox 预缓存），`vite.config.ts` 中 `srcDir: "sw"`、`filename: "sw.ts"`；
- `VitePWA` 配置（`strategies:"injectManifest"`、`manifest:false`、`injectRegister:null`、`registerType:'autoUpdate'`、`devOptions.enabled:false`，预缓存 `globPatterns` 含 `woff2`）；
- 新增直接依赖 `vite-plugin-pwa`（^1.3.0）与 `workbox-precaching` / `workbox-routing` / `workbox-strategies`（7.4.1）。

### 决策 3：manifest 品牌
**固定 “Laelia” + 通用图标**。已实现：
- `frontend/public/manifest.webmanifest`（`name/short_name/start_url/scope/icons` 等）；
- `frontend/public/icons/`（`icon-192.png`、`icon-512.png`、`icon-maskable-512.png`、`apple-touch-icon.png`，由 `temp/laelia_logo.png` 生成）；
- `frontend/index.html` 增加 manifest / theme-color / apple-touch-icon 标签。

### 决策 4：部署路径
**根路径部署**。manifest 的 `scope`/`start_url` 均为 `/`，SW scope 为根。

### 实现状态小结
- 前端构建通过（`pnpm build`），PWA 插件输出 `dist/sw.js`（预缓存条目为全部构建产物 + `public/` 静态资源，数量随构建变化）并保留推送逻辑。
- 后端 `server_frontend_embed.go` 已补充 `.webmanifest` MIME（`mime.AddExtensionType`）与 `pwaStaticHeadersMiddleware`（`sw.js`/`manifest`/HTML 为 `no-cache`，`/icons/*` 为 `public, max-age=31536000`，哈希资源 `immutable`），`go build -tags embed_frontend` 通过。
- 设计文档第 5.7 节（实例级 manifest 动态化）为二期预留，本期未实现。

### 补充：安装后桌面布局（standalone = desktop）
桌面安装的 PWA 窗口可能比 1024px 窄，导致误走手机端布局。处理方式（已实现）：

- **CSS**：在 `frontend/src/assets/css/tailwind.css` 用 Tailwind v4 `@custom-variant lg` 重定义 `lg:` 断点，使其在“宽度 ≥ 1024px”**或**“`display-mode: standalone` 且 `hover: hover` 且 `pointer: fine`（桌面设备）”时生效。这样安装后的桌面 PWA 即使窗口窄，也渲染桌面布局。
- **JS**：`frontend/src/hooks/use-is-desktop.ts` 的 `DESKTOP_QUERY` 同步加入同一条件，保证线程面板、消息右键菜单等 JS 驱动的桌面行为一致。
- 手机“添加到主屏幕”没有 `hover: hover`/`pointer: fine`，因此仍保持手机布局，不受影响。

## 4. 总体架构

```
浏览器
  │
  ├─ 首次访问 / 刷新
  │    GET /index.html  ──▶ 返回含 <link rel="manifest"> 的 HTML
  │    GET /manifest.webmanifest（静态）
  │    GET /assets/*（hash 资源）
  │
  ├─ 应用启动
  │    main.tsx ──▶ lib/pwa.ts 立即注册 /sw.js（仅生产构建；dev 不注册）
  │
  ├─ Service Worker（同一个 sw.js，源码 frontend/sw/sw.ts）
  │    ├─ install  : 预缓存 App Shell（index.html + 全部 hash 资源 + manifest + 图标）
  │    ├─ activate : cleanupOutdatedCaches() + skipWaiting + clients.claim
  │    ├─ fetch    : 导航请求走 NetworkFirst（10s 超时，离线回落到预缓存的
  │    │             index.html；新部署立即可见）；哈希静态资源走 Workbox
  │    │             precache（cache-first）；API/未预缓存请求无路由匹配，
  │    │             直接走网络，绝不写 Cache Storage
  │    ├─ push     : 现有 Web Push 逻辑（保持不变）
  │    └─ message  : SUPPRESS_ROUTE / PUSH_SUPPRESSED / NOTIFICATION_CLICK（保持不变）
  │
  └─ 离线时
       导航请求 10s 超时/失败 → 回落到预缓存的 index.html，壳子秒开
       API 失败 → 前端展示“离线”降级 UI（OfflineBanner，非白屏）
```

页面侧（`frontend/src/lib/pwa.ts` 的 `watchForUpdates`）：SW 用
`skipWaiting` + `clients.claim` 立即接管，已受控页面在 `controllerchange`
后重载一次进入新版本（避免旧构建的懒加载 chunk 被新预缓存清掉后 404）；
首次访问（从未受控）不重载。

---

## 5. 详细设计

### 5.1 Web App Manifest（`manifest.webmanifest`）

放在 `frontend/public/manifest.webmanifest`，由 Vite 原样复制到 `frontend/dist/`，再随前端一起被 Go 内嵌。

```json
{
  "name": "Laelia AI",
  "short_name": "Laelia",
  "description": "Self-hosted AI agent collaboration platform",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#0b0f14",
  "theme_color": "#0b0f14",
  "lang": "en",
  "icons": [
    { "src": "/icons/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any" },
    { "src": "/icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any" },
    { "src": "/icons/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
```

- `start_url` 与 `scope` 都是 `/`，因为自部署在根路径。
- 颜色取当前 Tailwind 主题的底色（`background`/`accent`），以实际设计为准。
- 若选择“实例可配置”，此处改为后端动态生成（见 5.7）。

在 `frontend/index.html` 的 `<head>` 中（已实现，与实际一致）：

```html
<link rel="manifest" href="/manifest.webmanifest" />
<meta name="theme-color" content="#0b0f14" />
<meta name="apple-mobile-web-app-capable" content="yes" />
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
<meta name="apple-mobile-web-app-title" content="Laelia" />
<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png" />
```

### 5.2 图标资源（已实现）

`frontend/public/icons/` 现有（由 `temp/laelia_logo.png` 生成）：

- `icon-192.png`（192×192，`any`）
- `icon-512.png`（512×512，`any`）
- `icon-maskable-512.png`（512×512，带安全边距的 maskable 版本）
- `apple-touch-icon.png`（iOS 主屏图标，`index.html` 引用）

### 5.3 Service Worker 合并与改造

原状：`frontend/public/sw.js` 是手写 JS、通知专用。**已按方案 X 落地**，SW 源
移至 `frontend/sw/sw.ts`（`srcDir: "sw"`），`public/sw.js` 已删除。

#### 方案 X：`vite-plugin-pwa`（Workbox `injectManifest` 模式，已实现）

- `vite.config.ts` 的实际配置：

```ts
import { VitePWA } from "vite-plugin-pwa";

VitePWA({
  strategies: "injectManifest",
  srcDir: "sw",            // SW 源在 frontend/sw/sw.ts
  filename: "sw.ts",
  registerType: "autoUpdate",
  injectRegister: null,    // 由 lib/pwa.ts 在应用启动时自行注册
  manifest: false,         // 我们用 public/manifest.webmanifest 手写
  injectManifest: {
    // 预缓存 index.html + 全部构建产物；/icons/*、/manifest.webmanifest 也在 public 里
    globPatterns: ["**/*.{js,css,html,svg,png,webmanifest,woff2}"],
    maximumFileSizeToCacheInBytes: 5 * 1024 * 1024,
  },
  devOptions: { enabled: false },
});
```

- SW 源里保留现有 push/message/notificationclick 逻辑，并加上 Workbox 预缓存。
  实际实现（`frontend/sw/sw.ts`）与最初的 `precacheAndRoute` 草案有一个重要差异——
  **路由注册顺序是刻意安排的**：`precache()` 只填充缓存不注册路由；导航请求全部
  走 `NavigationRoute` + `NetworkFirst`（10s 超时），预缓存的 `index.html` 只作为
  离线 catch handler；`addRoute()`（precache 路由）最后注册，只服务导航未命中的
  哈希资源。若用 `precacheAndRoute()`，`/` 会被映射到预缓存的旧 `index.html`
  （cache-first），把 PWA 固定在已安装 SW 的构建上、掩盖新部署：

```ts
/// <reference lib="webworker" />
import { addRoute, cleanupOutdatedCaches, createHandlerBoundToURL, precache } from "workbox-precaching";
import { NavigationRoute, registerRoute } from "workbox-routing";
import { NetworkFirst } from "workbox-strategies";

declare const self: ServiceWorkerGlobalScope;
precache(self.__WB_MANIFEST);

const navigationRoute = new NavigationRoute(
  new NetworkFirst({ cacheName: "laelia-navigations", networkTimeoutSeconds: 10 }),
);
navigationRoute.setCatchHandler(createHandlerBoundToURL("/index.html"));
registerRoute(navigationRoute);
addRoute();
cleanupOutdatedCaches();

// ... 现有 push / message / notificationclick 逻辑保持不变 ...
```

- Workbox 处理：预缓存、hash 变化时的更新、旧缓存清理；SW 更新配合
  `skipWaiting` + `clients.claim`（见 5.5）。

#### 方案 Y：零依赖手写预缓存（未采用）

- 新增 `frontend/scripts/generate-precache.mjs`：构建后读取 `frontend/dist`，生成 `precache-manifest.json`，形如：

```json
{
  "version": "<build-version-or-git-commit>",
  "assets": [
    "/index.html",
    "/assets/index-abc123.js",
    "/assets/index-def456.css"
  ]
}
```

- 在 `scripts/build_laelia.sh` 与 `Dockerfile.manager` 的 `pnpm build` 之后调用该脚本。
- `sw.js` 在 `install` 事件里 fetch `precache-manifest.json`，逐个 `cache.addAll` 到 `laelia-app-shell-v<version>`；`activate` 时删除其它 `laelia-app-shell-*` 缓存。
- 需要自己保证：SW 文件本身不设长缓存、SW 更新时能拿到新 manifest、离线时 manifest 已缓存。

> **推荐方案 X**：Workbox 对“hash 资产更新 + 旧缓存清理 + SW 生命周期”处理成熟，能显著减少手写边界 bug。如果团队对新增依赖敏感，再退回到方案 Y。

### 5.4 注册时机：应用启动即注册（已实现）

`frontend/src/lib/pwa.ts` 提供 `registerServiceWorker()`（`main.tsx` 启动时调
用），幂等、失败静默、仅生产构建：

```ts
export function registerServiceWorker(): void {
  if (!("serviceWorker" in navigator)) return;
  if (!import.meta.env.PROD) return;
  window.addEventListener("load", () => {
    navigator.serviceWorker.register(SW_URL)
      .then((reg) => watchForUpdates(reg))
      .catch(() => { /* 注册失败静默；不影响应用使用 */ });
  });
}
```

- 与 `web-push.ts` 的 `getRegistration()` 复用同一个 `/sw.js`（同一个注册，互不冲突）。
- 开发模式（`vite dev`）不注册，避免热更新与 SW 缓存互相干扰。
- 通知功能仍然走 `web-push.ts`：用户开启通知时 `getRegistration()` 拿到的是同一个已注册 SW。

### 5.5 更新策略

- 采用 `registerType: "autoUpdate"`；SW 源里 `install` 即 `skipWaiting()`，
  `activate` 即 `clients.claim()` + `cleanupOutdatedCaches()`。
- **页面侧闭环**（`lib/pwa.ts` 的 `watchForUpdates`）：新 SW 接管会在已受控页面
  触发 `controllerchange`，页面此时跑的是旧构建、其懒加载 chunk 正被清出预缓存，
  因此重载一次进入新版本；首次 claim（页面从未受控）不算更新，不重载。
- 每次发布：
  - 新 `index.html` 引用新的 hash 资源 → 浏览器发现新 SW 脚本（SW 文件本身内容变化）→ 安装新版本。
  - `activate` 清理旧 App Shell 缓存。
- 后端已把 `sw.js`、`manifest.webmanifest`、SPA HTML 的缓存头设为 `no-cache`
  （见 5.8，已实现），确保更新能及时生效。

### 5.6 离线降级体验

- **静态资源**：导航请求走 NetworkFirst（10s 超时），断网/超时回落到预缓存的
  `index.html`，壳子仍能打开；哈希资源由 Workbox precache 服务（cache-first）。
- **API（`/v1/*`、`/api/*`、文件上传等）**：SW 对未预缓存、非导航请求不注册任何
  路由，请求直接走网络，**绝不写 Cache Storage**（等效 network-only）。
- **前端离线 UI**：
  - 已实现 `OfflineBanner`（`frontend/src/components/offline-banner.tsx`，挂在
    `frontend/src/app/root-layout.tsx`）：基于 `navigator.onLine` +
    `online`/`offline` 事件，在顶部显示离线提示条。
  - 断网时如果页面是登录态且壳子已缓存，用户至少能看到界面；发消息等写操作由
    Connect 调用失败路径明确提示失败，不做本地假成功。
  - 设计中的极简离线路由（`/offline`）**未实现**——用常驻 `OfflineBanner`
    替代，无需整页切换。

### 5.7 （可选，二期）实例级 manifest 动态化

如果确认要做实例品牌自定义，则：

- 后端新增一个公开（无需登录）的 `GetWorkspacePublicInfo` 接口，返回实例名称、品牌图标、主题色等（从 `workspace_profile` / `BRANDING_LOGO` 读取）。
- `manifest.webmanifest` 改为后端动态路由（在 `echo_routes.go` 注册 `GET /manifest.webmanifest`），返回 `application/manifest+json`，内容按实例配置生成。
- 前端 `index.html` 仍引用 `/manifest.webmanifest`（不变），只是内容动态。
- 本期不做，仅预留接口位。

### 5.8 服务端静态资源与 MIME/缓存头调整（已实现）

`backend/manager/server/server_frontend_embed.go` 现状：

1. **`/manifest.webmanifest`**：启动时 `mime.AddExtensionType(".webmanifest",
   "application/manifest+json")` 补齐 MIME 表，静态处理器直接返回正确类型。
2. **`pwaStaticHeadersMiddleware`** 按路径设缓存头：
   - `/sw.js`、`/manifest.webmanifest` → `no-cache`（更新及时生效）；
   - `/icons/*` → `public, max-age=31536000`（图标内容替换而非改名，故不用
     `immutable`）；
   - SPA HTML 路径（无扩展名/`/`）→ `no-cache`，保证每次导航拿到新 hash 引用；
   - `/assets/*` 哈希资源维持原有的 `public, max-age=31536000, immutable`。

### 5.9 安全与合规

- SW 与 manifest 都是同源静态资源，不改变现有 CORS/CSRF 模型。
- **不缓存任何含用户数据的响应**（API 全部 network-only），避免敏感信息落入 Cache Storage。
- 保留现有安全响应头；`X-Frame-Options: DENY` 不影响 SW/manifest。
- PWA 的 Service Worker 要求 HTTPS（或 localhost）。自托管部署文档已推荐 HTTPS 反向代理，需在部署文档中明确“PWA 需 HTTPS 才能生效”。
- `index.html` 的 `<meta name="theme-color">` 与 manifest 的 `theme_color` 保持一致。

### 5.10 构建与发布集成

- `frontend/public/` 下新增的文件（`manifest.webmanifest`、`icons/*`）会被 Vite 自动复制到 `dist/`，因此：
  - `scripts/build_laelia.sh`：`pnpm build` 后自然带上这些文件，无需额外改动（方案 Y 则需追加生成 precache 的步骤）。
  - `Dockerfile.manager`：`COPY frontend/ ./` 后 `pnpm build`，同样自动带上（方案 Y 需在 Dockerfile 中也调用生成脚本）。
- 版本号：Workbox 以每个预缓存条目的内容 hash（revision）作缓存版本标识，
  构建产物变化即自动更新；无需再单独注入构建版本号。

### 5.11 测试与验证

- **单元/组件测试（现状）**：
  - `lib/pwa.ts` 与 `web-push.ts` 目前**没有**自动化测试（原计划的
    mock `navigator.serviceWorker` 注册逻辑测试未写）；推送/抑制逻辑的回归
    目前依赖手动验收。
- **手动验收清单**：
  - Lighthouse PWA 审计（Installable、Manifest、SW）通过。
  - 桌面 Chrome/Edge 出现“安装应用”提示，安装后以 standalone 窗口打开。
  - iOS Safari 添加到主屏幕后以独立模式打开，图标/名称正确。
  - 首次在线打开 → 刷新离线（DevTools offline）→ 应用壳仍能打开并显示离线提示。
  - 断网时发送消息有明确失败提示，不假成功。
  - 发布新版本（改一个 hash 资源）→ 旧缓存被清理，页面加载新版。
  - Web Push 通知在“未打开对应会话”时弹出系统通知；在“打开对应会话”时被抑制（现有行为不回归）。

---

## 6. 实施步骤（建议顺序）

**2026-09-06：以下步骤已全部完成。**

1. **确认决策 1–4**（离线深度、工具链、品牌、路径假设）。
2. 设计/获取品牌图标（192、512、maskable），加入 `frontend/public/icons/`。
3. 添加 `manifest.webmanifest` 与 `index.html` 的 manifest/theme-color/apple 标签。
4. 引入 `vite-plugin-pwa`（方案 X）：
   - 改造 SW：保留 push 逻辑 + 加入预缓存（`frontend/sw/sw.ts`）。
   - `main.tsx` 启动即注册（`frontend/src/lib/pwa.ts`）。
5. 后端静态服务调整：`manifest.webmanifest`、`sw.js`、SPA HTML 的 MIME 与缓存头（`server_frontend_embed.go`）。
6. 前端离线降级 UI（`OfflineBanner`；未做独立离线页面）。
7. 构建链路验证（`scripts/build_laelia.sh` 与 Docker 镜像内嵌后 `/manifest.webmanifest`、`/sw.js`、`/icons/*` 可访问）。
8. 更新 `docs/deploy.md` / `docs/deploy_zh.md`：已说明 PWA 需要 HTTPS（两份文档的外部访问章节）。
9. 自动化 + 手动验收（5.11；Lighthouse/安装/离线清单为手动验收项）。

---

## 7. 需要你确认的问题（汇总）

**2026-09-06：以下问题均已确认并实现，答案见第 3 节（决策 1–4）。**

1. 离线能力做 **A（推荐）** 还是 **B（含本地数据缓存/后台同步）**？→ 已选 A 并落地。
2. 工具链用 **vite-plugin-pwa/Workbox（推荐）** 还是 **零依赖手写预缓存**？→ 已选方案 X。
3. manifest 品牌用**固定 Laelia**（推荐，本期）还是**实例可配置**（二期）？→ 固定 Laelia；实例可配置仍为二期（5.7）。
4. 是否确认**根路径部署**假设成立（无子路径需求）？→ 成立，`scope`/`start_url` 均为 `/`。
5. 图标资产：是否有现成的 Laelia 品牌图标可提供，还是需要先占位？→ 已由 `temp/laelia_logo.png` 生成 `frontend/public/icons/` 四件套。
