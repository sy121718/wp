# 02-B · 媒体中心规范 (Media Center Specification)

> 本文档规范 `go_wp` 媒体模块（`media`）的功能设计、分类管理逻辑、调用场景及与构建系统的契约关系。

## 1. 架构定位与核心原则

- **不可变资产与稳定引用**：媒体文件一旦上传，生成唯一的稳定标识（`assetId`，内容哈希派生）与内容哈希。Page Document 和 CMS 内容中**仅保存 `assetId` 或稳定引用路径**，禁止直接硬编码动态生成的临时物理路径。
- **物理存储与逻辑分类解耦**：分类和标签是纯元数据层面的组织形式，调整媒体分类不会改变底层文件的物理存储路径，避免任何因分类变动导致的 URL 失效或构建断链。
- **构建期变体注入**：媒体的尺寸裁剪、WebP/AVIF 转码在上传或后台异步生成，Go Publish Compiler 在构建阶段根据组件需求自动解析最适合的尺寸与响应式图片标签（`<picture>` / `srcset`）。

## 2. 媒体库核心功能设计

### 资产管理与检索

- **树形分类与标签体系**：支持无限级媒体文件夹/分类（如：`品牌素材`、`产品图/2026春夏`、`博客配图`），支持按分类筛选、按标签聚合。
- **多维搜索与快速过滤**：支持按文件名、替代文本（Alt Text）、文件类型（图片/视频/SVG/文档）、上传时间、引用状态（已被使用 / 未被引用）快速过滤。
- **文件去重与版本替换**：上传时基于文件哈希检测重复文件，提供"替换原文件"功能（保留原 `assetId` 与引用关系，一键刷新全站该图片的所有静态编译产物）。

### 自动化处理与元数据

- **自动变体生成**：图片上传后自动生成缩略图（Thumbnail）、中等尺寸（Medium）、高清大图（Large）及现代格式（WebP/AVIF），并自动读取并写入图片的原始宽高（Width/Height），杜绝前端排版抖动（CLS）。
- **全局 SEO 元数据**：统一维护图片的全局默认 `alt`（替代文本）、`title` 和 `caption`（图注）。页面组件引用时默认继承全局值，同时允许在 Inspector 中进行局部覆盖。
- **引用追踪与保护**：实时记录每个媒体文件被哪些 Page、Product、Article 或 Global Component 引用。当用户尝试删除已被引用的媒体时，系统强制拦截并提供引用列表警告。

## 3. 调用场景与交互入口

媒体库作为系统级基础服务，主要在以下三处提供调用界面：

- **Visual Builder（可视化编辑器）**：图片/视频组件选择媒体时唤出媒体抽屉（Drawer/Modal）；支持直接在编辑器内拖拽上传新文件，上传成功后自动选中并绑定到当前组件；允许在面板中切换绑定的变体规格（原始尺寸 / 大图 / 缩略图）。
- **CMS 内容管理后台（文章 / 产品 / 分类）**：文章封面图（Featured Image）、产品图集（Gallery）、分类 Banner 等字段的点击选择；富文本编辑器中的"插入媒体"弹窗。
- **全局设置（Site Settings）**：站点 Favicon、Logo、社交分享默认图（OG Image）的上传与绑定。

## 4. 业务调用与数据流转逻辑

```text
Visual Builder / CMS Admin
└── 唤出 Media Modal
    └── 选择媒体或上传新文件
        └── 写入 Page Document / CMS 实体 (仅记录 assetId)
            └── 保存 Draft

Go Publish Compiler (发布构建期)
├── 读取 Page Document 中的 assetId
├── 向 Media 模块解析完整元数据 (实际 URL、宽高、srcset 变体集合、Alt)
└── 编译为标准 HTML (<img loading="lazy" width="..." height="..." srcset="...">)
```

## 5. 前端交互体验要点（解决传统 WP 痛点）

- **左侧分类树 + 右侧网格瀑布流**：左侧展示分类文件夹结构与快速标签，右侧支持平滑滚动的缩略图网格与实时搜索。
- **批量操作与移动**：支持按住 `Shift`/`Ctrl` 多选文件，一键批量修改分类、批量添加标签或批量导出。
- **即时详情预览侧栏**：点击任意素材，右侧滑出元数据面板，实时展示引用次数、文件大小、不同分辨率变体，并支持直接复制 CDN 链接。

## 6. 实现映射（代码位置）

```text
internal/builder/
├── media/                      # 媒体中心领域内核（内存存储；上传/转码/持久化由媒体业务模块对接）
│   ├── asset.go                #   Asset/Variant/Reference 模型、去重索引、版本替换、引用保护、多维检索
│   ├── resolve.go              #   core.MediaResolver 实现：变体选择、srcset、现代格式 source
│   └── render.go               #   图片 HTML 编译（<picture>/<img>，宽高必写、懒加载默认开）
├── core/render.go              # MediaResolver 契约与 MediaMeta（媒体级，覆盖图片/视频/SVG/文档）
└── components/
    └── image/                  # core.image 图片组件：仅记录 assetId，构建期解析注入
```

| 规范条目 | 实现 |
|---|---|
| 不可变资产与稳定引用 | `media/asset.go` `Upload`（内容哈希派生 assetId）、`deriveAssetID` |
| 文件去重与版本替换 | `Upload` 重复检测返回 duplicateOf；`Replace`（assetId/引用保留、Generation+1 触发全站刷新） |
| 引用追踪与保护 | `RecordRef` / `Refs` / `Delete`（被引用强制拦截并列出引用清单） |
| 多维搜索 | `Search`（文件名/类型/分类/标签/引用状态） |
| 构建期变体注入 | `media/resolve.go` `ResolveMedia` + `core.MediaResolver` 契约 |
| 响应式图片编译 | `media/render.go` `RenderImageHTML`（AVIF→WebP→fallback、宽高必写杜绝 CLS、`loading="lazy"` 默认） |
| 组件仅存 assetId | `components/image`（Validate 强制白名单 assetId，Render 经解析器取元数据） |
| 单元测试 | `public/test/builder/unit/media_test.go` |

命名约定：解析契约为**媒体级** `MediaResolver`（覆盖图片/视频/SVG/文档）；`core.image` 是消费它的**图片展示组件**（渲染 `<img>`），后续 `core.video` 等组件复用同一解析器。视频/文档资产解析返回稳定 URL 与 SEO 元数据（变体语义不适用）。