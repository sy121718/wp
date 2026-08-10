# 01 · 概览与边界

> 本文集中存放 go_wp 的产品定位、总体架构、技术栈、仓库模块边界，以及作为速查表的「冻结边界」。其他协议细节见同目录其余文档。

## 1. 产品定位

go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`，不是访问时动态 Theme Runtime，也不是通用 Headless CMS Builder。

### 1.1 核心术语

```text
Admin                整个管理后台
CMS Admin            固定业务内容管理模块
Visual Builder       可视化页面构建器
Project              站点级构建工程
Blueprint            创建 Page Document 时使用的版本化初始化蓝图
Page                  绑定一个规范化公开 URL、可独立构建与发布的页面实例
Page Document         某个 Page 独占的可编辑组件 AST
Publish Compiler      发布期把 Page Document 与 CMS 数据编译为静态产物
ArtifactStore         保存不可变 HTML/CSS/JS 构建产物
PublicationStore      原子激活、回滚或取消某个 URL 的 Artifact
BuildComponent        在发布期完整编译为静态 HTML/CSS 的组件
RuntimeFragment       由白名单服务端处理器按需返回 HTML 的动态片段
ClientEnhancement     对已有 HTML 渐进增强的受控客户端行为
ContentTemplate       为内容实体类型（Product/Article/Category）设计的版本化页面结构定义，参与每次构建
PresentationInstance  内容实体（Product/Article/Category）的自动发布页面实例，含已解析的完整 AST 快照
DocumentSnapshot      构建时从 ContentTemplate 派生、含已解析数据的完整不可变 AST 快照
GlobalComponentUpdatePolicy  全局组件新版本发布时对已有页面的更新策略（immutable / auto-update / pinned）
```

系统只管理固定 CMS 业务：

- Page
- Post / Article
- Product
- Category / Tag
- Menu
- Media
- SiteSettings
- User

CMS Admin 保存内容。Visual Builder 保存内容的页面结构和展示规则。二者不得把对方的数据复制进自己的协议：

```text
CMS Content
+
Page Document
+
Build-time Registry
→ Publish Compiler
→ Immutable Page Artifact
→ Public URL
```

### 1.2 核心边界

- Page 是可独立构建、发布、回滚和取消发布的手工页面实例。PresentationInstance 是内容实体（Product/Article/Category）的自动发布页面实例，使用 ContentTemplate 派生 DocumentSnapshot 后经同一流水线生成 Artifact。
- 单个公开 URL 可以对应一个 Page 或一个 PresentationInstance，两者独占其 URL。
- Article 与 Product 等 CMS 内容若需要公开详情 URL，必须各自关联 PresentationInstance，再由该实例生成 Artifact：`Product → PresentationInstance → DocumentSnapshot → Artifact`、`Article → PresentationInstance → DocumentSnapshot → Artifact`。
- 禁止 `Product → Template → Runtime Render`、`Article → Template → Runtime Render` 等访问时套模板路径。
- Page Document 描述页面结构、组件组合、样式、Binding 和资源需求，不保存文章正文、商品库存、菜单项等实际业务数据。
- CMS 内容通过与 `PageKind` 匹配的固定 `ContentTarget` 和白名单 Binding 在发布时注入。
- Blueprint 只是 Page Document 的初始化工具。创建完成后，Page Document 是独立快照，Blueprint 不参与构建、发布或访客请求，后续修改也不自动传播。
- Go + Jet 只在 Preview/Publish 构建阶段运行，不参与普通访客页面渲染。
- 访客直接读取已经激活的 HTML/CSS/JS Artifact。
- 库存、购物车、登录状态和实时价格等实时能力优先通过 HTMX Runtime Fragment 提供；只有纯客户端状态或必须在浏览器执行的行为才使用 Client Enhancement。

```text
CMS 内容实例 ≠ DocumentSnapshot
Page ≠ CMS 展示模板
PresentationInstance ≠ Page（前者自动，后者手工）
Blueprint = Page Document 初始化工具
ContentTemplate = PresentationInstance DocumentSnapshot 的版本化结构来源
Blueprint ≠ 构建期或运行时模板
ContentTemplate ≠ 运行时模板（仅参与构建期派生）
Page Document ≠ CMS Content
DocumentSnapshot = 已解析的完整 AST
Artifact ≠ 可编辑源码
```

### 1.3 公开 URL 范围

所有有限、可枚举、规范化的公开 URL 都进入页面构建图：

- Home
- 普通 Page
- Article / Post Detail
- Product Detail
- Category / Tag / Archive
- 有限分页
- Search Shell（例如 `/search/`，查询结果本身不静态枚举）
- 404

无限查询空间不生成独立静态结果 Page，例如不会为每个 `/search/?q=...` 建立 Page。系统只发布一个规范化 Search Shell Page，搜索结果由其中的 HTMX Runtime Fragment 按需返回 HTML；个性化区域和登录态区域也必须使用 Registry 白名单内的 Runtime Fragment 或 Client Enhancement，不能让构建系统枚举任意查询组合。

### 1.4 非目标

MVP 不做：

- 访问时执行 Jet
- 访问时读取 Page Document 或解释 ThemeNode
- 每个请求动态解析 Binding
- 自定义 CMS Schema
- 通用 Query DSL
- 通用 Template DSL
- 用户输入任意 JavaScript、CSS、API endpoint 或 Jet
- Blueprint 修改自动传播到已有页面
- 整站 Release 与整站原子回滚
- 多人实时协作
- 第三方组件市场
- 多语言静态发布（i18n）——后续按 URL 语言前缀方案扩展：`/{lang}/{path}` 各生成独立 Artifact，切换语言等同跳转新 URL 并触发构建。同一 Page Document 按 Project 语言列表循环构建，BuildContext 按 language 注入翻译。Admin 控制面 i18n 复用 go-mvc 的 `sys_i18n` + 内存缓存方案。

## 2. 总体架构

```text
┌───────────────────────────────────────────────────────────────┐
│ Admin                                                         │
│                                                               │
│ CMS Admin                         Visual Builder              │
│ Content / Product / Menu          Page Document / Blueprint   │
└───────────────┬─────────────────────────────┬─────────────────┘
                │                             │
                ▼                             ▼
        BuildContext Resolver        Component Registry
                │                             │
                └──────────────┬──────────────┘
                               ▼
                    Go Publish Compiler
                    ├── Migrate / Normalize
                    ├── Validate
                    ├── Component Lowering
                    ├── Fragment Compiler
                    ├── Style Compiler
                    ├── Asset Collector
                    ├── Diagnostic Passes
                    └── Jet Build-time Render
                               │
                               ▼
                   Immutable Page Artifact
                   HTML / CSS / JS / Manifest
                               │
                    ArtifactStore.put()
                               │
                    PublicationStore.activate()
                               │
                               ▼
                      Static Server / CDN
                               │
                               ▼
                         Visitor Browser
                         ├── HTMX → Runtime Fragment Endpoint
                         └── Optional Client Enhancement
```

控制面与访问面严格分离：

```text
控制面：Database + CMS + Builder + Build Worker + ArtifactStore
访问面：PublicationStore 激活结果 + Static Server/CDN + Runtime Fragment Endpoint
```

普通访客请求不得查询 `pages.active_artifact_id` 后再选择模板。数据库指针用于控制、审计和故障恢复；实际 URL 必须由 PublicationStore 映射到已经激活的静态文件。

### 2.1 端到端发布主线

**Page（手工页面）**：

```text
编辑 Page Document
→ 保存 Draft
→ Go Preview Build
→ Go Publish Build
→ 写入不可变 Page Artifact
→ Stage
→ PublicationStore 原子激活 URL
→ 更新 active_artifact_id
→ Static Server/CDN 直接提供文件
```

**PresentationInstance（自动内容页面）**：

```text
CMS 实体变更
→ 自动派生 DocumentSnapshot
→ Go Preview Build（可选）
→ Go Publish Build
→ 写入不可变 Page Artifact
→ Stage
→ PublicationStore 原子激活 URL
→ Static Server/CDN 直接提供文件
```

两个路径共享 Publish Compiler、ArtifactStore 和 PublicationStore。详细步骤见 `03-pipeline.md`。

### 2.2 普通访客链路

```text
HTTP Request
→ Static Server/CDN
→ Activated index.html + hash assets
→ Browser
→ Optional HTMX Runtime Fragment request
→ Optional Client Enhancement
```

## 3. 技术栈与实现归属

| 层 | 选型 | 职责 |
|---|---|---|
| Builder UI | Alpine.js | 面板、选中态、viewport 等轻量 UI 状态 |
| 编辑器内核 | Plain TypeScript | Page Document、Command、History、NodeIndex |
| 拖拽 | SortableJS | 只上报结构修改意图 |
| Admin 请求 | HTMX | 草稿、预览、构建、发布、回滚请求 |
| 公开动态片段 | HTMX + Go Handler | 按 Registry capability 返回受控 HTML Fragment |
| 客户端增强 | Registry 固定 JS module | 对完整静态 HTML 做局部渐进增强 |
| 后端与构建器 | Go | CMS、BuildContext、Publish Compiler、版本与发布状态机 |
| 构建期模板 | Jet | 在 Go 构建阶段把受限 Fragment 与 BuildContext 渲染为最终 HTML |
| 数据库 | PostgreSQL | CMS 内容、Page 草稿、Artifact 元数据和依赖索引 |
| Artifact | 本地文件系统 / 对象存储 | 不可变构建文件与内容寻址资源 |
| 访问 | Static Server / CDN | 直接提供激活后的 Artifact |

Go Publish Compiler 是发布语义的唯一真相源。Visual Builder 不维护第二套 Production Compiler：

- Builder 使用版本化 Registry Manifest 创建节点和 Inspector。
- iframe Preview 调用 Go Preview Build Endpoint。
- Preview 和 Publish 共用 Migration、Normalize、Validator、Binding、Lowering、Style 与 Jet 语义。
- Preview 可以注入 `data-node-id` 和 mock data；Publish 必须移除全部编辑属性。

## 4. 仓库模块边界

```text
go_wp/
├── apps/
│   ├── cms/
│   │   ├── cmd/
│   │   └── internal/
│   │       ├── content/
│   │       ├── page/
│   │       ├── blueprint/
│   │       ├── contenttemplate/
│   │       ├── presentation/
│   │       ├── build/
│   │       ├── artifact/
│   │       ├── publication/
│   │       └── runtimefragment/
│   └── visual-builder/
├── packages/
│   └── page-editor/
└── fixtures/
    ├── documents/
    ├── build-contexts/
    └── artifacts/
```

- `apps/cms/internal/build` 是 Publish Compiler 唯一实现。
- `apps/cms/internal/artifact` 管理不可变文件，不管理 URL 激活。
- `apps/cms/internal/publication` 管理 URL 激活、回滚和取消发布，不解释 Page Document。
- `apps/cms/internal/contenttemplate` 管理 ContentTemplate 与版本，不参与运行时渲染。
- `apps/cms/internal/presentation` 管理 PresentationInstance 与 DocumentSnapshot。
- `packages/page-editor` 只实现 Document、Command、History、NodeIndex 和 Builder 适配，不实现发布语义。
- `fixtures` 用于 Preview、Publish、Artifact 校验和静态访问的跨模块验收。
- 目录仅在对应实现落地时创建，不预建空模块。

## 5. 冻结边界速查

下表是全系统所有协议边界的「负责 / 禁止」速查。每条边界的详细约束分散在对应主题文档中，本表用于快速定位冲突。

| 边界 | 负责 | 禁止 |
|---|---|---|
| CMS Content | 文章、商品、菜单、媒体等业务事实 | 保存页面 AST |
| Blueprint | 初始化新 Page Document 的版本化输入 | 参与构建/访问或隐式传播到已有 Page |
| Page | 手工页面：单 URL 的独立构建、发布、回滚和取消发布 | 充当 CMS 内容的访问时展示模板；自动创建内容页面 |
| ContentTemplate | 内容实体类型的版本化页面结构定义，参与每次构建派生 DocumentSnapshot | 参与运行时渲染；作为一次性初始化工具 |
| PresentationInstance | 内容实体的自动发布页面实例，从 ContentTemplate 派生 DocumentSnapshot 后经同一流水线生成 Artifact | 手工在 Visual Builder 编辑；URL 脱离 CMS 实体手动设置 |
| DocumentSnapshot | 构建时从 ContentTemplate 派生的已解析完整 AST 快照 | 手工编辑；保存业务内容实例或任意代码 |
| GlobalComponentUpdatePolicy | 定义全局组件新版本发布时对已有页面的更新策略 | 片段级覆盖或绕过 Compiler 的自动升级 |
| Project Assets | CMS Media 的稳定引用与构建期资源解析 | 复制媒体二进制或保存最终 URL |
| Global Component | Project 内不可变版本子树 | 绕过 updatePolicy 自动升级 Page 或引用点局部 override |
| Page Document | 单 URL 的结构、样式、Binding | 保存业务内容实例或任意代码 |
| Editor Kernel | Command、History、NodeIndex | 实现发布 Compiler |
| Component Registry | 组件语义、Inspector manifest、lowering、renderStrategy | 任意脚本/API/查询扩展 |
| BuildContext | 一次构建的固定数据与依赖 revision | 构建中继续查询数据库 |
| Publish Compiler | Document + Context → 静态 Artifact | 访问时运行或输出未解析 Binding |
| ArtifactStore | 不可变文件存取 | 管理公开 URL |
| PublicationStore | URL 原子激活、取消和检查 | 构建或修改 Artifact |
| Page Artifact | 不可变源码快照、输入、入口和内容对象闭包 | 原地修改历史版本 |
| Runtime Fragment | 白名单 Go Handler 返回受控 HTML 片段 | 读取 Page Document、执行 Jet 或接受任意 endpoint |
| Client Enhancement | 对完整静态 HTML 做局部渐进增强 | 承载唯一内容、任意脚本或默认网络访问 |
| GC | 删除无引用且超过保留期的对象 | 直接响应内容删除同步删历史目录 |