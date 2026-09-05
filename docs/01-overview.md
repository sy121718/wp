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
- Category / Tag / Archive 首页（第一页静态构建）
- Search Shell（例如 `/search/`，查询结果本身不静态枚举）
- 404

列表页、归档页、搜索结果和分类页的分页翻页不静态枚举所有页码，改用 HTMX Runtime Fragment 按需加载：构建时只输出第一页静态 HTML，后续页码由 HTMX 发起到同一个 `/_fragments/{type}` 端点，返回 HTML 片段替换列表区域。

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
- 多语言静态发布（i18n）——后续按 URL 语言前缀方案扩展：`/{lang}/{path}` 各生成独立 Artifact，切换语言等同跳转新 URL 并触发构建。同一 Page Document 按 Project 语言列表循环构建，BuildContext 按 language 注入翻译。Admin 控制面 i18n 复用现有 `sys_i18n` + 内存缓存机制。

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

项目采用现有单应用基础框架。模块通过 `contract` 隔离，后续拆分微服务时保留契约并替换 inbound/outbound 适配器，不先引入 `apps/` 多应用目录。

```text
go_wp/
├── cmd/                              # 进程入口
├── config/                           # 配置与组件生命周期编排
├── internal/
│   ├── middleware/                   # 全局中间件
│   ├── routers/                      # 顶层路由与模块装配
│   ├── task/                         # 异步任务注册和处理器
│   └── module/
│       ├── admin/                    # 已实现：后台管理员、角色、权限点、菜单、部门、数据权限（六领域合并大模块）
│       ├── common/                   # 已实现：公共业务入口（当前为验证码：标准库自绘 PNG 图片化，答案绝不下发）
│       ├── dashboard/                # 已实现：仪表盘、可视化工作台 Workbench、媒体库、主题管理等后台页面入口
│       ├── media/                    # 已实现：附件与文件分类（LIKE 通配符转义、软删除过滤）
│       ├── project/                  # 已实现：站点工程、SiteSettings、多主题 Theme
│       ├── page/                     # 已实现：手工 Page 与 Page Document（草稿/构建/发布/回滚/改 URL）
│       ├── block/                    # 已实现：全局块（页眉/页脚/区块）与 stale 传播编排
│       ├── artifact/                 # 已实现：Artifact 元数据与内容对象闭包
│       ├── publication/              # 已实现：URL 占用、激活、回滚与恢复
│       ├── content/                  # 规划 0-A2：固定 CMS 内容与 revision
│       ├── contenttemplate/          # 规划 0-A2：ContentTemplate 与版本
│       ├── presentation/             # 规划 0-A2：PresentationInstance / DocumentSnapshot
│       ├── blueprint/                # 规划 0-B：Blueprint 与版本
│       ├── component/                # 规划 0-B：Global Component、版本、策略与 Registry
│       ├── navigation/               # 规划 0-C：公开站点菜单及其 revision
│       └── runtimefragment/          # 规划 0-D：白名单动态片段
├── pkg/                              # 通用基础组件（facade + provider/driver）
├── public/
│   ├── migrations/                   # 数据迁移
│   ├── storage/                      # 本地 Artifact / Media 存储
│   └── test/                         # 集成测试与 fixtures
```

目录树中「已实现」模块已落地；「规划 0-X」模块尚未创建，不预建空目录。`build` 无独立 module 目录：编译内核在 `internal/builder`，发布内核在 `internal/pipeline`。原 `internal/embed/dist/` 下的 Vue SPA 构建产物已移除（后台全部改为 HTMX + Jet SSR）。

### 4.1 现有模块与 go_wp 核心模块映射

| 模块组 | 状态 / Phase | 负责 | 明确不负责 |
|---|---|---|---|
| `admin`（六领域合并：角色 / 权限点 / 菜单 / 部门 / 数据权限） | 已实现 | 管理控制面大模块：管理员、角色、权限点、菜单、部门、数据权限（同包直调） | CMS 内容、公开站点用户、公开站点导航 |
| `common` | 已实现 | 暂存尚未形成独立领域的业务入口；当前为验证码（标准库自绘 PNG 图片化，答案绝不下发） | 通用基础设施、持续膨胀的共享业务逻辑 |
| `dashboard` | 已实现 | 需要后端逻辑的后台页面入口（仪表盘、可视化工作台 Workbench、媒体库、主题管理） | — |
| `project` | 已实现 | `projects`、站点级设置、构建所需 Project 快照、多主题 Theme（list/activate/delete/settings） | Page AST、Artifact 文件、发布激活 |
| `page` | 已实现 | `pages`、Page Document、草稿版本、Page 用例与改 URL | 编译器实现、文件存储、URL 原子激活 |
| `block` | 已实现 | 全局块（页眉/页脚/区块）与 block→page 的 stale 传播编排 | CMS 内容、公开站点导航 |
| `build` | 非独立模块 | BuildContext 解析、Normalize/Validate/Lowering/Render 管线与构建任务；编译内核在 `internal/builder`，发布内核在 `internal/pipeline` | 持久化 CMS 实体、直接切换线上 URL |
| `artifact` | 已实现 | Artifact 元数据、内容对象闭包、`ArtifactStore` 契约与实现 | URL 占用、发布状态机 |
| `publication` | 已实现 | `page_routes`、激活/回滚/取消发布、event/receipt 与恢复任务（两段式回执、占用前置） | 编译 Document、修改 Artifact 内容 |
| `content` | 规划 0-A2 | 固定 Article/Product/Category/Tag 等 CMS 内容及单调 revision | 页面 AST、访问时模板解释、库存等实时状态 |
| `contenttemplate` | 规划 0-A2 | ContentTemplate 草稿、不可变版本和 Binding 约束 | CMS 内容实例、运行时渲染 |
| `presentation` | 规划 0-A2 | PresentationInstance、DocumentSnapshot 及内容驱动的发布入口 | 手工 Page 编辑、模板版本管理 |
| `blueprint` | 规划 0-B | Blueprint 草稿、不可变版本和 Page 初始化 | 构建期继承、自动传播 |
| `component` | 规划 0-B | Global Component、版本、更新策略、pins 与 Registry manifest | Publish Compiler 的树遍历和产物组装 |
| `media` | 已实现 | 媒体元数据、变体、内容 hash 和稳定 `assetId`（LIKE 通配符转义、软删除过滤） | Page Document、公开 URL 激活 |
| `navigation` | 规划 0-C | 公开站点菜单、位置和 revision | 后台权限菜单；后者始终属于 `menu` |
| `runtimefragment` | 规划 0-D | capability 白名单、受控 HTML Fragment handler | 读取 Page Document、执行 Jet、接受任意 endpoint |

关键命名约束：

- `menu` 固定表示管理后台权限菜单；公开站点的 Header/Footer 导航统一属于 `navigation`。
- `admin` 固定表示管理控制面账号；未来访客账号或客户账号必须另建明确领域模块，不能复用 `admin` 表和会话语义。
- `content` 只聚合共享同一发布/revision 生命周期的固定 CMS 内容。若 Product、Article 后续出现独立事务和高频跨模块参数，应拆为独立模块，而不是让 `content/service` 演变为总控层。
- `build` 内核（`internal/builder` 编译 + `internal/pipeline` 发布）、`artifact`、`publication` 是单向流水线边界：`build → artifact → publication`。后者不得反向导入前者实现；跨模块只使用 `contract` 和不可变 DTO。

每个业务模块沿用统一分层：

```text
internal/module/{module}/
├── contract/                         # 对外业务契约
├── dto/                              # 入站/出站 DTO
├── enums/                            # 模块枚举与消息
├── inbound/http/                     # HTTP 适配器和路由
├── model/                            # 本模块持久化模型
├── outbound/                         # 可选：RPC/HTTP/MQ/存储适配器
└── service/                          # 业务用例实现
```

- `service` 只能直接访问本模块 `model`；跨模块调用只依赖目标模块 `contract`，不得导入其他模块的 `service/model/dto`。
- `internal/builder` 是 Publish Compiler 唯一实现（发布内核在 `internal/pipeline`，非独立 module）。
- `internal/module/artifact` 管理不可变文件，不管理 URL 激活。
- `internal/module/publication` 管理 URL 激活、回滚、取消发布和 Receipt 恢复，不解释 Page Document。
- `internal/module/contenttemplate` 管理 ContentTemplate 与版本，不参与运行时渲染。
- `internal/module/presentation` 管理 PresentationInstance 与 DocumentSnapshot。
- `pkg` 只放业务无关的基础设施 facade；CMS/构建/发布语义不得下沉到 `pkg`。
- Visual Builder 的 Editor Kernel 位于 `web` 前端工程内，只实现 Document、Command、History、NodeIndex 和 Builder 适配，不实现发布语义。
- 新业务模块只在对应阶段落地时创建，不预建空目录。

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