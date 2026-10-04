# 05 · 全阶段实施与验收计划

> 内部过程文档（非对外使用）

> 本文是 go_wp 从当前状态到完整交付的执行路线。每个阶段有明确的验收门禁，未通过不得进入下一阶段。
>
> **状态更新于 2026-09（按代码二次回填）：阶段 0-4、6、7 已完成；阶段 5 部分完成（`blueprint`/`media` 已落地，`component` 未落地，语义由 `block.reuse_mode` 承担，该结论经本轮复核仍准确）。**
>
> **本轮（按当前代码）更正的两处过期读数**：① 上一版写「presentation 侧未落地（其持久化与生产 DDL 未对齐）」—— 实测两侧均已接入同一 fan-out，presentation 实例已按生产 DDL 持久化；② 上一版写阶段 7「分页列表与搜索 Shell 的 capability 未注册（当前仅 `loginPanel`/`cartSummary`）」—— 实测已注册 25 条 capability，分页与搜索两条均在其中。留痕见[当前状态](#当前状态)表与阶段 4/7 各节。

## 冻结决策

| 决策项 | 结论 |
|--------|------|
| 管理后台 | Go + Jet SSR 渲染页面/片段，HTMX 负责交互；不再维护 Vue SPA |
| 认证 | Session + Cookie（gin-contrib/sessions + Cookie 存储），不用 JWT |
| 鉴权 | Casbin（Enforce(user_id, path, method)） |
| CSRF | 所有 POST 写操作强制 CSRF Token 校验（已实现并全局挂载） |
| 数据库 | PostgreSQL（主库，唯一领域 SQL 基线）；MySQL 历史兼容；SQLite/SQL Server 驱动已移除 |
| 会话存储 | Redis（pkg/cache，Critical：认证会话/封禁/心跳硬依赖） |
| 公共前台 | Jet 只在 Preview/Publish 构建阶段执行，访客读取静态 Artifact |
| 富文本 | Trix 2.x（本地 vendor `internal/templates/static/vendor/trix/`，零 CDN；服务端白名单清洗在 `core/richtext.go`） |
| 设计系统 | Stripe 风格，亮暗双主题，详见 [DESIGN.md](../DESIGN.md) |

## 数据流总览

```text
管理后台：
  Admin Jet 页面 → HTMX 请求 → Gin inbound/http → 模块 Service → PostgreSQL
                                                              ↓
                                                     Casbin 鉴权
                                                              ↓
                                                     Jet 渲染 HTML 片段返回

公开站点（构建期）：
  Page Document + CMS Content + BuildContext
    → Publish Compiler（Jet 构建期渲染）
    → Immutable Artifact
    → ArtifactStore.put()
    → PublicationStore.activate()
    → Static Server / CDN

公开站点（访客）：
  HTTP Request → Static Server → index.html + hash assets
    → Browser
    → 可选 HTMX → Runtime Fragment Endpoint
```

## 执行规则

1. 每个阶段固定执行：`go test ./...`、`go vet ./...`、`go build ./...`
2. 涉及数据库的阶段额外验证：空库迁移成功 → 第二次迁移幂等 → 集成测试通过
3. 每个阶段完成后提交 Git（中文注明改动文件路径和修改内容）
4. 任一门禁失败立即停止，修复后重新执行

## 当前状态

| 阶段 | 状态 | 说明 |
|------|------|------|
| 阶段 0 | 已完成 | 基线搭建 |
| 阶段 1 | 已完成 | 后台壳 + Session + 安全 |
| 阶段 2 | 已完成 | 模块 HTMX 迁移（admin 六领域合并大模块，SPA/JWT 已清理） |
| 阶段 3 | 已完成 | 0-A1 Page 静态发布主链（project/page/artifact/publication 模块落地，两段式回执/占用前置/activating 随机化） |
| 阶段 4 | 已完成 | 0-A2 CMS 内容 + 自动发布：`content`/`contenttemplate`/`presentation` 三模块已落地（`042_content.sql`；`content_templates`/`presentation_instances`/`document_snapshots` 建表在 `public/migrations/init_builder_schema.sql` 的 :56/:129/:150，另有 `071_dependency_fanout.sql`）；「CMS 变更 → 自动发布」**两侧均已落地**：content 写入即扇出（`internal/module/content/service/content_service.go:97/124/222` 的 `notifyContentChanged`，键 `direct_content:{type}:{id}` + `content_collection:collection:content:{type}`），装配层注册两个来源与各自重建器（`internal/routers/assembly_publish.go:452/458/482/488`），presentation 侧依赖随构建落库（`internal/module/presentation/service/presentation_persist.go:136` 的 `persistDependenciesTx` 写 `presentation_dependencies`）+ 精确反查 + 自动重建（`internal/module/presentation/service/presentation_stale.go:33` 的 `MarkStaleByDependency`、:176 的 `RebuildStale`）；**文章编辑页 Trix 集成已落地**：`/admin/articles` 列表 + `/admin/articles/edit` 编辑页（`internal/module/content/inbound/http/article_router.go:50/60`，模板 `internal/templates/admin/content/articles.html`、`article_edit.html`，正文经 `core.SanitizeRichHTML` 清洗，调用点 `internal/module/content/inbound/http/article_page_data.go:38`）。**原记录「presentation 侧未落地（其持久化与生产 DDL 未对齐）」「未做文章编辑页 Trix 集成（后台无内容管理页）」已过时。** 依赖键覆盖面的遗留（menu/media/site_setting 等）仍见 `10-todo.md` PIPE-3 |
| 阶段 5 | 部分完成 | 0-B Blueprint + Component + Media：`blueprint`（建表 `public/migrations/init_builder_schema.sql:30`，DDL 对齐见 `073_blueprint_ddl_align.sql`）、`media`（迁移 `048_media_variant.sql` + asynq 变体任务 `internal/module/media/service/media_variant_task.go`）已落地；`component` **未落地**，其版本/更新策略语义由 `block.reuse_mode` 承担（迁移 `049_block_reuse_mode.sql`；`internal/module/block/model/block_model.go:64` 的 `ReuseMode` 字段、:49/:51 的 `ReuseGlobal`/`ReuseTemplate` 常量）。**本行 component 结论经本轮复核仍然准确、未改**；原记录引用的「迁移 `045_blueprint.sql`」与「`block_model.go:37-40`」已过时（前者不存在；后者是 `kind` 常量区，不是 reuse_mode） |
| 阶段 6 | 已完成 | 0-C Navigation（`internal/module/navigation`，迁移 046/054，后台 `/admin/navigations`） |
| 阶段 7 | 已完成 | 0-D Runtime Fragment：内核与能力白名单均已落地（`internal/module/runtimefragment` 的 registry/capability/endpoint/router，`GET`/`POST /_fragments/:type`，见 `router.go:44/49`）。`internal/module/runtimefragment/` 下经 `Register(Spec{...})` 共注册 **25 条 capability**：`loginPanel`（`capability.go`）、`cartSummary`/`cartView`/`cartAdd`/`cartSetQty`/`cartClear`/`checkout`（`cart.go` 六条）、`productList`（`product_list.go`）、`productVariantAvailability`（`variant_availability.go`）、`productLivePrice`（`live_price.go`）、`searchResults`（`search_results.go`）、`ordersList`/`orderDetail`（`orders.go`）、`returnRequest`（`returns.go`）、`bundleConfigurator`/`bundleConfiguratorCheck`（`bundle_configurator.go`）、`loginForm`/`registerForm`/`forgotForm`/`resetForm`/`accountPanel`（`user_forms.go` 五条）、`accountProfileForm`/`accountPreferenceForm`/`accountPasswordForm`/`accountSessionsPanel`（`user_account_forms.go` 四条）。**原记录「分页列表与搜索 Shell 的 capability 未注册（当前仅 `loginPanel`/`cartSummary`）」已过时**：分页走 `productList`（自第 2 页起把 offset 下推 SQL，`internal/builder/components/productlist/jet.go:246/521`），搜索走 `searchResults`（构建期组件 `core.searchResults` 只输出搜索框 + 挂载点，命中由片段现拉，`internal/builder/components/searchresults/searchresults.go:3/18`） |

---

## 阶段 0：基线、依赖与测试底座

**目标**：搭好 Jet 渲染骨架，迁移系统就绪。

**任务**：

- [x] `go.mod` 确认 Jet v6 依赖已引入
- [x] 加入 HTML sanitize 依赖（实现为自研 sanitize：基于 x/net/html 白名单，非 bluemonday），用于富文本内容白名单清洗（现集中实现于 `core/richtext.go`，编辑器为 Trix 2.x）
- [x] 加入 CSRF 中间件依赖
- [x] 建立 `internal/templates/admin/` 目录结构和 Jet 渲染 facade
  - 实现 `gin.HTMLRender` 接口封装 Jet `*jet.Set`
  - 开发模式 `DevelopmentMode(true)`
- [x] 建立 `internal/templates/publish/` 目录（构建期模板实际位于 `internal/templates/components/`，go:embed，与 Admin 模板隔离）
  - **原记录已过时（本轮实测）**：`internal/templates/publish/` 目录不存在；`internal/templates/components/` 现仅剩 `_placeholder.jet`（embed 入口仍是 `internal/templates/components_embed.go:20` 的 `//go:embed components/*.jet`）。构建期组件模板现已与组件源码同目录 —— `internal/builder/components/<组件>/*.jet`（各组件自己的 `//go:embed <name>.jet`）
- [x] 修复 `public/migrations/`：当前只有 runner 无迁移文件
  - 增加独立 migration 命令
  - 确保 runner 接入启动流程
- [x] 加入 `gin-contrib/sessions` 依赖

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| `go build ./...` | 编译通过 |
| `go test ./...` | 现有测试全绿 |
| `go vet ./...` | 无警告 |
| Jet 渲染 | 最小模板渲染 + 自动转义测试通过 |
| PostgreSQL 迁移 | 空库迁移成功，第二次幂等 |

---

## 阶段 1：Jet + HTMX 后台壳、Session 与安全边界

**目标**：无 JavaScript 也能登录、退出、导航后台。

**任务**：

- [x] 在 `routes.go` 注册 `/admin/*` 页面路由和 `/admin/fragments/*` 片段路由
  - **原记录已过时（本轮实测）**：不存在 `/admin/fragments/*` 路由组（`internal/routers/routes.go` 内无此前缀注册）。后台 HTMX 片段走各模块自己的页面路由，例如 `/admin/product-categories/children`、`/admin/product-tags/hits`
- [x] 实现 Session 认证中间件（`SessionAuthMiddleware`）
  - gin-contrib/sessions + Cookie 存储
  - Cookie 属性：`HttpOnly`、`Secure`（生产）、`SameSite=Lax`
  - Session ID 随机不透明，Cookie 不保存用户资料
- [x] 实现 CSRF 中间件
  - 所有 POST 请求强制 CSRF Token
  - Token 通过 Jet 模板注入到表单隐藏域
- [x] 登录页 Jet 模板（原记录 `admin/login.jet` —— **已过时**：现为 `internal/templates/admin/login.html`）
- [x] 后台布局 Jet 模板（原记录 `admin/layout.jet` —— **已过时**：现为 `internal/templates/admin/layout.html`）
  - 侧栏导航、顶栏、主题切换按钮
  - HTMX CDN 引入
  - 主题初始化脚本（防 FOUC）
- [x] HTMX 请求区分：`HX-Request: true` 返回片段，否则返回完整页面
- [x] 安全头中间件复用现有 `security_headers.go`

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 登录流程 | 无 JS 也能登录、退出、导航 |
| Cookie 属性 | HttpOnly + SameSite 验证通过 |
| CSRF | POST 缺 Token 被拒、带 Token 通过 |
| 未登录 | 自动重定向到登录页 |
| HTMX 片段 | `HX-Request` 返回 HTML 片段，非 HTMX 返回完整页面 |
| Casbin 拒绝 | 无权限返回 403 |
| XSS 转义 | Jet 自动转义测试通过 |
| `go test ./...` | 全绿 |

---

## 阶段 2：现有后台模块逐个迁移并移除 SPA

**目标**：管理控制面所有功能无需 Vue/Node 即可操作。

**迁移顺序**：`admin → permission → menu → role → dept → datarule`（注：六领域已合并为 `admin` 大模块，不再独立目录，迁移顺序为历史记录）

**每个模块的任务**：

- [x] 列表页：Jet 表格 + HTMX 分页（`hx-get` + Query 参数）
- [x] 搜索筛选：`hx-trigger="keyup delay:500ms"`
- [x] 新增/编辑：弹窗式 fragment（`hx-get` 加载表单 → `hx-post` 提交）
- [x] 删除：`hx-confirm` 确认 → `hx-post` 提交
- [x] 权限相关：Casbin 鉴权保持不变，复用现有 service/contract/model
- [x] 响应模式：HTMX 请求返回 HTML 片段，非 HTMX 返回完整页面

**迁移完成后清理**：

- [x] 删除 `internal/embed/dist/` 及 `setupEmbeddedFrontend`（embed SPA 已移除）
- [x] 清理旧 JWT 相关代码（`pkg/auth/jwt.go`、`internal/middleware/builtin/auth.go` 中的 JWT 逻辑）
  - 本轮复核：`pkg/auth/jwt.go` 已不存在；`internal/middleware/builtin/auth.go` 作为 Session 认证中间件保留，文件内已无 JWT 逻辑（仅剩注释提及「key 与旧 JWT 中间件保持一致」「无需 JWT 自动续期」）
- [x] 移除 `internal/embed/embed.go` 的 SPA 入口

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 各模块 CRUD | 列表/新增/编辑/删除/分页/筛选功能正常 |
| 权限 | 无越权跨模块访问 |
| 禁用 JS 冒烟 | 关闭 JavaScript 后基本功能可用（登录、导航、表单提交） |
| HTMX 局部更新 | 列表刷新、弹窗编辑、删除确认正常工作 |
| 无 SPA 残留 | 不存在 Vue 构建产物、旧业务资源或浏览器 token 存储 |
| `go test ./...` | 全绿 |

---

## 阶段 3：Phase 0-A1 — 手工 Page 静态发布主链

**目标**：Page Document → Publish Compiler → Artifact → Publication 全链路打通。

### 3.1 Project 与 Page

- [x] 创建 `project` 模块：Project / SiteSettings / 构建所需 Project 快照
- [x] 创建 `page` 模块：Page Draft / Page Document / 版本与乐观锁
- [x] 实现 `PageKind + ContentTarget` 封闭枚举、ThemeNode AST、Binding 协议
- [x] PostgreSQL 迁移：`projects`、`pages`、`page_documents` 表（主库 PostgreSQL，MySQL 历史兼容）
  - **原记录已过时（本轮实测）**：不存在 `page_documents` 表 —— Page Document 存于 `pages.draft_document`（jsonb，主键内容见 `public/migrations/init_builder_schema.sql:81`），版本表是 `page_revisions`（:117）；同批还有 `page_routes`（:161）、`page_artifacts`（:206）、`page_artifact_objects`（:242）、`page_dependencies`（:250），`projects` 在 :17

**验收**：合法 Draft 可往返序列化；非法 kind/target、重复 Node ID、旧版本写入被拒绝。

### 3.2 Publish Compiler

- [x] 创建构建内核：BuildContext Resolver → Migrate → Normalize → Validate → Lowering → Fragment → Style → Asset → Diagnostics → Jet Build-time Render（落地于 `internal/builder`，发布内核在 `internal/pipeline`，非独立 module/build）
- [x] BuildContext 固定后禁止读数据库、网络和当前时间
- [x] map/集合/资源路径必须稳定排序与规范化

**验收**：同一输入重复构建字节完全相同；最终 HTML 不含 Jet 表达式、Binding 或编辑属性。

### 3.3 Artifact 与 Publication

- [x] 创建 `artifact` 模块：不可变写入、内容对象闭包、ArtifactStore 契约与本地实现
- [x] 创建 `publication` 模块：URL 占用、stage、activate、rollback、receipt 恢复（两段式回执 pending→committed/rolled_back、占用前置检查）
- [x] 静态访问面只读取 PublicationStore 文件状态

**验收**：Page 可发布、二次发布、单页回滚；任一步失败旧页面仍可访问。

---

## 阶段 4：Phase 0-A2 — CMS 内容 + 自动发布

**目标**：Article/Product/Category 内容变更自动触发发布。

**任务**：

- [x] `content` 模块：固定 CMS 内容（Article/Product/Category/Tag）与单调 revision（`internal/module/content`，迁移 `042_content.sql`）
- [x] `contenttemplate` 模块：ContentTemplate 草稿、不可变版本、Binding 约束（`internal/module/contenttemplate`；**原记录「迁移 `043_content_template.sql`」已过时** —— 建表在 `public/migrations/init_builder_schema.sql:56` 的 `content_templates`，后续演进见 `164_content_template_is_default.sql`、`223_content_template_default_per_project.sql`）
- [x] `presentation` 模块：PresentationInstance / DocumentSnapshot / 内容驱动发布入口（`internal/module/presentation`；**原记录「迁移 `044_presentation.sql`」已过时** —— 建表在 `public/migrations/init_builder_schema.sql` 的 `presentation_instances`:129、`document_snapshots`:150、`presentation_dependencies`:300，依赖 fan-out 迁移 `071_dependency_fanout.sql`）
- [x] CMS 实体变更 → 自动派生 DocumentSnapshot → 经同一 Publish Compiler → ArtifactStore → PublicationStore
  - **原记录「未做：当前仅手动 `POST /api/presentation/rebuild`」已过时**：content 写入即触发扇出 —— `internal/module/content/service/content_service.go:50` 的 `notifyContentChanged`（由 Create/Update/Delete 路径在 :97/:124/:222 调用）推导 `direct_content:{type}:{id}` + `content_collection:collection:content:{type}` 两条键；装配层两个来源都注册（`internal/routers/assembly_publish.go:452` page、:458 presentation）并各挂重建器（:482/:488，另有启动自检 `RegisteredSourceTypes()`）；presentation 侧依赖随构建落库（`internal/module/presentation/service/presentation_persist.go:136` 的 `persistDependenciesTx` 写 `presentation_dependencies`）、按 `(kind,key)` 精确反查（`internal/module/presentation/service/presentation_stale.go:33`）、自动重建并重新发布（同文件 :176 的 `RebuildStale`）。手动 `POST /api/presentation/rebuild` 仍在，作为显式入口的兜底。另见 `03-pipeline.md` §8、`10-todo.md` PIPE-3/INF-2
- [x] 富文本编辑器（Trix 2.x，本地 vendor）集成到文章编辑页（服务端白名单清洗）
  - **原记录「部分：后台尚无文章/内容编辑页」已过时**：编辑页已落地 —— 列表 `/admin/articles`（`internal/module/content/inbound/http/article_router.go:50`，模板 `internal/templates/admin/content/articles.html`）与编辑页 `/admin/articles/edit`（同文件 :60，模板 `internal/templates/admin/content/article_edit.html`：Trix 富文本 + 摘要 + 封面 + SEO 字段 + 评测侧栏 + 发布区块；Trix 本地 vendor 引用见该模板 :19 的 `trix.css` 与 :350 的 `trix.umd.js`）；正文落库前过白名单唯一来源 `core.SanitizeRichHTML`（定义于 `internal/builder/core/richtext.go:94`，调用点 `internal/module/content/inbound/http/article_page_data.go:38`）。见 `10-todo.md` INF-1

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 内容 CRUD | ✅ Article/Product/Category 创建/编辑/删除正常（`content` 模块 API） |
| 自动发布 | ✅ 通过（**原「❌ 未通过：内容变更不自动触发」已过时**）：内容写入 → `notifyContentChanged`（`internal/module/content/service/content_service.go:50`）→ `pipeline.Fanout`（装配 `internal/routers/assembly_publish.go:452/458/482/488`）→ `presentation.MarkStaleByDependency`（`internal/module/presentation/service/presentation_stale.go:33`）→ `RebuildStale`（:176）自动重建并重新发布 |
| ContentTemplate 版本 | ✅ 通过（**原「❌ 未通过：版本变化未自动触发」已过时**）：模板产生新版本 / 切换生效后按 `content_template:{id}` 失效并触发关联实例重建，接线见 `internal/routers/assembly_publish.go:513` 起，依赖键常量见 `internal/pipeline/dependency.go:30` 与 :79-85 的 `ContentTemplateKey` |
| 富文本编辑器（Trix 2.x） | ✅ 通过（**原「⚠️ 部分：文章编辑页尚未存在」已过时**）：文章编辑页已落地，Trix 加载正常、提交经 `core.SanitizeRichHTML` 白名单清洗 |
| `go test ./...` | 全绿 |

---

## 阶段 5：Phase 0-B — Blueprint + Component + Media

**目标**：页面初始化工具、全局组件版本管理和媒体资源闭环。

**任务**：

- [x] `blueprint` 模块：Blueprint 草稿、不可变版本、Page 初始化（用完即弃）（`internal/module/blueprint`；**原记录「迁移 `045_blueprint.sql`」已过时** —— 建表在 `public/migrations/init_builder_schema.sql:30` 的 `blueprints`，另有 DDL 对齐迁移 `073_blueprint_ddl_align.sql`）
- [ ] `component` 模块：Global Component、版本、更新策略（immutable/auto-update/pinned）、Registry manifest（**未落地且本轮复核仍准确**：无 `internal/module/component`；复用与版本语义由 `block` 的 `reuse_mode` 承担，迁移 `049_block_reuse_mode.sql`，`internal/module/block/model/block_model.go:64` 的 `ReuseMode` 字段与 :49/:51 的 `ReuseGlobal`/`ReuseTemplate` 常量 —— **原引用 `block_model.go:37-40` 已过时**，那是 `kind` 常量区）
- [x] `media` 模块：媒体元数据、变体、内容 hash、稳定 assetId（`internal/module/media`，迁移 `048_media_variant.sql`，变体生成走 asynq）

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| Blueprint | ✅ 初始化 Page Document 后不再参与构建；修改不传播 |
| Component 版本 | ❌ 未通过：无 `component` 模块，无 immutable/auto-update/pinned 三策略；`block.reuse_mode` 只提供 global（引用+stale 传播）/ template（复制后独立）两种语义 |
| Media | ✅ 上传→变体→引用链路闭环，assetId 稳定（`sys_attachment`/`sys_file_category`/`sys_media_variant`） |
| `go test ./...` | 全绿 |

---

## 阶段 6：Phase 0-C — Navigation

**目标**：公开站点导航独立于后台权限菜单。

**任务**：

- [x] `navigation` 模块：公开站点 Header/Footer 导航、菜单位置、revision（`internal/module/navigation`，迁移 `046_navigation.sql`/`054_navigation_sources.sql`）
- [x] 导航构建期编译进静态 Artifact（`internal/builder/navigation_test.go` 覆盖）

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 导航管理 | 后台可增删改公开站点导航项 |
| 与 menu 隔离 | `navigation` 不复用 `menu` 表和逻辑 |
| 构建输出 | 导航出现在发布的静态 HTML 中 |
| `go test ./...` | 全绿 |

---

## 阶段 7：Phase 0-D — Runtime Fragment

**目标**：白名单动态片段为静态页面提供实时交互能力。

**任务**：

- [x] `runtimefragment` 模块：capability 白名单、受控 HTML Fragment handler（`internal/module/runtimefragment/registry.go`）
- [x] HTMX 请求 → Go Handler → 返回受控 HTML 片段（`GET /_fragments/:type`，`router.go`）
- [x] 分页列表：构建时只输出第一页静态 HTML，后续页码 HTMX 按需加载（**原记录「未注册 capability」已过时**：`productList` 能力已注册（`internal/module/runtimefragment/product_list.go:63`，参数 `page`/`pageSize` 见 :103-104），组件在构建期输出分页控件、静态产物只出默认那一屏，自第 2 页起把 offset 下推 SQL（`internal/builder/components/productlist/jet.go:246` 与 :521 的 `resolveProductsPage`），边界常量 `MaxPage`/`MaxPageSize` 在 `internal/builder/components/productlist/productlist.go:175`）
- [x] 搜索 Shell：规范化 Search Shell Page，搜索结果由 HTMX Fragment 按需返回（**原记录「未注册 capability；当前白名单仅 `loginPanel`/`cartSummary`」已过时**：`searchResults` 已注册（`internal/module/runtimefragment/search_results.go:62`，参数 `q` 见 :72），构建期组件 `core.searchResults` 只输出搜索框 + 结果挂载点，命中由 `/_fragments/searchResults` 现拉 —— `internal/builder/components/searchresults/searchresults.go:3` 与 :18 的 `searchResultsPath`）

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 白名单 | ✅ 只允许 Registry 内 capability，拒绝任意 endpoint |
| 分页 | ✅ 通过（**原「❌ 未通过：未注册分页 capability」已过时**）：`productList` 已注册并按 `page`/`pageSize` 下推 offset（`internal/builder/components/productlist/jet.go:521`） |
| 搜索 | ✅ 通过（**原「❌ 未通过：未注册搜索 capability」已过时**）：`searchResults` 已注册（`internal/module/runtimefragment/search_results.go:62`），经收窄只读端口 `SetContentSearchProvider` / `SetProductSearchProvider` / `SetPublishedEntityLocator`（同文件 :51/:54/:57）取数，端口未注入时走降级文案、不报 500（同文件 :17/:39 的边界注释） |
| 安全 | Fragment 不读取 Page Document、不执行 Jet、不接受任意 endpoint |
| `go test ./...` | 全绿 |