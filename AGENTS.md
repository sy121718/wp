# AGENTS.md

本文件描述 go_wp 仓库的实际开发约定，是 DSH 会话的最高项目级规则。
兼容说明：旧 `CLAUDE.md` 内容已并入本文；子目录规则见 `internal/module/CLAUDE.md`、`pkg/CLAUDE.md`、`public/CLAUDE.md`、`docs/agents/`。

## 语言要求（最高优先级）

- 所有回复、分析、总结、计划、报告一律使用简体中文
- 推理/思考过程也使用简体中文
- 工具输出、代码、上游数据即使是英文，回复仍必须是中文；代码标识符、专有名词、命令保留原文

## 项目概览

go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`。

控制面（CMS + Builder + Build Worker）把可编辑的 Page Document 和 CMS 内容编译为不可变静态 Artifact；访问面（Static Server / CDN + Runtime Fragment Endpoint）只读取已激活的 HTML/CSS/JS。Go + Jet 只在 Preview/Publish 构建阶段运行，访客请求不执行任何模板或数据库查询。

**技术栈**（当前实际状态，2025-09 核对）：

| 层 | 选型 | 职责 |
|---|---|---|
| Web 框架 | Gin | HTTP 路由、中间件链、请求绑定 |
| 后端与构建器 | Go | CMS、BuildContext、Publish Compiler、版本与发布状态机 |
| 构建期模板 | Jet v6（`github.com/CloudyKit/jet/v6`） | 发布阶段把受限 Fragment 与 BuildContext 渲染为最终 HTML；后台页面 SSR |
| Admin 交互 | HTMX（CDN） | 草稿、预览、构建、发布、回滚请求 |
| 认证 | Session + Cookie（gin-contrib/sessions + Cookie 存储） | 替代旧 JWT 方案，HTMX 请求自动携带 Cookie |
| 鉴权 | Casbin（自研 persist.Adapter） | Enforce(user_id, path, method)；业务 API 已挂载 |
| 公开动态片段 | HTMX + Go Handler | 按 Registry capability 返回受控 HTML Fragment（规划态，暂无路由） |
| 富文本编辑器 | TinyMCE（CDN） | 文章内容编辑 |
| 数据库 | PostgreSQL（主库） | CMS 内容、Page 草稿、Artifact 元数据和依赖索引；MySQL 为历史兼容；SQLite/SQL Server 驱动已移除 |
| 会话存储 | Redis（pkg/cache） | 用户会话、封禁标记、在线心跳（**Critical 组件，配置必须启用**） |
| Artifact 存储 | 本地文件系统 / 对象存储 | 不可变构建文件与内容寻址资源 |
| 访问（公开站点） | Static Server / CDN | 直接提供激活后的 Artifact |

> ~~Vue 3 / vue-pure-admin~~ 已废弃并移除。所有后台界面由 Go 渲染 Jet 模板 + HTMX 片段实现。

## 常用命令

```bash
# 后端
go run cmd/main.go
go build -o app cmd/main.go
go test ./...
go test -race ./...          # 并发回归
go vet ./...
```

## 架构约束（核心不变量）

以下不变量贯穿全系统，违反任意一条即为设计缺陷。详细论证见 `docs/01-overview.md` 等文档。

1. **控制面与访问面分离**：访客请求不查询数据库、不执行 Jet、不解释 AST。URL → 文件映射由 PublicationStore 文件系统状态决定，不由数据库指针决定。
2. **两条发布路径共享同一管线**：Page（手工）已实现；PresentationInstance（自动）属规划态，落地后走同一 Publish Compiler → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃，ContentTemplate 每次构建参与**：均属规划不变式（0-B/0-A2），尚未落地；Blueprint 只初始化 Page Document，后续修改不传播。
4. **Binding 不是 Query DSL**：Document 只保存白名单 FieldBinding / CollectionSource / MediaBinding，不能保存 SQL、过滤表达式或任意 endpoint。
5. **确定性构建**：同一 Page Document + BuildContext + Registry + Compiler 产生相同 Artifact 字节（有 determinism/fuzz 测试背书）。
6. **冻结边界不可越权**：每个模块、组件、协议都有明确的「负责 / 禁止」边界，详见 `docs/01-overview.md` §5 冻结边界速查。

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

## 模块现状（2025-09 核对）

### 已实现模块

| 模块 | 职责 | 不负责 |
|---|---|---|
| `admin` | 管理控制面大模块：管理员、角色、权限点、菜单、部门、数据权限（六领域已合并，同包直调） | CMS 内容、公开站点用户 |
| `common` | 公共业务入口（当前为验证码：标准库自绘 PNG 图片化，答案绝不下发） | 通用基础设施 |
| `dashboard` | 需要后端逻辑的后台页面入口（仪表盘、可视化工作台 Workbench、媒体库、主题管理） | — |
| `media` | 附件与文件分类（LIKE 通配符转义、软删除过滤） | — |
| `project` | 站点工程、SiteSettings、多主题 Theme（list/activate/delete/settings） | — |
| `page` | 手工 Page 与 Page Document：草稿/构建/发布/回滚/改 URL | — |
| `block` | 全局块（页眉/页脚/区块）与 stale 传播编排 | — |
| `artifact` | Artifact 元数据与内容对象闭包（不可变写入、同版本重构建原地替换） | — |
| `publication` | URL 占用、激活（两段式回执 pending→committed/rolled_back）、回滚 | — |

> `build` 无独立模块目录：编译内核在 `internal/builder`，发布内核在 `internal/pipeline`。
> `permission/role/menu/dept/datarule` 已并入 `admin` 大模块，不再独立。

### 规划模块（未落地，不预建空目录）

| Phase | 模块 | 职责 |
|---|---|---|
| 0-A2 | `content` / `contenttemplate` / `presentation` | 固定 CMS 内容 + 版本化结构模板 + 自动发布实例 |
| 0-B | `blueprint` / `component` | Page 初始化工具 + 全局组件版本与策略 |
| 0-C | `navigation` | 公开站点菜单（与后台 `menu` 严格隔离） |
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
- `config.InitComponents()` → 初始化所有 `pkg` 组件（Critical 优先：database → cache → auth → casbin → …）
- `config.CloseComponents()` → 逆序关闭

组件不自行决定进程退出，组件只返回 `error`。配置校验在各自 `pkg.Init()` 内部完成。
**auth 组件 fail-fast**：`redis.enabled=false` 时启动失败（`RequireSessionStorage`），release 模式弱 `session_secret` 拒绝启动。

### 模板渲染（Jet v6）

- 所有后台页面由 Go 服务端使用 Jet v6 渲染，实现 `gin.HTMLRender` 接口包装为 Gin 标准 Render
- 模板位置：`internal/templates/admin/`（后台页面）、`internal/templates/components/`（构建期组件，go:embed）
- 开发模式 `jet.DevelopmentMode(true)` 禁用模板缓存；**生产模式必须关闭**（由部署配置驱动）
- Jet 模板内 CSRF token 注入必须用 chain 索引写法 `{{ .["csrf_token"] }}`（`{{.csrf_token}}` 缺 key 会运行时报错）

### 交互方式（HTMX）

所有前台交互通过 HTMX 属性驱动，不写自定义 JS（后台工作台 workbench.js 例外，属构建器前端）。
CSRF：HTMX 请求经 `<body hx-headers='{"X-CSRF-Token":"{{ .["csrf_token"] }}"}'>` 继承；原生表单必须显式加 `csrf_token` 隐藏域；fetch 请求必须带 `X-CSRF-Token` 头（workbench.js/media-lib.js 已封装）。

### 认证与鉴权（已实现，三层链）

- `Session + Cookie` → 认证（gin-contrib/sessions + Cookie 存储）：cookie 只存最小认证信息（user_id/username/session_id/issued_at），用户资料走 Redis
- `CSRF` → 所有 POST 写操作强制 token 校验（登录成功返回 token；`X-CSRF-Token` 头或 `csrf_token` 表单域）
- `Casbin` → 鉴权（Enforce(user_id, path, method)；业务权限点见迁移 030/031 seed，超管 is_admin=1 全量策略）

挂载矩阵：

| 路由组 | SessionAuth | CSRF | Casbin |
|---|---|---|---|
| `/api/captcha`、`/api/admin/login` | 豁免 | 豁免 | 豁免 |
| `/api/admin/*` 六领域 | ✅ | ✅ | ✅ |
| `/api/{media,project,block,page,artifact,publication}/*` | ✅ | ✅ | ✅ |
| `/admin/*` 页面、`/`、`/workbench*` | ✅ | ✅ | —（页面路由） |

Cookie 属性：`HttpOnly`、`Secure`（release 自动启用）、`SameSite=Lax`。

### 登录安全

- 密码 bcrypt；验证码图片化（`/api/captcha` 只返回 `captcha_id` + `captcha_image`，答案绝不下发）
- 登录失败 ≥5 次只写 `locked_until_time = now+30min`（自动过期），**绝不修改 Status**
- 失败计数必须用原子 SQL（`count = count + 1`），禁止读-改-写回

### 路由

- 只用 `GET` 和 `POST`；禁止 RESTful 路径参数，全部用 Query 参数
- 主路由聚合在 `internal/routers/routes.go`；模块路由在 `internal/module/<模块>/inbound/http/`
- 健康检查：`GET /livez`、`GET /readyz`（组件级就绪）
- 静态面：`/site`（激活产物，`http.Dir` 只读）、`/storage`（媒体上传）、`/static`（后台静态资源）
- CORS：白名单来自 `server.cors_allowed_origins`；release 无白名单拒绝跨域；TrustedProxies release 模式为 nil（不信任 XFF）

### 响应与错误处理

| 请求类型 | 响应格式 |
|---|---|
| HTMX 请求（`HX-Request: true` / `Accept: text/html`） | Jet 渲染的 HTML 片段 |
| JSON API | `Response{Code,Message,Data}`（`pkg/response`） |

- 未登录页面请求 302 到 `/admin/login`；API 请求返回 401 JSON
- 业务模块统一通过模块 `enums` 提供响应消息；`pkg` 和系统包直接用中文提示或原始 `err`
- dashboard 页面 handler 禁止 `c.String(500, err.Error())` 直出内部错误，必须走 `pkg/response` + enums

### 数据库

- 统一走 dbx MCP，默认连接 `WSL_PostgreSQL@16.14`（127.0.0.1:5432），默认库 `base`；调用时显式传 `connection_name`
- 查询一律参数化；context 必须传播（`WithContext`）
- 迁移：`public/migrations/` 版本化 SQL（幂等），`register.go` 注册；seed 用 ConditionSQL（030 权限点 / 031 超管策略）
- datarule 插件字段引用按方言（PG 双引号 / MySQL 反引号）；部门范围整段精确匹配

### model 层定位（重要，评审与开发共同遵守）

`model` 是**表访问单元（Repository）**，不是 DDD Domain Model：

- ✅ 允许：单表 CRUD、单表聚合、单表内的原子组合操作（如 `ReplaceByRuleID`）
- ❌ 禁止：跨 model 调用、业务规则/决策（谁能删、状态机）、**跨表事务**（两个 model 各自开事务无法共享）
- 跨表事务必须在 service 层编排：model 方法接受外部 `*gorm.DB`/`*gorm.Session`（或 model 暴露 `Transaction()` 透传），由 service 决定事务边界与回滚
- `contract/` 只放模块对外接口；`service` 依赖其他模块能力时直接引用对方 `contract`

## 测试

- 默认跑现有测试，不新增额外测试框架
- 接口优先维护 feature 链路测试（`public/test/`，真实 PostgreSQL 环境），复杂逻辑补 unit
- 测试基建已迁移到本地 PostgreSQL（sqlite 驱动已移除）；PG/Redis 不可用时相关用例 `t.Skip`
- 组件测试在组件包内（`internal/builder/components/*`），含确定性构建与 fuzz 测试
- 并发敏感代码跑 `go test -race`

## Git 与工具约定

- 每次 commit 用中文注明改动文件路径和修改内容简述
- 文档更新与代码提交分开；修改规则文件（本文件及子目录 CLAUDE.md）前先重新读取，桌面端可能并发改写
- GitHub 操作（仓库/Issue/PR/Release）优先用 `gh` CLI；Go 项目发版优先 GoReleaser（`goreleaser`）
- 语言运行时版本由 vfox 管理（`~/.vfox`），禁止 Homebrew/apt/系统包安装运行时；Node.js 依赖优先 pnpm，Python 用 uv

## 文档导航

- `docs/01-overview.md` — 概览、边界、冻结边界速查（注意：模块表以本文「模块现状」为准）
- `docs/03-pipeline.md` — 发布管线（§4.3/§5/§6.5/§9 已实现；§4.4/§7/§8 规划）
- `docs/05-implementation-plan.md` — 阶段计划（阶段 0-3 已完成；4-7 待办）
- `docs/02-*` — 组件规格；`docs/03-A-workbench.md` — 工作台
- `docs/agents/` — Issue 追踪、Triage 标签、领域术语
- `internal/module/CLAUDE.md` — 模块开发规范；`pkg/CLAUDE.md` — pkg 组件规范
