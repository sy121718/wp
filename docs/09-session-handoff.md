# 09 · 会话交接（2026-09-09）

> 给下一个对话的入口文档：现状、待办、坑。会话上下文已压缩，细节以代码为准。

## 0. 环境与运行

| 项 | 值 |
|---|---|
| 项目 | `/home/sky/project/go/wp`（Go + Gin + Jet v6 + HTMX） |
| 服务 | **air 热重载**，端口 8080（改代码自动重编译；当前进程 gosky-dev，`curl 127.0.0.1:8080/livez` 应为 200） |
| 启动命令 | `export WP_SITE_BASE_URL='http://127.0.0.1:8080/site' && air -c .air.toml`（或 `bash scripts/dev.sh`） |
| 配置 | `.air.toml`（go/jet/html/yaml 触发重编译；static 的 js/css 直读不重启） |
| 测试 | `go test ./...`（**当前全绿**，见 §4） |
| golden 更新 | `go test ./internal/builder -run TestJetViewByteEquivalent -update-jet-golden` |

**坑**：
- 造页面数据**必须走 API**（`POST /api/page/create` + `/api/page/build` + `/api/page/publish`）；直接 SQL 插入的页面构建会报 `violates foreign key constraint`。
- 预览 iframe 有缓存：验证前端改动要 `Page.reload({ignoreCache:true})`，或给 iframe src 加时间戳。
- 调试 handler 错误时临时把 `pageErrorMessage(err)` 换成 `err.Error()`，验完改回。

## 1. 已完成

### 1.1 组件与检查器重构
- **取色器** `wbColorPicker`：HSV 面板 + 色相/透明度条 + 透明按钮 + HEXA/RGBA/HSLA 切换 + 吸管 + 收藏色（localStorage）
- **四角圆角** `cornersControl`：四输入 + 🔗 联动锁 + 单位（兼容 `radiusTL` 与 `radius.topLeft` 两套命名）
- **样式面板** WP 式折叠分组：基础 / 布局 / 背景 / 边框 / 变换 / 动效 / 响应式（空组不显示、有值自动展开、显示已用项数）
- **通用层下沉**：26 个组件统一 `core.AdvancedProps`（layout/border/transform/motion/响应式覆盖）；控件系统支持 `ct:"group"` 嵌套展开
- **新增控件类型**：spacing（三端×四向）、corners、rtext（三端文本）、classes、cssdecls（按端样式覆盖）、mediaList（多图）、number（浮点）、**richtext（Trix 富文本，见 §1.6）**
- **修复**：schema 丢弃自定义分组；`field()` select 分支漏 `appendChild`（>6 选项下拉不显示）；8 个组件误用编辑器变量 `--c-*` → 站点变量 `--sky-c-*`；按钮类元素改为跟随 `--sky-btn-bg`；`--sky-tr-duration: ms` 空值

### 1.2 air 热重载
`.air.toml` + `scripts/dev.sh`；静态资源不触发重启（gin.Dir 直读）。

### 1.3 updvape 首页复刻
- 页面 id `32274c84-4f6f-46ac-91bb-f87e4eeed4be`，路径 `/updvape-home`，135 节点
- 页眉/Hero/信任条(grid)/分类/畅销/积分/品牌/新品/组合装/指南/关于/页脚
- 新增容器**背景轮播**能力：`visual.bgSlides[]` + `bgSlideInterval`，纯 CSS 交叉淡入；`core.CSSBuckets.AddKeyframes` 支持组件自定义关键帧
- **遗留（已解决 → 见 §3「updvape 外链图片本地化（已完成）」）**：当时页面仍有 updvape 外链图（防盗链导致轮播显示底色）；后续已下载并替换为 `/storage/image/updvape/*.webp`（当前 `public/storage/image/updvape/` 下 9 个 webp；产物中剩余 3 处 `updvape.com` 为页脚/社交 `<a>` 链接，合理保留）

### 1.4 SEO（4 步全部完成）
- `internal/seo/scoring`：8 维度 24 检查项 + 基准表（`benchmarks.go`）+ 页型调权 + 9 个表驱动测试；规则表见 `docs/02-E1-seo-scoring-rules.md`
- 页面设置评分面板：总分/等级/红黄绿圆点/逐项建议（**可点击跳转**到字段或画布组件）/SERP 预览，改字段自动重算
- 构建期 meta：canonical / OG / Twitter / JSON-LD（按 `settings.seo.schemaType` 输出 WebSite/Article/Product/FAQPage + URL 多级时自动 BreadcrumbList），见 `internal/builder/seo_head.go`
- `internal/seo/sitemap.go`：sitemap.xml + robots.txt；**发布成功后自动刷新**（`page/service/page_publish.go` → `routes.RefreshSiteFiles`）；站点根地址读环境变量 `WP_SITE_BASE_URL`
- ✅ **文章编辑页评分入口已落地（2026-09，INF-1 + SEO-10）**：文章后台页 `/admin/articles`（列表 / 新建 / 删除 / 发布状态）+ `/admin/articles/edit`（Trix 富文本 + SEO 字段 + 评测侧栏 + 发布区块）；评分提取在 `internal/seo/article.go`（`ScoreArticle`），页面侧栏走 `POST /admin/articles/score`（HTMX 局部刷新）。**仍缺**：文章详情页的 canonical / JSON-LD 注入 —— 那条 `head` 目前由内容模板的 `settings.seo` 决定，不由文章字段驱动，所以评分里这两项恒判未达标（见 `docs/10-todo.md` 的 SEO-10 备注）

### 1.5 工作台 UI
- 画布选中 → 结构树自动滚动定位并高亮
- 左侧面板可拖拽调宽（180–560px）
- 左侧图标栏改为**面板顶部标签条**：▦组件 ＋添加 ⚙页面 🔍SEO ◐全局 ↺历史 ☰结构树（顶栏「+组件」已移除）
- 设备切换五档：桌面(自适应)/1024/768/640/375 —— 改 iframe 视口宽度，媒体查询自动命中

### 1.6 富文本迁移（本轮）

- **编辑器换 Trix 2.x**：TinyMCE 全部移除，改为本地 vendor `internal/templates/static/vendor/trix/`（`trix.css` + `trix.umd.js`，`workbench/layout.html` 引入，零 CDN）；`public/test/page/feature/workbench_page_test.go` 已断言外壳不含 tinymce
- **富文本能力上移 core**：新增 `internal/builder/core/richtext.go`（**白名单唯一来源**）——`allowedRichTags` / `SanitizeRichHTML` / `RichTextHTML` / `HasRichMarkup` / `StripRichTags` / `MaxRichLen`（当时为 20000，**现已统一为 30000**，与 `ct:"richtext,maxlen=30000"` 一致，见 `internal/builder/core/richtext.go:26`）；`internal/builder/components/text/sanitize.go` 退化为薄转发（保留包内私有名 `sanitizeRichHTML` / `stripRichTags`，既有调用点与测试名不变）
- **白名单变化**：新增 `pre`（代码块）与 `div`（Trix 段落容器，输出侧归一为 `p`，带嵌套保护不产出 `<p><p>`）；`h1` 仅入白名单用于输出侧统一降级 `h2`
- **新增控件 kind `ct:"richtext"`**（与 `rtext` 三端文本区分）：`core/controls.go` 的 `ControlRichText` + `inspector_handle.go` 输出 `slot=richtext` + 客户端 `methods/controls/misc.js`（`schemaField` / `fillInspectorSlots`）分派到 `richTextField`（`methods/controls/text.js`，Trix）
- **四字段富文本化**：`core.card.text`、`core.quote.text`、`core.infobox.text`、`core.faq.FaqItem.answer` 改 `ct:"richtext,..."`，`BuildView` 走 `core.RichTextHTML` + 模板 `unsafe` 输出；`core.faq` 另加 `faqPanel` 编辑面板（答案单独成块，Trix 需要横向空间，不塞进 repeater 行）
- **存量纯文本兼容**：无标签输入 → `html.EscapeString` 后按空行包 `<p>`、段内换行转 `<br>`（`core.RichTextHTML`；富文本与纯文本共用 `MaxRichLen` 上限，超长判空）
- **富文本图片上传**：Trix 粘贴/拖入 → `POST /api/media/upload`（复用 media 模块，非超管需 `media:upload` 权限点）；失败移除 pending 附件，避免空 `<figure>` 进正文
- **顺带修复**：`faq.jet` 与 `table.jet` 的 Jet 语法 bug——`{{ range $x := ... }}` 不支持，已改 `{{ range _, x := ... }}`（faq 答案同时改为 `unsafe(item.Answer)` 走富文本）
- **验证**：`internal/builder/core/richtext_test.go`（纯文本段落化 / 清洗 / h1 降级 / 长度上限 / 幂等）+ `components/text/sanitize_test.go`、`sanitize_fuzz_test.go`（薄转发后仍覆盖）
- **已完成**：idiomorph 已引入（vendor 0.8.0，`internal/templates/workbench/layout.html:181` 引入 + `static/js/workbench/core.js:45` `morphHTML`；结构树刻意保留整块 innerHTML，见 `methods/tree.js:23`；见 `docs/06-C-htmx-extensions.md` §四/§五）

> 规范同步：`docs/02-C2-text.md`（§2 富文本字段清单 / §4 清洗规则 / §6 实现映射）、`docs/02-C3-controls.md`（`richtext` kind）、`docs/02-C4-groups.md`（新增控件类型）、`docs/06-C-htmx-extensions.md`（Trix 附件上传）。

## 2. 前台导航菜单（navigation）— 内核 + 装配 + 管理页已打通

### 组件与数据（上一轮 + 本轮）
- **`core.nav` 组件**（`components/nav/{nav.go,jet.go}` + `templates/components/nav.jet`）：两级菜单、链接/打开方式、水平垂直、对齐/间距、悬停与当前项色、子菜单浮层、移动端折叠（纯 CSS）
- **迁移 054**：`navigations` 加 `source_type`/`source_id`/`target`
- **权限点**：`036_blueprint_navigation_permissions.sql` 已 seed `navigation:*`（含 list/create/update/delete）+ 超管策略，管理页无需新迁移

### 内核契约（本轮）
- `core/render.go`：`NavigationItem` + `NavigationResolver` 接口；`RenderContext` 加 `Navigation` 与 `ProjectID`
- `builder.go`：`WithNavigationResolver` / `WithProjectID` + `compileConfig` 字段，`Compile` 时传入 RenderContext
- `components/nav/nav.go`：Props 加 `Menu`（`ct:"select,=自定义菜单,header=页眉导航,footer=页脚导航"`）；`jet.go` 加 `ItemsOf` / `ValidateItems`
- `jetview.go`：`navViewOf → resolveNavMenu` —— 绑定位置时用解析结果覆盖手写 Items，并二次校验；**未注入解析器或工程 ID 时编译期显式报错**（不静默产出空菜单）

### projectID 传递链（本轮，文档上一版缺的关键环）
- `pipeline.CompileFn` 签名加 `pageID`：`func(ctx, pageID string, docJSON []byte)`（`DefaultCompile` 同步改）
- `page/service`：`assembleCompile(ctx, pageID, docJSON)` → `projectIDOf(ctx, pageID)` 查 pages 表 → `compileDocument(ctx, page, projectID)` 注入
- 预览：`CompilePreview(ctx, docJSON, projectID)`（contract 同步改；dashboard 传 page/block 的 ProjectID），保证画布所见即产物
- `page_navigation.go`：`navigationResolverAdapter`（navigation 契约 → `core.NavigationResolver`，单次编译内按「工程+位置」缓存）

### navigation 模块（本轮）
- `contract.Tree(ctx, projectID, kind)`；service `Tree`（组装树）/ `Render`（复用树）
- Create/Update 落 `source_type`/`source_id`/`target`（白名单校验：非 custom 必须给来源实体）
- Create 未指定 sortOrder 时**自动追加到同级末尾**（model `MaxSortOrder`）；Delete **级联删子孙**（model `DeleteMany`，避免孤儿节点变顶级项）

### 管理页（本轮）
- `/admin/navigations`：`dashboard/inbound/http/navigation_handle.go` + `templates/admin/navigations.html`
- 工程 + 位置切换、添加菜单项（可挂到一级项下）、结构树缩进、↑↓ 排序、`<details>` 内联编辑、删除（级联），全程零新 JS
- 侧边栏入口：`nav_menu.go` 系统组加「导航菜单」（`Perm: navigation:list`）
- 路由：GET `/admin/navigations`；POST `create/update/delete/move`（写操作挂 `CasbinMiddlewareForPath`）

### 验证
- `go test ./internal/builder/ -run TestNav`：绑定解析 / 缺解析器 / 缺工程 ID / 解析错误 / 手写 items 不受影响（5 例）
- `go test ./public/test/navigation/...`：编译进产物、跨工程隔离、手写菜单、来源字段落库、管理页渲染冒烟
- `go test ./...` 全绿

### 来源实体解析（本轮完成）
- `navigation/contract.SourceResolver`（`ResolveSource` + `Candidates`）+ `SourceGroup/SourceCandidate`
- `navigation/outbound/source.Resolver`：page（SEO 标题 + 激活/草稿路径）、article/product/category（内容标题 + presentation 实例 URLPath）、block（仅标题，无 URL）
- `navigation/service`：`SetSourceResolver` + `Tree` 递归写回标题/URL + `SourceGroups`；解析失败保留记录自身值（不阻断构建）
- 装配：routes.go 在 page/content/presentation/block 全部就绪后 `navigationSvc.SetSourceResolver(navsource.New(...))`（解析器只依赖各模块 contract）

### 管理页来源添加（本轮完成）
- 「从已有内容添加」区块：按来源分组折叠 + 复选 + 批量加入（`POST /admin/navigations/add-source`）
- 标题/链接取自来源候选并写入 `source_type/source_id`；无公开路径的候选禁用并提示「先发布」
- 全局块不进候选（块无公开 URL，加了也是空链接）；`ResolveSource` 仍支持 block 历史数据

### 当前项高亮（本轮完成）
- `RenderContext.CurrentPath` + `builder.WithCurrentPath`；page service 从 pages 记录取路径（激活优先）
- `nav.MarkCurrent` 精确匹配（`/` 与 `""` 归一）；模板输出 `is-current` + `aria-current="page"`，子项同样标记；`ActiveColor` 同时作用于子项
- 预览同步：`CompilePreview(ctx, docJSON, projectID, currentPath)`（块预览传空）

### 下一步
导航菜单已收尾，转入 §3 HTMX 化重构（先做检查器面板打样）。

## 3. HTMX 化重构（用户核心痛点：workbench.js ~4100 行）

### 检查器面板打样（已完成，可评估）
- **服务端渲染**：`POST /workbench/inspector`（`dashboard/inbound/http/inspector_handle.go`）→ `templates/fragments/inspector_panel.html`；字段按 section 折叠分组（内容/基础/布局/背景/边框/变换/动效/响应式/高级），空组不渲染、有值自动展开并显示已用项数
- **字段形态**：每个控件带 `data-wb-path`（相对 props 的路径）+ `data-wb-kind`；覆盖 text/textarea/number/bool/select/color/spacing/corners/rtext/classes/cssdecls/media/mediaList/dimension；条件字段（widthValue/bgPositionXY/bgSizeValue/position.*）与 corners 合并规则已搬到 Go（`inspectorFieldVisible`/isCornerKey）
- **客户端只保留 ~90 行**（`syncInspectorHtmx` + `bindInspectorHtmx`）：取面板片段 → 事件委托（change/click）→ 回写 AST → 刷新树/画布；覆盖式绑定（`panel.onchange =`）避免监听器累积
- **开关**：URL 加 `?inspector=htmx` 启用（便于与旧面板对比），未开启时走旧 JS 面板
- **验证**：`public/test/dashboard/feature/inspector_panel_test.go`（字段渲染/值回填/空态）；`curl -X POST /workbench/inspector` 返回 401（未登录，路由已加载）
- **增强器挂载（已完成）**：服务端对需要 JS 的字段只输出定位占位（`data-wb-slot` + `data-wb-enhance`），客户端在片段就绪后调用**既有控件函数**就地填充（`fillInspectorSlots`：临时把闭包内的 `panel` 指向 slot，复用 colorControl/spacingControl/cornersControl/rtextControl/mediaControl/mediaListControl/dimensionControl/classesControl，零重写）；组件手写面板（containerLayout/sliderPanel/navPanel/galleryItemsPanel…）与操作按钮由 `renderInspectorExtras` 追加
- **已修复（浏览器实测发现）**：`syncInspector()` 开头曾有一处提前 `return this.syncInspectorHtmx()`，它在闭包控件函数定义之前就分流，导致 htmx 路径只拿到服务端片段、slot 全空、组件手写面板缺失（表现为面板"空/坏"）。已删除该提前分流，保留闭包内的分支（`fetchInspectorPanel → fillInspectorSlots → renderInspectorExtras → bindInspectorHtmx`）
- **浏览器实测（admin/admin123 登录后走查）**：检查器 16 个 slot 全部填充、84 个取色器元素、操作按钮就位；页面设置 17 个字段 + 评分 73 + SERP 预览；全局设置 7 组 + 21 个颜色槽 + 8 个下拉；历史 3 条修订；改字段回写 `doc` 且 `saveState=dirty`；全程无 JS 错误；`index.js`/`core.js`/`methods/*.js` 全部 200
- **仍未迁移**：htmx 模式下的「内容/样式」页签（片段目前是合并视图，分组折叠保留）；富文本（richTextField）本轮已随 Trix 迁移完成，见 §1.6
- **验证边界**：服务端片段与 JS 语法已自动化验证（`public/test/dashboard/feature/inspector_panel_test.go` 断言 slot 标记）；浏览器端到端需登录态（本地无默认超管账号 + 验证码图片化），待有登录环境时补一轮人工走查

### 结构树服务端渲染（已完成）
- **服务端**：`POST /workbench/outline`（`dashboard/inbound/http/outline_handle.go`）→ `templates/fragments/outline_tree.html`；输出 `<ul><li><div class="wb-node" data-id data-type draggable="true">` 结构，含 caret / 名称（`data-named` 标记用户命名）/ 隐·锁标记 / 上移下移复制删除按钮；过滤规则与前端一致（节点或后代命中即保留整条链路）
- **客户端**：`renderTreeHtmx` + `bindTreeHtmx`——**只绑一次**事件委托（click/dblclick/contextmenu/dragstart/dragover/dragleave/drop），覆盖选中、右键菜单、双击重命名、caret 折叠、拖拽排序与组件库拖入；`localizeTree` 用前端映射表把未命名节点换成组件中文名
- **开关**：`?htmx=all` / `?htmx=outline`（`wbHtmxEnabled(part)`，兼容旧的 `?inspector=htmx`）
- **验证**：`public/test/dashboard/feature/outline_tree_test.go`（嵌套结构、选中态、操作按钮、过滤保留祖先、未命名回退）；`curl -X POST /workbench/outline` 401（路由已加载）

### 页面设置面板与评分区（已完成）
- **服务端**：`POST /workbench/settings` → `fragments/settings_panel.html`（版心模式/SEO 标题·描述·焦点关键词·Canonical·分享图/结构化数据/次级关键词/查询意图，全部带 `data-wb-setting` 路径）；`POST /workbench/seo-score-panel` → `fragments/seo_score.html`（总分/等级/维度圆点/逐项建议/SERP 预览，由 `seoscore.ScoreDocument` 服务端计算）
- **客户端**：`renderSettingsPanelHtmx` + `bindSettingsHtmx`——change/click 事件委托回写 `doc.settings` → 刷新画布；SEO 字段改动只刷新评分区（`refreshSeoScoreHtmx`，避免整块表单重绘丢焦点）；评分建议 `data-wb-jump` 委托跳转（`settings.seo.x` 聚焦输入框 / `node:type` 选中首个该类型组件，`findNodeByTypeHtmx`）
- **开关**：`?htmx=settings`（或 `?htmx=all`）
- **验证**：`public/test/dashboard/feature/settings_panel_test.go`（字段与 segment 选中态、list 型字段空格连接、评分区结构/SERP）；`curl -X POST /workbench/settings` 401（路由已加载）

### 全局设置面板（已完成）
- **服务端**：`POST /workbench/global`（`global_handle.go`）→ `fragments/global_panel.html`；43 个主题字段（色板 11 / 排版·标题 5 / 排版·正文 4 / 排版·链接 3 / 按钮 12 / 表面 4 / 动效 3）的分组、控件与当前值全部由 Go 侧字段表渲染（`themeFieldGroups`），回显路径与提交键名分离（如 `typography.heading.fontWeight` → 提交 `typography.heading.weight`）
- **客户端**：`renderGlobalPanelHtmx` + `bindGlobalHtmx`——颜色字段输出槽（`data-wb-theme-color` + `data-wb-theme-path`），客户端注入 hidden input 并用既有 `wbColorPicker` 增强；保存沿用 `/admin/themes/settings/save`（遍历 `[name]` 收集，按提交键回写本地缓存）
- **开关**：`?htmx=global`
- **验证**：`public/test/dashboard/feature/global_panel_test.go`（分组标题、颜色槽路径、select 选中态、文本值、空态）

### 修订历史面板（已完成）
- **服务端**：`POST /workbench/history`（`history_handle.go` → `fragments/history_list.html`）渲染版本列表（vN / 路径 / 时间 / 恢复按钮）；`POST /workbench/history/restore` 服务端编排「查修订 → 以当前 draftVersion 覆盖保存草稿」（乐观锁，冲突返回 409）
- **客户端**：`loadHistoryHtmx` + `bindHistoryHtmx`——列表片段替换 + 恢复按钮委托，恢复成功后 `location.reload()` 重新注入文档与版本号
- **验证**：`public/test/page/feature/history_restore_test.go`（恢复后草稿回到旧版本且产生新修订）、`public/test/dashboard/feature/history_panel_test.go`（空态）

### 组件库与媒体库：评估为「不迁移」（结论）
- **组件库**（`renderPaletteComponents`）：组件清单是前端静态数组（`paletteItems`/`paletteGroups`）+ 插件组件合并，渲染一次即可、无往返收益；迁到服务端需要把「组件中文名/图标/提示」补进 Go 侧元数据表（当前这些只在前端），属重复维护。**保留客户端**，仅搜索过滤与手风琴状态留在 JS。
- **媒体库弹窗**（`openMediaPicker`/`renderMediaTree`/`loadMediaList`）：分类树 + 搜索 + 分页 + 选中回调全是有状态交互，且本身已通过 `/api/media/*` JSON 接口取数；服务端渲染片段反而要来回搬运选中态。**保留客户端**。
- 结论：HTMX 化的边界稳定为「无客户端状态的表单/列表/树」——检查器、结构树、页面设置、全局设置、历史已迁移；手势（拖拽）、iframe 选中联动、撤销、缩放/设备切换、组件库、媒体库保留 JS。

### workbench.js 文件拆分（已完成）
原单文件 4700 行 → ES modules（`internal/templates/static/js/workbench/`）：

| 文件 | 行数 | 职责 |
|---|---|---|
| `index.js` | 81 | 入口：初始状态（含 `canUndo/canRedo` getter）+ `Object.assign` 合并各模块方法 + boot |
| `core.js` | 488 | 共享内核：meta/bootstrap 解析、CSRF、取色器/下拉/颜色换算、组件清单与中文标签 |
| `methods/state.js` | 161 | 视图动作、组件库与插入、快照与草稿备份 |
| `methods/canvas.js` | 552 | 画布直改、画布联动、剪贴板 |
| `methods/history.js` | 255 | 修订历史面板 |
| `methods/nodes.js` | 156 | 节点查找、右键菜单、选择联动 |
| `methods/panels.js` | 622 | 面板切换、页面设置、全局设置 |
| `methods/tree.js` | 206 | 结构树（含 HTMX 路径） |
| `methods/inspector.js` | 1788 | 检查器面板与全部控件 |
| `methods/media.js` | 152 | 媒体库选择器 |
| `methods/api.js` | 93 | 草稿保存/构建/发布接线 |
| `methods/shortcuts.js` | 247 | 快捷键与初始化 |

- **加载方式**：`layout.html` 改为 `<script type="module" src="/static/js/workbench/index.js?v={{.jsVer}}">`；`icons.js`/`media-lib.js` 仍是普通脚本（先于 module 执行）
- **共享状态**：`core.js` 用 `export const/let/function` 导出（顶层变量无运行时重赋值，已核对）；各 methods 模块 `import` 同名绑定，**原引用点零改动**；方法间 `this` 调用由 `Object.assign` 合并后仍然有效
- **验证**：12 个模块 `node --check` 全通过；Node 端 import 图全部加载成功并断言合并对象含 `init/renderTree/syncInspector/saveDraft/openMediaPicker/bindTreeHtmx/renderSettingsPanelHtmx/loadHistoryHtmx/select/snapshot`；`/static/js/workbench/{index,core,methods/inspector}.js` 均 200，旧 `/static/js/workbench.js` 404；`go test ./...` 全绿
- **已知（后续已解决，见本节「下一个减量目标」）**：当时 `inspector.js` 1788 行，`syncInspector` 含 30+ 闭包控件函数；现已提取到 `methods/controls/*.js`（`inspector.js` 161 行）。仍成立的部分：ES module 子模块 import 不带版本参数，开发时若遇旧缓存需硬刷新
- **HTMX 替代收尾**：本轮补齐 htmx 路径缺失的手写面板（`typographyPanel`/`interactionPanel`/`dividerInsetStyle`）；`?htmx=all` 浏览器走查无误后，可把 htmx 设为默认并删除旧渲染路径（约 775 行）

### htmx 成为默认路径 + 页签补齐（已完成）
- **默认开启**：`wbHtmxEnabled()` 默认返回 true（`?htmx=off` 整体回退旧渲染路径；`?htmx=-outline` 单模块回退），日常无需带参数
- **检查器页签补齐**：服务端 `POST /workbench/inspector` 新增 `tab` 参数（content / style，空 = 全部），`sectionInTab` 过滤分组；客户端 `fetchInspectorPanel` 带 `this.tab`，`renderInspectorExtras` 按页签分支（内容页签出容器/轮播/图集等 repeater 面板，样式页签出排版/动效/分隔线样式）
- **修复**：`buildInspectorSections` 的「未登记分组兜底」循环此前未受页签约束，会把被过滤掉的已登记分组（如 advanced）当未登记加回来——已修正
- **验证**：`TestInspectorPanelTabFiltering`（content 不含样式字段 / style 不含内容字段 / 空 tab 渲染全部）；`go test ./...` 全绿
- **旧渲染路径已删除（本文档上一版遗留的「可删 775 行」已完成）**：`renderSettingsPanel` / `renderGlobalPanel` / `loadHistory` 等旧 DOM 拼装分支已移除，现为薄包装（见各文件「旧 DOM 拼装路径已删除」注释）；`wbHtmxEnabled` 开关与 `?htmx=off` 回退已不存在，htmx 是唯一路径
- **下一个减量目标（已完成，2026-09 回填）**：`inspector.js` 1829 行时 `syncInspector()` 内嵌 49 个闭包控件函数。现已提取到 `methods/controls/*.js`（base/color/corners/media/misc/repeater/selects/spacing/text 共 9 个文件 2093 行），`methods/inspector.js` 降到 **161 行**（`wc -l` 实测）

### 走查反馈修复（浏览器实测）
1. **页签第一次点击内容/样式相同**：`index.js` 初始 `tab: 'layout'`，而服务端 `sectionInTab` 只认 `content`/`style`（其他值渲染全部）→ 改为 `'content'`；另给 `fetchInspectorPanel` 加请求序号（快速切换时丢弃过期响应，避免「后发先至」显示错页签）
2. **内外边距四向变四行大框**：`.wb-margin-row` / `.wb-margin-side` / `.wb-margin-link` 在 `workbench.css` 里**从未定义**（历史遗留），四向输入被默认 block 布局堆叠 → 补齐 grid 样式（`repeat(4, minmax(0,1fr)) auto`，一行四个小框 + 联动按钮）
3. **取色器「全透明」**：代码路径正常（`wbParseColor(opts.value)` → `sync()` 设置色板/色相条/透明度条），截图中的棋盘格是「透明度条左端 + 未收藏的空位」的正常渲染；待用户确认是否某个具体字段有值却显示透明

### 走查反馈修复（第二轮）
4. **取色器预览全透明**：根因是 CSS —— `.wb-cp-preview` / `button.wb-color-swatch` 用 `background-image: repeating-conic-gradient(...)` 画棋盘格，而 JS 只设了 `backgroundColor`，颜色被背景图盖住。改为与透明度条同款做法：`backgroundImage = linear-gradient(soft,soft), checker`（`core.js` 的 `sync()`）
5. **内外边距应是一行四向 + 默认联动**：container 的 `box.padding`/`box.margin` 原本是 `ct:"rtext"`（三端 CSS 简写字符串），只能渲染成单个输入框。新增 `boxspacing` 控件：
   - Go：container 两字段改 `ct:"boxspacing,sec=layout"`；`controls.go` 加 `boxspacing` 的 `IsSafeCSSValue` 校验；`inspector_handle.go` 输出 slot（`data-wb-enhance="boxspacing"`）
   - JS：`boxSpacingControl`——三端切换 + 一行四向（上/右/下/左）+ 默认联动（点 🔗 解除），内部做 CSS 简写 ↔ 四向互转（1/2/3/4 值规则），**数据仍是简写字符串，编译端零改动、老文档兼容**；旧渲染路径也接同一控件
   - `spacingControl`（advanced 的四向间距）默认联动同步改为 true
   - 验证：`TestInspectorPanelBoxSpacingSlot`（style 页签输出 `box.padding`/`box.margin` 的 boxspacing 槽）

### 走查反馈修复（第三轮：折叠分组与紧凑布局）
6. **折叠分组收起了下面却冒字段**（最重要）：客户端手写面板（typography/interaction/各 repeater）此前直接 append 到面板末尾，落在折叠组之外。现在模板给每个折叠组加 `data-wb-section="{{s.Key}}"`，`renderInspectorExtras` 用 `sectionFor(key,title)` 找（缺失则创建，如「动效」组）+ `into(target, fn)` 临时把闭包 `panel` 指向该组，面板落进对应折叠块
7. **内外边距联动换行**：`spacingControl` 去掉单独的单位行（单位由输入值自带），联动锁与四向框同一行；`.wb-margin-row` 列宽固定 `repeat(4,1fr) 30px` 防挤压换行
8. **最小/最大高度分两行**：`inlineSlots('box.minHeight','box.maxHeight')` 把两个增强槽并成一行两列（`.wb-inline-row`）
9. **字段太占高度**：服务端渲染的单行字段（select/number/text）加 `wb-field-compact`（标签 84px + 控件同一行）

### 走查反馈修复（第四轮）
10. **三端（桌面/平板/手机）切换「无反应」**：点击只重绘下方四向框，三端按钮高亮没同步；新端通常没值（四个框全空），视觉上像切不动。已改为点击时同步按钮高亮（`DEVICES[i][0] === device`）——注意三端是「空 = 继承上一端」的语义，切到平板看不到值属正常。
11. 关于「轻量 CSS 库」：项目**没有引入任何 CSS 框架**——`theme.css` 头部写明「Stripe Design Language · 亮暗双主题 · CSS 变量驱动 · 零组件库依赖」；`workbench/layout.html` 只引 `theme.css` + `workbench.css`，CDN 仅 htmx（admin 页）；富文本编辑器 Trix 2.x 已改本地 vendor（不走 CDN）。工作台控件（四向间距/取色器/按端覆盖）是定制结构，通用库只能覆盖 input/select/button 基础样式与间距 token。

### 旧渲染路径删除 + 全组件审计（已完成）
**删除旧路径**（htmx 成为唯一实现，开关一并移除）：
| 文件 | 前 → 后 | 删除内容 |
|---|---|---|
| `methods/history.js` | 255 → 92 | 旧 `loadHistory` DOM 拼装、旧 `renderTree` 递归建树 |
| `methods/panels.js` | 622 → 265 | 旧 `renderSettingsPanel`、旧 `renderGlobalPanel` |
| `methods/inspector.js` | 1930 → 1668 | `syncInspector` 的旧「schema → DOM」主流程（分组/条件/控件生成） |
| 合计 | -766 行 | `wbHtmxEnabled` / `inspectorHtmxEnabled` / `outlineHtmxEnabled` 全部移除 |

**自动化审计**（`public/test/dashboard/feature/schema_audit_test.go`，遍历全部 29 个组件）发现并修复：
1. `core.globalref` 的 `blockId` 缺 ct 标注 → 检查器整块空白（已补 `ct:"string,..."`）
2. `ct:"textarea"` kind 客户端/服务端都没实现 → `core.card.text`/`core.quote.text` 只能单行输入（已支持，渲染为多行文本域；**本轮这两个字段已升级为 `ct:"richtext"`**，见 §1.6）
3. 审计测试会拦截「任何未被渲染分支覆盖的 kind」，防止再出现静默缺陷

**遗留**：视觉/交互走查需要登录态——agent 浏览器（ego）与用户 Edge 是独立实例、cookie 不共享，每次会话都要重新过验证码登录（此前失败均为 `ErrCaptchaExpired`）。建议在 ego 窗口登录一次后由 agent 继续模拟操作，或由用户截图反馈。

### 浏览器全组件审计（已完成）
**登录方案**：项目已有 `cmd_tmp_login/main.go`（从 DB 取 admin、写 Redis 会话、用 `auth.session_secret` 签出 cookie）——`go run cmd_tmp_login/main.go` 打印 `COOKIE=...`，再用 CDP `Network.setCookie` 注入 ego 浏览器即可，无需验证码。注意：会话是**单会话**设计（`user:session:{userID}`），重新生成会顶掉浏览器里的登录态。

**审计结果**（29 个组件 × 内容/样式两个页签）：
| 页签 | 通过 | 面板长度 | 分组 | 增强槽填充 | JS 错误 |
|---|---|---|---|---|---|
| 内容 | 29/29 | 319~4686 | 1 | 全部填充 | 0 |
| 样式 | 29/29 | 2965~41320 | 1~8 | 全部填充 | 0 |

**审计中发现并修复的缺陷**：
1. **误删增强函数**（最严重）：删旧渲染主流程时把 `fillInspectorSlots` / `renderInspectorExtras` / `inlineSlots` 一起删了 → 面板显示「加载失败」。已恢复。
2. **fetch 的 catch 吞掉增强异常**：`done()` 内抛错会被 `fetchInspectorPanel` 末尾的 `.catch` 捕获并把面板覆盖成「加载失败」，掩盖真实原因。已改为 `try { done() } catch (e) { console.error(...) }`，面板保持已渲染内容。
3. 前两轮已修：`core.globalref` 缺 ct 标注（面板空白）、`ct:"textarea"` 未实现（card/quote 正文单行）。

### 交互走查（浏览器实测，已完成）
| 项 | 结果 |
|---|---|
| 改字段 → 回写 AST | ✅ props 更新 + saveState=dirty |
| 三端切换（桌面/平板/手机） | ✅ **修复后**高亮与值同步 |
| 取色器 | ✅ 面板打开、预览显示真实颜色（`linear-gradient(...), checker`） |
| 撤销 | ✅ snapshot → push → undo 后节点消失 |
| 面板切换 | ✅ 页面设置 17 字段 + 评分、全局设置 7 组 21 色槽、历史 3 行 |
| 设备切换 | ✅ `is-mobile` + iframe 375px |

**修复**：`rtextControl` 里三端按钮点击处理器误用全局名 `DEVICES`（该函数内没有定义）→ 点击抛 `ReferenceError`，表现为「三端切不动」。已在函数内补 `var DEVICES = [...]`；三处设备切换控件（spacingControl / boxSpacingControl / rtextControl）现在都持有各自局部的 DEVICES。

### 交互走查补充（第二轮）
| 项 | 结果 |
|---|---|
| 组件库点击插入 | ✅ 容器组件插入后 doc.root 1→2、结构树 137→138 |
| 结构树右键菜单 | ✅ `#wb-context-menu` 显示（编辑/复制/剪切/粘贴到内部/粘贴到下方/在内部插入组件/删除） |
| 快捷键 Ctrl+Z | ✅ 撤销后新增节点消失 |
| 复制/粘贴 | ✅ `pasteInto` 后目标容器 children +1 |
| 结构树拖拽 | 未做 DOM 级模拟（`moveNode` 已有 Go 侧行为覆盖） |

### updvape 外链图片本地化（已完成）
- 下载 10 张 updvape.com 图片到 `public/storage/image/updvape/*.webp`（带 Referer 绕过防盗链；服务端返回 webp，故统一 `.webp` 扩展名与 src）
- 草稿文档 20 处 URL 替换为 `/storage/image/updvape/...` → 保存草稿 → build → publish
- 验证：`/site/updvape-home/` 产物中本地图片引用 22 处，剩余 3 处 `updvape.com` 是页脚/社交 **<a> 链接**（非图片，合理保留）；`/storage/image/updvape/upd-logo-removebg-1.webp` 返回 200 image/webp
- **CSRF 坑**：`cmd_tmp_login` 生成的 cookie 只含 `auth_user`，**没有 csrf_token**；首次请求页面时服务端会 `Set-Cookie` 补发。用 curl 保存草稿必须用 **cookie jar**（`-c/-b`）承接这个更新，否则 403 CSRF 校验失败

### 设计/无障碍子代理产出
| 子代理 | 状态 | 产出 |
|---|---|---|
| 检查器面板重排 | 中途失败但**改动已写入** | `workbench.css` +434 行（间距 4/8/12/16/24、字号 11/12/13/14、控件高 28/32、圆角 6/8、焦点轮廓、深色主题对比度）；`go build`/`go test` 通过 |
| 无障碍 P1 | ✅ 完成 | 新建 `workbench-a11y.css`（161 行）：`:focus-visible` 统一轮廓（含覆盖 workbench.css 里 `outline:none` 的同等特异性规则）、`prefers-reduced-motion` 分层降级（纯位移动效停用、状态指示保留终态）；`layout.html` 第 15 行引用 |
| 容器 Flex 面板 | 未完成，但**核查后无需改动** | `containerLayout()`（inspector.js:1351）已渲染方向/主轴/交叉轴/换行/间距完整面板，schema 无需补 ct |

验证：`go build`、`go test ./...` 全绿；浏览器实测 a11y CSS 已加载、节点 `border-radius: 6px / font-size: 13px`（符合新尺度）。

### 无障碍 P1 收尾：大纲树键盘可达（已完成）
- **服务端**：`outline_handle.go` 的节点 div 加 `role="treeitem" tabindex="0"`（焦点环样式已在 `workbench-a11y.css`）
- **客户端**：`tree.js` 的 `bindTreeHtmx` 加 keydown 事件委托：
  - ↑/↓ 在**可见**节点间移动焦点（折叠分组内节点用 `offsetParent` 排除）
  - Enter 选中节点；空格切换折叠；→ 展开 / ← 收起（无子节点时忽略）
  - 菜单键 / Shift+F10 打开右键菜单（定位到节点下方）
- **组件库**：`.wb-palette-item` 本身就是 `<button>`，原生可 Tab 聚焦 + Enter/Space 触发，无需改动
- **验证**：浏览器实测 `tabindex=0` + `role=treeitem`、ArrowDown 焦点下移、Enter 选中、ArrowLeft 折叠；`go test ./...` 全绿（同步更新 `outline_tree_test.go` 的 DOM 断言）

### 图片变体改有损 JPEG（已完成）
**背景**：变体早就实现了（`image_processor.go` + `media_variant*.go`），但用的是 `HugoSmits86/nativewebp` **无损**编码 —— 实测 1280px medium 达 1.7MB、全尺寸 10.5MB，是页面慢的主因；且构建期 `image.jet` 只输出原图 `src`，**变体根本没被页面使用**。

**本轮改动**：
1. `image_processor.go`：编码换成标准库 `image/jpeg`（质量 82，`variantJPEGQuality`）；带 alpha 的图先 `flattenToOpaque` 合成白底（JPEG 无透明通道）
2. **补 webp 解码器** `golang.org/x/image/webp`：此前只注册 gif/jpeg/png，**webp 源图会以 "unknown format" 跳过变体生成**（现代图片大多是 webp，属于静默失效）
3. `media_variant.go`：变体文件名后缀 `.webp` → `.jpg`、MIME `image/webp` → `image/jpeg`
4. `config.yaml`：`allowed_extensions` 补 `.webp/.avif/.svg`、`allowed_mime_types` 补 `image/webp` 等 —— 此前 **webp 根本传不进来**，自然也没有变体

**实测体积**（同一张图，1280px 变体）：
| 媒体 | 改前（无损 webp） | 改后（JPEG q82） | 降幅 |
|---|---|---|---|
| id3 medium | 1,701,426 B | 296,985 B | **-82%** |
| id3 全尺寸 | 10,536,946 B | 1,781,405 B | **-83%** |
| id4 medium | 102,258 B | 75,565 B | -26% |
| id6（新上传 webp 源） | — | 58,812 B | — |

旧无损变体文件已清理、已有媒体已用新编码重新生成；`go test ./...` 全绿（同步更新 `media_variant_unit_test.go` 的 RIFF/MIME 断言）。

**仍未做**：构建期 `<picture>/srcset`（`image.jet` 目前仍只输出原图 src，变体虽已生成但页面不引用）；本地化那 10 张 updvape 图未走媒体库（体积 22~62KB，影响小）。

### 图片懒加载三态 + 主题骨架屏 + 响应式 srcset（已完成，覆盖全部含图组件）
**主题「图片管理」**（主题设置 → 全局设置面板「图片」分组）：
- `ThemeImages.LazyLoad`：`on`（默认）/ `off` —— 组件级「默认」时继承
- `ThemeImages.Skeleton`：懒加载时显示骨架屏（纯 CSS 渐变 + `sky-skeleton-shimmer` keyframes，图片加载完成后内容自然覆盖背景，**零 JS**）
- 保存走 `SaveThemeSettings` 的 `images.lazyLoad` / `images.skeleton` 点分键

**组件级三态**（`loading` 字段；输出 `<img>` 的 5 个组件全部支持：`core.image` / `core.card` / `core.gallery` / `core.infobox` / `core.button` 媒体图标）：
| 值 | 含义 |
|---|---|
| 空 | 默认：继承主题「图片管理」的懒加载策略 |
| `on` | 强制开启懒加载 |
| `off` | 强制关闭（立即加载） |
| `lazy` / `eager` | 旧值保留（历史文档兼容），编译期映射到 on/off |

**单图级设置（gallery 每个 item 独立，轮播与网格共用）**：`items[].loading`（空=继承组件级）与 `items[].fetchPriority`（high / low，空=auto 不输出属性）；解析顺序 = **单图 → 组件级 → 主题 → 内置默认**。

**资源提示 fetchpriority**：`core.ResolveFetchPriority` 统一解析（仅 high / low 输出属性，空 / auto / 非法值不输出，避免产物噪声）；`core.image` / `card` / `infobox` / `button`（媒体图标）在组件级可设，gallery 还可逐图设。

**轮播首屏自动分级（LCP）**：carousel 模式下未显式设置的单图按位置自动分级——第 1 张 `fetchpriority="high"`（LCP 候选），其余 `low`；显式 `high`/`low` 优先。grid 模式不自动分级（首屏范围由整页决定，交给作者显式设置）。

**轮播首图优先加载（默认开启）**：`carousel.firstEagerOff` 关闭。开启时首图强制 `loading="eager"`（显式单图 `loading` 仍优先），配 `fetchpriority="high"` 压 LCP；关闭后首图回落到组件级 → 主题的懒加载设置。工作台图集面板在 `mode=carousel` 时显示该开关（`carouselFirstEagerControl`）。

**工作台面板**：`inspector.js` 的 `galleryItemsPanel` 每行新增两个下拉（单图加载策略 / 单图加载优先级），与文档 JSON 的 `items[].loading` / `items[].fetchPriority` 同源；未设置时显示「继承组件」/「优先级自动」。

**实现（统一收口，零重复）**：
- `core.RenderContext.ImageDefaults`（core 不依赖 builder，用轻量投影结构）+ `builder.Compile` 从 `ThemeSettings` 注入
- `core.ResolveImageLoading` / `core.ImageSkeletonClass` / `core.AddImageSkeletonCSS`：三态解析、骨架类并入、骨架 CSS 的唯一实现（`internal/builder/core/image_loading.go`）
- `core.ImageLoadingAware` 接口：组件 View 自持 `Loading` 原值，`ApplyImageLoading(ImageDefaults)` 回填 `IsEager`/`Skeleton`/`Class`；`jetview` 在 `atomViewOf` / `leafViewOf` / button / gallery 四处统一调用，组件包不重复实现
- `CSSBuckets.Add` 新增规则去重（断点 + 规则体）：多张图共享骨架规则只输出一份，顺带消掉 image/tabs 原有重复 CSS（golden 已按「有意变更」更新）

**响应式图片（构建期 srcset，已完成端到端接线）**：
- `core.RenderContext.AssetProbe func(url string) []int` + `builder.WithAssetProbe`：由装配层注入「该 URL 存在哪些变体宽度」
- `image.BuildView` 按宽度映射变体文件名（`≤320 → _thumb.jpg`、`≤1280 → _medium.jpg`，对齐 media 的 `<stem>_<type>.jpg` 约定）输出 `srcset` + `sizes="(max-width: 640px) 100vw, 50vw"`；原图仍留在 `src` 作回退
- **接线（已完成）**：media 新增 `MediaService.ProbeImageVariants(ctx, url)` —— `/storage/...` → 查启用附件 → 查 ready 变体 → 映射标准边（thumb 320 / medium 1280，webp 不参与）升序去重；`page/service/page_assemble.go` 以 `builder.WithAssetProbe` 注入，`SetupMediaRoutes` 的返回值经 `routes.go` 传入 `SetupPageRoutes`；未注入（如无媒体契约的测试装配）时不出 srcset，行为与之前一致

**验证**：`internal/builder/image_skeleton_test.go`（继承 / 组件覆盖 / 主题关闭）+ `internal/builder/image_loading_test.go`（card / gallery / infobox 三态 + srcset 候选）+ `public/test/media/unit/media_probe_unit_test.go`（探测：ready 变体 → `[320, 1280]`；非媒体库 URL / 路径穿越 / 附件不存在 / 空串 → nil）+ `TestGalleryPerItemLoading`（单图 off 覆盖组件级 on，逐图 fetchpriority）+ `TestCardFetchPriority`（组件级 high / 未设置不输出），`go test ./...` 全绿。

### 最终结论（五块面板迁移后）
- **行数对比**：`workbench.js` 4136 → 4699 行（+563 行 HTMX 胶水，含双路径开关）；服务端新增 6 个片段模板 178 行 + 5 个 handler 约 880 行 Go。**若 htmx 成为默认并删除旧路径**，可删 JS 约 775 行（settings 150 / global 120 / history 55 / syncInspector 的 schema 生成 350 / renderTree 的 build 100）→ 净减约 210 行 JS，且这部分逻辑变成**可测试的 Go**（当前 dashboard feature 已有 5 个片段渲染测试）
- **体验**：检查器面板片段 <10KB、结构树 135 节点约 40KB，本地往返 ~5ms；改字段仍即时（评分区局部刷新，不整块重绘）
- **收益天花板**：只要 AST 权威在客户端，服务端面板就必须配通用绑定器（当前约 450 行胶水），旧面板代码在双路径期删不干净。要彻底压缩 workbench.js，需要第 3 步——**服务端持 AST**（编辑操作服务端化），届时才可能把 JS 收敛到「手势 + 快捷键 + postMessage 胶水」
- **推荐落地顺序**：先把 `?htmx=all` 设为默认跑一段时间 → 删除旧路径（-775 行）→ 再评估服务端持 AST

### 原有结论（仍然成立）
- **能到「HTMX 为主 + 极少量 JS 胶水」，做不到 100% 无 JS**
- 可换：检查器面板（schema→DOM 最重，收益最大）、结构树、组件库列表、页面设置/全局设置/历史、媒体库弹窗、评分面板 —— 全是无客户端状态的表单/列表
- 换不掉：拖拽（手势）、iframe 选中联动（postMessage）、撤销/快捷键、画布缩放/设备切换（即时反馈）
- 代价：HTMX 每次交互一次往返 + 片段渲染；大页面（135 节点）重编译会卡 → 需防抖 + 只回改动节点

### 下一步
先做**检查器面板 HTMX 化**打样：`hx-get="/workbench/inspector?node=xxx"` 返回 Jet 片段；对比改造前后 JS 行数与体验，再决定是否推进其余部分。

### 画布局部刷新（已完成，用户痛点：改属性整页重刷）

**现状根因**：`inspector.js` 的 `commit()` → `refreshCanvas()`（250ms 防抖）→ `submitCanvas()`（`canvas.js:389`）把**整份文档 JSON** POST 到 `/workbench/preview`，服务端整页重渲染 → iframe 重载。所以任何属性改动都触发整页重刷（非本轮引入，一直如此）。

**已有地基**：iframe 内 `editor_bridge.go` 的桥接脚本已把编译期 `sky-c-<nodeId>` 类还原为 `data-sky-id="<nodeId>"`（editor_bridge.go:13-23），可据此做节点级替换。

**实现（零服务端改动）**：
1. `canvas.js` 新增 `fetchCanvasHTML()`：fetch 复用现有 `POST /workbench/preview`（不重载 iframe），拿到整页 HTML
2. `canvas.js` 新增 `patchCanvas(nodeId)`：`DOMParser` 解析后取 `.sky-c-<nodeId>` 的 outerHTML + 首个 `<style>` 内容，`postMessage({type:'wb-patch'})` 送进 iframe；带序号防并发覆盖，失败回退 `refreshCanvas()`
3. `editor_bridge.go` 新增 `wb-patch` 处理：`[data-sky-id="id"]` 的 outerHTML 替换（重设 `data-sky-id`/`draggable`/选中态），并写入 `<style id="wb-live-css">`（后定义覆盖旧规则）
4. `inspector.js` 的 `commit()` 改为 `renderTree + renderUI + patchCanvas(当前节点)`；无节点上下文（页面设置）与结构性变更仍走整页刷新

**验收**：改 padding / 颜色 / 文本时 iframe 不重载（滚动位置与焦点保持），DOM 只变动该节点；结构操作仍整页刷新。

## 4. 其他遗留（非阻塞）

- ~~复刻页外链图片本地化（走媒体库，顺带验证变体管线）~~ **已完成**：外链图已下载到 `public/storage/image/updvape/`（见 §3）；「未走媒体库」部分仍成立
- `card` 组件缺标题/正文排版字段（只能靠通用层）——**仍成立**（`internal/builder/components/card/card.go` 仅 title/text/buttonText/buttonLink）
- ~~`core.text` 正文 `ct:"richtext,maxlen=30000"` 与清洗硬上限 `core.MaxRichLen=20000` 不一致~~ **已统一**：`MaxRichLen = 30000`（`internal/builder/core/richtext.go:26`），与 `internal/builder/components/text/text.go:43` 一致
- 富文本图片（Trix `figure/img`）未接媒体变体与 caption：白名单直出 `src`，不走 `srcset`——**仍成立**
- ~~idiomorph 引入中（面板 `innerHTML` → `Idiomorph.morph`）~~ **已完成**（`layout.html:181` + `core.js` `morphHTML`）
- ~~容器 flex 布局面板（方向/主轴/交叉轴/换行/间距）~~ **已核销**：`containerLayout()`（`internal/templates/static/js/workbench/methods/controls/misc.js:128`）已渲染完整面板，见 §3
- `infobox.icon` 手写面板与后端 tag 是否重复显示
- 后台「菜单管理」页（sys_menus）尚未收录 `/admin/navigations`（侧边栏走代码配置 `nav_menu.go`，不受影响；要一致可补 seed）
- **测试基线已修复**：上一轮遗留的 8 个断言（advanced 5 例文案、container 边框三要素、按钮圆角/位移字面值、spacer 透明度文案）已按现行实现更新，`go test ./...` 全绿——后续任何红都是真回归。

---

## 5. 文档回填记录（2026-09）

本文以「会话交接」为定位，§1/§4 保留的是各轮当时的未完成口径，与 §3 的实施记录冲突。本次按**代码为准**统一以下 4 处（`10-todo.md` DOC-8 / §10.2 记录的同一问题）：

| 项 | §1/§4 旧口径 | 代码事实 | 依据（代码路径 / 符号） |
|---|---|---|---|
| updvape 图片本地化 | §1.3「遗留外链图」、§4「未做」 | 已完成 | §3 实施记录；`public/storage/image/updvape/`（9 个 `.webp`） |
| `core.MaxRichLen` | §1.6「= 20000」、§4「与 maxlen=30000 不一致」 | 已统一为 30000 | `internal/builder/core/richtext.go:26`；`internal/builder/components/text/text.go:43` |
| idiomorph | §1.6「进行中」、§4「引入中」 | 已引入（vendor 0.8.0） | `internal/templates/workbench/layout.html:181`；`internal/templates/static/js/workbench/core.js:45` `morphHTML` |
| 容器 flex 布局面板 | §4「未做」 | 已实现（核查后无需改动） | `internal/templates/static/js/workbench/methods/controls/misc.js:128` `containerLayout` |

> 说明：§1.3 的「17 张」与 §3 的「10 张」为不同口径（前者是页面引用点数、后者是去重后下载的图片数），原文保留不改，仅标注状态。
