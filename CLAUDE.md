# CLAUDE.md

本文件描述当前仓库的实际开发约定。

## 项目概览

go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`。

控制面（CMS + Builder + Build Worker）把可编辑的 Page Document 和 CMS 内容编译为不可变静态 Artifact；访问面（Static Server / CDN + Runtime Fragment Endpoint）只读取已激活的 HTML/CSS/JS。Go + Jet 只在 Preview/Publish 构建阶段运行，访客请求不执行任何模板或数据库查询。

**技术栈**：

| 层 | 选型 | 职责 |
|---|---|---|
| Web 框架 | Gin | HTTP 路由、中间件链、请求绑定 |
| 后端与构建器 | Go | CMS、BuildContext、Publish Compiler、版本与发布状态机 |
| 构建期模板 | Jet v6（`github.com/CloudyKit/jet/v6`） | 发布阶段把受限 Fragment 与 BuildContext 渲染为最终 HTML；后台页面 SSR |
| Admin 交互 | HTMX（CDN） | 草稿、预览、构建、发布、回滚请求 |
| 认证 | Session + Cookie（gin-contrib/sessions） | 替代旧 JWT 方案，HTMX 请求自动携带 Cookie |
| 鉴权 | Casbin | Enforce(user_id, path, method) |
| 公开动态片段 | HTMX + Go Handler | 按 Registry capability 返回受控 HTML Fragment |
| 富文本编辑器 | TinyMCE（CDN） | 文章内容编辑 |
| 数据库 | PostgreSQL（主库） | CMS 内容、Page 草稿、Artifact 元数据和依赖索引；MySQL 为历史兼容、SQLite 为轻量部署/测试、SQL Server 为预留可选（暂无部署场景） |
| Artifact 存储 | 本地文件系统 / 对象存储 | 不可变构建文件与内容寻址资源 |
| 访问（公开站点） | Static Server / CDN | 直接提供激活后的 Artifact |

> ~~Vue 3 / vue-pure-admin~~ 已废弃。所有后台界面由 Go 渲染 Jet 模板 + HTMX 片段实现。

## 常用命令

```bash
# 后端
go run cmd/main.go
go build -o app cmd/main.go
go test ./...
```

## 架构约束

以下不变量贯穿全系统，违反任意一条即为设计缺陷。详细论证见 `docs/` 目录（[01-overview.md](./docs/01-overview.md) 等 4 份文档）。

### 核心不变量

1. **控制面与访问面分离**：访客请求不查询数据库、不执行 Jet、不解释 AST。URL → 文件映射由 PublicationStore 文件系统状态决定，不由数据库指针决定。
2. **两条发布路径共享同一管线**：Page（手工）和 PresentationInstance（自动）都经过同一个 Publish Compiler → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃，ContentTemplate 每次构建参与**：Blueprint 只初始化 Page Document，后续修改不传播；ContentTemplate 版本变化触发所有关联 PresentationInstance 重建。
4. **Binding 不是 Query DSL**：Document 只保存白名单 FieldBinding / CollectionSource / MediaBinding，不能保存 SQL、过滤表达式或任意 endpoint。
5. **确定性构建**：同一 Page Document + BuildContext + Registry + Compiler 产生相同 Artifact 字节。
6. **冻结边界不可越权**：每个模块、组件、协议都有明确的「负责 / 禁止」边界，详见 [01 §5 冻结边界速查](./docs/01-overview.md)。

### 控制面与访问面

```text
控制面：Database + CMS + Builder + Build Worker + ArtifactStore
访问面：PublicationStore 激活结果 + Static Server/CDN + Runtime Fragment Endpoint
```

- 普通访客请求不得查询 `pages.active_artifact_id` 后再选择模板
- 数据库指针用于控制、审计和故障恢复；实际 URL 必须由 PublicationStore 映射到已激活的静态文件
- 库存、购物车、登录状态等实时能力优先通过 HTMX Runtime Fragment 提供；只有纯客户端状态才使用 Client Enhancement

### 关键协议辨析

```text
CMS 内容实例      ≠ DocumentSnapshot
Page              ≠ CMS 展示模板
PresentationInstance ≠ Page（前者自动，后者手工）
Blueprint         = Page Document 初始化工具（用完即弃）
ContentTemplate   = PresentationInstance DocumentSnapshot 的版本化结构来源（仅参与构建期）
Blueprint         ≠ 构建期或运行时模板
Page Document     ≠ CMS Content
Artifact          ≠ 可编辑源码
```

## 模块规划

### 已有模块（管理控制面基础能力）

| 模块 | 职责 | 不负责 |
|---|---|---|
| `admin` | 后台管理员、登录与会话 | CMS 内容、公开站点用户 |
| `role` | 后台角色 | — |
| `permission` | 权限资源管理 | — |
| `menu` | **后台权限菜单** | 公开站点导航（属于 `navigation`） |
| `dept` | 部门组织管理 | — |
| `datarule` | 数据权限规则 | — |
| `common` | 公共业务入口（当前仅验证码） | 通用基础设施 |

### 目标模块（按 Phase 落地，不预建空目录）

| Phase | 模块 | 职责 |
|---|---|---|
| 0-A1 | `project` | 站点工程与 SiteSettings |
| 0-A1 | `page` | 手工 Page 与 Page Document |
| 0-A1 | `build` | BuildContext Resolver 与 Publish Compiler |
| 0-A1 | `artifact` | Artifact 元数据与 ArtifactStore |
| 0-A1 | `publication` | URL 占用、激活、回滚与恢复 |
| 0-A2 | `content` | 固定 CMS 内容（Article/Product/Category/Tag）与 revision |
| 0-A2 | `contenttemplate` | ContentTemplate 与版本 |
| 0-A2 | `presentation` | PresentationInstance / DocumentSnapshot |
| 0-B | `blueprint` | Blueprint 与版本 |
| 0-B | `component` | Global Component、版本、策略与 Registry |
| 0-B/0-C | `media` | 媒体元数据、变体与内容引用 |
| 0-C | `navigation` | 公开站点菜单及其 revision |
| 0-D | `runtimefragment` | 白名单动态片段 |

### 命名约束

- `menu` = 管理后台权限菜单；`navigation` = 公开站点导航，两者不可混用
- `admin` = 管理控制面账号；未来访客账号必须另建领域模块
- `build → artifact → publication` 是单向流水线，后者不得反向导入前者实现
- 跨模块只使用 `contract` 和不可变 DTO，不得导入其他模块的 `service/model/dto`

## 核心约定

### 启动与关闭

统一入口：

- `config.Init()` → 读配置
- `config.InitComponents()` → 初始化所有 `pkg` 组件
- `config.CloseComponents()` → 逆序关闭

组件不自行决定进程退出，组件只返回 `error`。配置校验在各自 `pkg.Init()` 内部完成。

### 模板渲染（Jet v6）

所有后台页面由 Go 服务端使用 Jet v6 模板引擎渲染。

**集成方式**：实现 `gin.HTMLRender` 接口，将 Jet `*jet.Set` 包装为 Gin 的标准 Render。

```go
// 伪代码示意
import "github.com/CloudyKit/jet/v6"

func NewJetRender(viewDir string, isDev bool) gin.HTMLRender {
    loader := jet.NewOSFileSystemLoader(viewDir)
    set := jet.NewSet(loader, jet.DevelopmentMode(isDev))
    return &jetRender{set: set}
}

type jetRender struct {
    set *jet.Set
}

func (r *jetRender) Instance(name string, data any) gin.Render {
    // 返回 Render 接口，内部调用 set.GetTemplate() + Execute()
}
```

**模板位置**：`internal/templates/admin/`（后台管理页面）

**开发模式**：`jet.DevelopmentMode(true)` 禁用模板缓存，修改 `.html` 文件即时生效。

**模板语法要点**（Jet v6）：

| 特性 | 语法 |
|------|------|
| 输出变量 | `{{ varName }}` |
| 原始 HTML | `{{ unsafe(varName) }}` |
| 条件判断 | `{{ if condition }}...{{ end }}` |
| 循环 | `{{ for _, item := range items }}...{{ end }}` |
| 继承布局 | `{{ extends "./layout.html" }}` |
| 定义块 | `{{ block body() }}...{{ end }}` |
| 引入片段 | `{{ include "./partials/table.html" }}` |
| 注释 | `{* 注释内容 *}` |

### 交互方式（HTMX）

所有前台交互通过 HTMX 属性驱动，不写自定义 JS。

**CDN 引入**（在布局模板的 `<head>` 中）：

```html
<script src="https://cdn.jsdelivr.net/npm/htmx.org@2.0.4/dist/htmx.min.js"></script>
```

**常用场景映射**：

| 场景 | HTMX 写法 |
|------|----------|
| 表单提交 | `<form hx-post="/admin/login" hx-target="#result">` |
| 分页/列表刷新 | `<a hx-get="/admin/users?page=2" hx-target="#table-body">` |
| 搜索筛选 | `<input hx-get="/admin/users/search" hx-trigger="keyup delay:500ms" hx-target="#table-body">` |
| 弹窗编辑 | `<button hx-get="/admin/users/edit?id=1" hx-target="#modal-body">` |
| 删除确认 | `<button hx-post="/admin/users/delete?id=1" hx-confirm="确定删除？">` |
| 权限树展开 | `<span hx-get="/admin/menus/children?id=1" hx-target="#menu-1-children">` |
| 替换自身 | `hx-swap="outerHTML"` |

**服务端响应**：返回 Jet 渲染的 HTML 片段（而不是完整页面），由 `hx-target` 指定的 DOM 元素接收并替换。

### 富文本编辑器（TinyMCE）

文章内容编辑使用 TinyMCE，通过 CDN 按需加载。

**CDN 引入**（仅在需要编辑器的页面）：

```html
<script src="https://cdn.tiny.cloud/1/YOUR_API_KEY/tinymce/7/tinymce.min.js" referrerpolicy="origin"></script>
```

**基本初始化**：

```javascript
tinymce.init({
    selector: '#content-editor',
    plugins: ['advlist', 'autolink', 'link', 'image', 'lists', 'code', 'fullscreen', 'media', 'table', 'help'],
    toolbar: 'undo redo | styles | bold italic | alignleft aligncenter alignright | bullist numlist | link image | code | fullscreen',
    height: 400
});
```

**表单提交**：TinyMCE 自动同步内容到原始 `<textarea>`，HTMX 提交时带上 HTML 内容即可。

### 设计系统

所有 UI 设计需求**必须遵守** [DESIGN.md](./DESIGN.md)，不得自行发挥。

该文稿定义了：Stripe 风格设计语言、配色/字体/间距/圆角 Token、组件规范、亮暗双主题、Do's & Don'ts。

**核心约定**：

- 配色、字体、间距、圆角等 Token 以 `DESIGN.md` 中定义的为准
- 亮暗双主题通过 CSS 变量 + `data-theme` 属性实现，切换走 `localStorage`（纯客户端，不走后端）
- 页面加载时在 `<head>` 最早位置执行主题初始化脚本，防止 FOUC（闪烁）
- `DESIGN.md` 的 Do's & Don'ts 章节具有约束力，违反即为设计缺陷

### 认证与鉴权

两层分工：

- `Session + Cookie` → 认证（gin-contrib/sessions + Cookie 存储，HTMX 请求自动携带 Cookie）
- `Casbin` → 鉴权（Enforce(user_id, path, method)）

**CSRF 防护**：所有 POST 写操作强制 CSRF Token 校验。Token 通过 Jet 模板注入到表单隐藏域，中间件拦截校验。

**Cookie 属性**：`HttpOnly`、`Secure`（生产环境）、`SameSite=Lax`。

菜单即权限的可视化：`sys_menus` type=2,3 必含 permission_code，给角色/用户分配后写入 `sys_casbin_rule`。

已实现中间件：CasbinMiddleware、安全响应头、BodyLimit、RateLimit、Recovery。
待实现：SessionAuthMiddleware（替代旧 JWTAuthMiddleware）、CSRFMiddleware。


### 路由

- 只用 `GET` 和 `POST`
- 禁止 RESTful 路径参数，全部用 Query 参数
- 主路由聚合在 `internal/routers/routes.go`
- 模块路由在 `internal/module/<模块>/inbound/http/`
- 健康检查：`GET /livez`、`GET /readyz`

### 响应与错误处理

**两种响应模式**（按请求类型区分）：

| 请求类型 | 响应格式 | 场景 |
|---------|---------|------|
| HTMX 请求 | Jet 渲染的 HTML 片段 | 列表刷新、表单提交后局部更新、弹窗内容、分页 |
| JSON API | `Response` 结构体 | 纯数据接口（如验证码、配置拉取） |

JSON 响应格式：

```go
type Response struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

- HTMX 请求通过 `Accept: text/html` 或 `HX-Request: true` 请求头识别，返回 HTML 片段
- JSON API 请求返回 `Response` 结构体
- `pkg` 和系统包直接用中文错误提示或原始 `err`，不用 i18n
- 业务模块统一通过模块 `enums` 提供响应消息和错误消息
- 未接好 `i18n` 时，模块 `enums` 的值可直接写中文常量；接好后再切到 i18n key 或 i18n 取值
- 业务模块响应调用 `response.Success/ErrorWithMessage` 时，消息参数统一取自模块 `enums`
- 底层系统错误优先返回原始 `err`，不过度包装

### 测试

- 默认跑现有测试，不新增额外测试框架
- 接口优先维护 feature 链路测试，复杂逻辑再补 unit

### 模块开发

模块内部按 `contract / inbound / outbound / service / model / dto` 分层。

`contract/` 只放本模块对外暴露的接口，不需要定义其他模块的依赖接口。`service` 依赖其他模块能力时，直接引用对方 `contract/`。

**每个模块的 `router.go` 自行装配**：获取 `db`、创建本模块 `model` 和 `service`、注册路由。顶层 `routes.go` 只负责获取通用依赖并按依赖顺序调用各模块 Setup 函数。

详见：

- [internal/module/CLAUDE.md](./internal/module/CLAUDE.md)

`outbound` 只负责外部调用实现（RPC / HTTP / MQ / SDK / cache），不承担业务决策。非必需目录，直接依赖对方 contract 即可满足需求时不加 outbound。

`pkg` 组件方向详见：

- [pkg/CLAUDE.md](./pkg/CLAUDE.md)

### 数据库查询（dbx MCP）

本项目查询数据库时统一走 dbx MCP，**默认连接本地**：

- **PostgreSQL 默认连接**：`WSL_PostgreSQL@16.14`（127.0.0.1:5432），默认库 `base`
- **MySQL 历史连接**：`WSL_MySql@9.7.1`（127.0.0.1:3306），仅兼容遗留查询
- 用户未显式指定连接时，一律使用 PostgreSQL 默认连接
- 查询本项目的业务表默认走 `base` 库
- 库名相同的情况下，如查不到表时需在 SQL 中指定 `SET search_path TO public;`
调用 dbx 工具时显式传 `connection_name`（`WSL_PostgreSQL@16.14`），避免连错。

### git 提交

每次 commit 用中文注明改动文件路径和修改内容简述。

## 目录职责

| 目录 | 职责 |
|------|------|
| `cmd/` | 启动入口 |
| `config/` | 配置读取、组件注册与关闭编排 |
| `internal/templates/` | Jet 模板文件（后台管理页面） |
| `internal/middleware/` | 全局中间件 |
| `internal/routers/` | 主路由聚合 + Jet 模板渲染器注册 |
| `internal/module/` | 业务模块 |
| `internal/task/` | 任务注册与调度 |
| `pkg/` | 可复用基础组件（facade + provider/driver） |
| `public/` | 日志、存储、文档、测试资源 |


## Agent skills

### Issue tracker

Issues and specs for this repo live as GitHub Issues (uses the `gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles map to the default labels (`needs-triage` / `needs-info` / `ready-for-agent` / `ready-for-human` / `wontfix`); `/wayfinder` additionally uses `wayfinder:map` and the `wayfinder:<type>` ticket labels. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout. See `docs/agents/domain.md`.

