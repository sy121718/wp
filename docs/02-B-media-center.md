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
- **CMS 内容管理后台（文章 / 产品 / 分类）**：文章封面图（Featured Image）、产品图集（Gallery）、分类 Banner 等字段的点击选择；富文本编辑器（Trix 2.x）的图片插入走**附件上传**——粘贴/拖入/工具条插图直接 `POST /api/media/upload`（复用媒体模块，非超管需 `media:upload` 权限点），不经过媒体库弹窗。
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

> **口径更正（2026-09 按代码回填）**：本节原先引用的 `internal/builder/media/`（`asset.go`/`resolve.go`/`render.go`）与 `core.MediaResolver` **均不存在**。媒体能力的真实落点如下。

```text
internal/module/media/                # 媒体业务模块（持久化 + 变体生产 + 下载）
├── contract/media_service.go         #   MediaService 契约（ProbeImageVariants / GenerateVariants 已导出）
├── service/media_crud.go             #   上传 / 列表 / 详情 / 删除（sys_attachment、sys_file_category）
├── service/media_category.go         #   分类树增删改（含后代校验）
├── service/media_variant.go          #   变体登记/生成/探测（EnsureVariantRecords / GenerateVariants / ProbeImageVariants）
├── service/media_variant_task.go     #   asynq 异步变体任务
├── service/image_processor.go        #   decode / flattenToOpaque / encodeJPEGBytes / buildVariantImage
├── service/media_download.go         #   单图与批量 zip 下载（BuildDownloadPlan / BuildBatchDownloadPlan）
└── model/media_model.go、media_variant_model.go   # sys_attachment / sys_file_category / sys_media_variant 表访问

internal/builder/
├── core/render.go                    # RenderContext.AssetProbe：构建期探测「该 URL 存在哪些变体宽度」
├── core/image_loading.go             # 懒加载三态 + 骨架屏 CSS 的唯一实现
└── components/image/                 # core.image：image.go（模型/校验）+ jet.go（srcset 映射 _thumb/_medium）
```

| 规范条目 | 实现 |
|---|---|
| 稳定引用 | `sys_attachment.id`（自增主键）为引用标识；`md5` 列仅落库留痕，**不做去重** |
| 文件去重与版本替换 | **未实现**：`Upload` 无重复检测/duplicateOf，无 `Replace`/`Generation` |
| 引用追踪与保护 | 引用登记表 `media_reference` **⚠️ 已废弃：由 sys_attachment / sys_file_category / sys_media_variant 实现（见 §6 实现映射）**——该表已删除；引用保护改由 `sys_attachment.extra_info` 的 JSONB refs 承担 |
| 多维检索 | `Service.List`（`internal/module/media/service/media_crud.go:79`）：`file_type` + `category_id` + `search`（文件名）+ 分页 |
| 变体生成 | `GenerateVariants` + `media_variant_task.go`（asynq）；类型 `thumb`(320)/`medium`(1280)/`webp`，统一有损 JPEG q82 落盘为 `<stem>_<type>.jpg` |
| 构建期变体注入 | `builder.WithAssetProbe`（`internal/builder/builder.go:213`）+ `media.Service.ProbeImageVariants`（`page/service/page_assemble.go:104`） |
| 响应式图片编译 | `components/image/jet.go` 输出 `srcset`（`_thumb.jpg`/`_medium.jpg`）+ `sizes`，原图留 `src` 回退；宽高必写、`loading="lazy"` 默认开 |
| 懒加载/骨架屏 | `core.ResolveImageLoading` / `core.ImageSkeletonClass`（`core/image_loading.go`），单图→组件→主题→默认四级解析 |
| 单元测试 | `internal/builder/image_loading_test.go`、`internal/builder/image_skeleton_test.go`、`public/test/media/unit/media_probe_unit_test.go` |

命名约定（2026-09 更正）：当前**不存在** `MediaResolver` 契约；构建期响应式图片改由 `core.RenderContext.AssetProbe`（`internal/builder/core/render.go:42`，装配层经 `builder.WithAssetProbe` 注入）提供「URL → 可用变体宽度」探测，`core.image`（`internal/builder/components/image`）是消费它的图片展示组件。视频/文档资产当前只输出原文件 URL，无变体语义。

## 7. 数据库表结构（wp 库，PostgreSQL）

> **⚠️ 已废弃：由 sys_attachment / sys_file_category / sys_media_variant 实现（见 §6 实现映射）**
>
> `media_asset` / `media_asset_variant` / `media_reference` 三表**已删除**：代码零引用，且语义已由
> `sys_attachment` / `sys_file_category` / `sys_media_variant` 承担。`init_schema.sql` 中的建表语句
> （原 L305 / L337 / L356）已移除，落地迁移为 `public/migrations/069_drop_obsolete_design_tables.sql`。
> 下文 §2 的功能设计保留为历史设计记录，实现口径以 §6 为准。
>
> **口径更正（2026-09 按代码回填）**：该三表**没有任何 Go 代码读写**——属于早期设计遗留。实际持久化使用下表三张表。

| 表 | 职责 | 对应规范条目 |
|---|---|---|
| `sys_attachment` | 附件主表：自增 `id`（引用标识）、`file_name`/`file_path`/`file_size`/`file_type`/`mime_type`、`storage_type`/`storage_path`/`url`、`md5`（留痕）、`extra_info`、`status`、分类外键 | §1 稳定引用、§2 SEO/检索 |
| `sys_file_category` | 文件分类表（树形分类，媒体库筛选维度） | §2 多维检索 |
| `sys_media_variant` | 变体表：`attachment_id` + `variant_type`（thumb/medium/full）+ `file_path` + 宽高 + `status`（pending/processing/ready/failed），`UNIQUE(attachment_id, variant_type)`，级联删除 | §2 自动变体生成 |

依据：`public/migrations/init_schema.sql:204,236`、`public/migrations/048_media_variant.sql:17`、`internal/module/media/model/media_model.go:13-14`、`internal/module/media/model/media_variant_model.go:13`。

与 02-B 目标模型的关系：**⚠️ 已废弃：由 sys_attachment / sys_file_category / sys_media_variant 实现（见 §6 实现映射）**——`media_*` 三表对应的「内容哈希派生 assetId / 去重 / 版本替换 / 引用保护」已统一到 `sys_*` 体系（`067_media_center.sql` 补 `generation` / `extra_info jsonb` / 去重索引），三张空表已于 `069_drop_obsolete_design_tables.sql` 删除。

域内核（`internal/builder/media`）与表的字段一一对应：`Asset.ID/Hash/FileName/MimeType/Type/Width/Height/Size/Alt/Title/Caption/CategoryID/Tags/Generation`、`Variant.Kind/Format/URL/Width/Height`、`Reference.Kind/ID/Title`。