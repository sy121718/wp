# 0-A2 · 页面设置与发布路由元数据规范（归档）

> 用户规范《0-A2 页面设置与发布路由元数据规范》原文归档。实现映射见 §5。
>
> 背景：在实际建站中，URL 路径（Slug / 永久链接 Permalink）、SEO 元数据、访问权限、重定向以及自定义代码注入等都必须支持灵活配置与修改。本规范将「页面级设置（Page Settings）」与「发布管道」中这些页面元属性完整规范化。

## 1. 访问路径与永久链接管理 (URL & Permalink)

编辑器顶栏或底栏的「页面设置（⚙️）」面板中，用户可随时修改访问路径：

- **自定义访问路径 (Slug)**：
  - 支持自定义多级路径（如 `/about-us`、`/products/wireless-earbuds`、`/promo/2026/summer`）。
  - **首页设为根路径**：支持一键勾选「设为全站首页」，此时访问路径自动绑定为根路径 `/`。

- **修改 Slug 后的自动重定向保障 (Auto 301 Redirect)**：
  - 当一个**已经发布上线**的页面被修改了 Slug 时，系统自动提示：「是否为旧 URL 创建 301 永久重定向？」。
  - 勾选后，发布管道自动在路由层生成 `旧 URL -> 新 URL` 的 301 跳转记录，防止外部引流链接失效或 SEO 权重丢失。

## 2. 页面全局元数据配置项 (Full Page Meta)

「页面设置」面板包含以下完整维度的可编辑字段，发布时由 Go 编译器直接注入最终 HTML 的 `<head>` 与 `<body>` 标签中：

### 2.1 SEO 与社交分享卡片 (SEO & Open Graph)

- **SEO 页面标题 (SEO Title)**：支持自定义或留空（留空时默认取页面主名称）。
- **页面描述 (Meta Description)**：自定义搜索引擎收录描述。
- **关键词 (Meta Keywords)**：可选配置页面专有关键词。
- **搜索引擎爬取索引控制 (Robots Meta)**：
  - `index / noindex`：允许/禁止搜索引擎抓取本页（如隐私条款页、临时测试页勾选 `noindex`）。
  - `follow / nofollow`：允许/禁止追踪本页内的链接。
- **规范链接 (Canonical URL)**：支持手动覆盖规范网址，避免多渠道内容重复带来的 SEO 惩罚。
- **社交分享卡片 (OG / Twitter Card)**：自定义分享封面图（OG Image）、分享标题与分享副标题。

### 2.2 访问权限与发布可见性 (Access & Visibility)

- **公开可见 (Public，默认)**：全网任何访客均可访问。
- **密码保护 (Password Protected)**：设置独立访问密码，访问时展示极简输密页，验证通过后方可浏览。
- **登录用户可见 (Private / Members Only)**：仅针对已登录会员或特定角色开放。
- **定时上下线 (Scheduled Publishing)**：
  - 设定未来指定时间自动激活上线（常用于营销活动倒计时准点发布）。
  - 设定活动结束下线时间，下线后自动跳回指定页面或 404。

### 2.3 全局代码注入 (Custom Head & Body Scripts)

允许为当前独立页面单独注入第三方统计与营销追踪代码，无需改动全站全局模板：

- **Head 注入区 (`<head>`)**：Google Analytics、Facebook Pixel、热力图追踪代码（Clarity/Hotjar）等。
- **Body 底部注入区 (`</body>` 前)**：在线客服浮窗插件（Intercom/Crisp）、第三方转化追踪脚本等。

## 3. 发布管道与路由更新执行流

用户修改了上述任一配置并点击【发布/更新】时，发布管道执行以下同步逻辑：

```text
[修改 Slug / SEO / 权限 / 注入代码]
       │
       ▼ (点击更新/发布)
[Go Publish Compiler 编译期]
 ├── 1. 将自定义 SEO Meta、OG 标签、Head/Body 脚本静态注入 HTML <head>/<body>
 ├── 2. 若配置了密码保护/权限，在产物包元数据中标记 AccessGuard 属性
 └── 3. 将新生成的 HTML/CSS 产物落盘
       │
       ▼ (激活上线时)
[Web 服务路由表 (Router Table)]
 ├── 1. 更新活跃路由映射：将当前活跃 HTML 产物指针绑定至【新 URL Slug】
 ├── 2. 若检测到旧 URL 变更，自动将【旧 URL】注册进 301 永久重定向缓存
 └── 3. 清理该页面旧路径在 CDN 边缘的静态缓存
```

## 4. 总结：所有杂项均在「页面设置」中闭环

1. **URL 想改就改**：支持任意多级 Slug 与一键设为首页，系统自带 301 重定向兜底防死链。
2. **SEO 与营销代码独立可控**：每个页面都可以单独配独立的 Meta 标题、描述、分享卡片和 Pixel 像素代码。
3. **纯静态直出**：所有配置在发布时一次性静态编译进 HTML，保持极致的加载性能与干净的源码结构。

## 5. 实现映射

| 规范条目 | 实现 / 计划 |
|---|---|
| 自定义访问路径（多级 Slug）、首页根路径 `/` | `docs/03-pipeline.md` §5.1（URL 规范化：只存 path、拒绝穿越、`page_routes` 全局唯一）、§6.5（URL 修改五步流程）；落地为 Phase 0-A1 `page` 模块的 `draft_path` 与路由占用（`05-implementation-plan.md` 阶段 3.1、`docs/01-overview.md` 模块规划 `page`/`publication`） |
| 修改 Slug 的 Auto 301 重定向 | `docs/03-pipeline.md` §4.4（Redirect Artifact）、§6.5 步骤 5（旧 active route 按显式策略取消或指向 Redirect Artifact）；落地为 Phase 0-A1 `publication` 模块，`page_routes` 投影记录 redirect 路径 |
| SEO Title / Description | 已实现：`internal/builder/settings.go` `SEO{Title,Description}`，编译期注入 `<title>` 与 `<meta name="description">`（`internal/builder/builder.go` `RenderDocument`） |
| Keywords / Robots / Canonical / OG / Twitter Card | 扩展点：`internal/builder/settings.go` `SEO` 增加 Keywords/Robots/Canonical/OGImage/OGTitle/OGDescription 等字段，编译期注入 `<head>`；`docs/04-runtime-and-delivery.md` §2.1 要求 Artifact 必须包含完整 title、description、canonical、Open Graph（当前 canonical/OG 缺失为验收缺口） |
| 访问权限（Public / 密码 / 登录可见） | 扩展点：Artifact Manifest 增加 AccessGuard 标记（`docs/03-pipeline.md` §4.2），静态访问面按守卫直出极简验证页；密码/会员语义属 Phase 0-A2 之后（访客用户域另建），0-A1 先支持公开 + 密码字段 |
| 定时上下线 (Scheduled Publishing) | 扩展点：基于发布状态机（`docs/03-pipeline.md` §6）与 `internal/task/` 定时任务，`page_routes`/`pages` 投影增加 scheduled 字段（`docs/03-pipeline.md` §9）；下线跳转目标由页面设置保存 |
| Head / Body 代码注入 | 扩展点：`internal/builder/settings.go` 新增 HeadScripts/BodyScripts（白名单 URL 或受限 JS 文本），`internal/builder/builder.go` `RenderDocument` 在 `</head>` 前与 `</body>` 前注入；安全基线见 `docs/04-runtime-and-delivery.md` §2.2 |
| 路由表更新与 CDN 缓存清理 | PublicationStore 原子激活（`docs/03-pipeline.md` §5）；CDN 端依赖内容哈希资源 immutable 缓存（`docs/04-runtime-and-delivery.md` §2.3），旧路径清理由发布任务在激活后执行 |

> 注意：`PageSettings`（`internal/builder/settings.go`）当前仅覆盖版心、基底样式与基础 SEO，未包含本文档的 URL、权限、注入代码等字段——该数据属于页面实例的全局设置，将由 Phase 0-A1 `page`/`project` 模块持有（`projects`/`pages` 表投影），编译器在构建时作为 Page Document 的一部分消费。