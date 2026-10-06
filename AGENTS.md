# AGENTS.md

go_wp 仓库的开发约定，DSH 会话的最高项目级规则。

- **本文件只放判据、红线与指针**；论证、实测数据、踩坑实例在 [`docs/rules/`](docs/rules/README.md)，按需读
- 子目录规则：`internal/module/`、`internal/templates/`、`pkg/`、`public/`、`public/test/` 各自的 `CLAUDE.md`；
  批次执行者读 `docs/agents/parallel-batch-rules.md`
- 项目规划 / 规格 / 审计 / 模块清单在 `docs/`（那是项目内容，不是规则）

**强制内容**：本系统开发阶段，不需要兼容任何老的代码，有问题直接重构。

## 语言要求（最高优先级）

- 所有回复、分析、总结、计划、报告一律使用简体中文；推理/思考过程也是
- 工具输出、代码、上游数据即使是英文，回复仍必须是中文；代码标识符、专有名词、命令保留原文

## 项目概览

go_wp = `CMS + Visual Website Builder + Static Publishing Engine`。

控制面（CMS + Builder + Build Worker）把 Page Document 与 CMS 内容编译为不可变静态 Artifact；
访问面（Static Server / CDN + Runtime Fragment Endpoint）只读已激活的 HTML/CSS/JS。
**Go + Jet 只在 Preview/Publish 构建阶段运行，访客请求不执行任何模板或数据库查询。**

选型：Gin + Go ｜ Jet v6（构建期组件 + 后台页面 SSR）｜ HTMX（**本地 vendor**）｜
Session + Cookie（gin-contrib/sessions）+ Casbin（自研 persist.Adapter）｜ PostgreSQL（主库）+ Redis
（`pkg/cache`，**Critical，配置必须启用**）｜ Artifact 本地文件系统（`Provider: "local"`，对象存储是预留
扩展点，**当前只有 local 实现**）｜ Trix 2.x（本地 vendor）+ 服务端白名单清洗。

完整边界见 [`docs/01-overview.md`](docs/01-overview.md)。Vue 3 / vue-pure-admin 已废弃移除 ——
后台界面全是 Go 渲染 Jet 模板 + HTMX 片段。

## 常用命令

```bash
go run cmd/main.go
go build -o app ./cmd        # 生产构建用「包路径」形式，且在 git 工作区内执行（见「组件更新与重建」）
go test ./...
go test -race ./...          # 并发回归
go vet ./...
```

### 组件更新与重建

组件（Go 实现 + `internal/templates/components/*.jet`）编译进二进制，**部署新组件后已发布的产物仍是旧组件
渲染的字节**。系统**不自动重建**：启动时比对 `builder.RegistryVersion()` 与 `page_artifacts.registry_version`
把差异页面标记 stale（只标记），运维经 `page.ListStalePages`（列出）+ `page.RebuildStale`（按 id 重建）处理，
或由后续编辑/发布自然覆盖。

- 构建**必须在 git 工作区内**：无 VCS 信息时只剩组件清单指纹，**发现不了只有 Go 代码变了的改动**。
  禁止用 `bi.Main.Path` 之类构建期变量兜底（同一 commit 会因构建方式不同算出不同版本）。
- 产物文件丢失不属于重建：`POST /api/page/artifact/rebuild`（看返回的 `hashMatched`）；
  `GET /api/page/publication/audit` 巡检 active 目录的悬空链接。

## 架构约束（核心不变量）

违反任意一条即为设计缺陷。论证见 [`docs/01-overview.md`](docs/01-overview.md)。
代码注释里以「**AGENTS.md 不变量 N**」引用本节第 N 条。

1. **控制面与访问面分离**：访客请求不查询数据库、不执行 Jet、不解释 AST。URL → 文件映射由
   PublicationStore 文件系统状态决定，不由数据库指针决定。
   · **唯一例外：访问统计打点**（BIZ-8）。`POST /analytics/collect` 是访客浏览器发起的**唯一写库路径**，
   边界写死三处：接口只有「写一条记录」（无查询/删除）、**不参与任何页面渲染**（响应恒 204 空体）、
   失败一律静默。落库全是匿名派生值（IP / 会话 / 访客标识带盐哈希，UA 只存粗粒度分类）。
   · **第二条例外：访问面守卫中间件只读访客会话**（PIPE-6 AccessGuard）。`/site` 的守卫中间件为判定
   「登录可见」需读 Redis 里的访客会话（`gowp_user_session`）：**只读一个 key、不查 PostgreSQL、不写库、
   不续期**，判定失败一律 fail closed（当未登录处理）。它与本条不冲突 —— URL → 文件映射仍由
   PublicationStore 文件系统状态决定，中间件只在映射命中之后决定「这份字节要不要给这个访客」；
   「密码保护」那一半则完全不碰会话（bcrypt 哈希烘在产物里、解锁走签名 cookie），零 PG 零 Redis。
2. **两条发布路径共享同一管线**：Page（手工）与 PresentationInstance（自动）走同一 Publish Compiler →
   ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃，ContentTemplate 每次构建参与**（0-B/0-A2）：Blueprint 只初始化 Page Document，
   后续修改不传播。
4. **Binding 不是 Query DSL**：Document 只保存白名单 FieldBinding / CollectionSource / MediaBinding，
   不能保存 SQL、过滤表达式或任意 endpoint。
5. **确定性构建**：同一 Page Document + BuildContext + Registry + Compiler 产生相同 Artifact 字节。
6. **冻结边界不可越权**：模块、组件、协议各有明确的「负责 / 禁止」边界，见 `docs/01-overview.md` §5。
7. **构建期数据源的依赖方向**（issue #35）：共享形状放 `internal/builder/source`（零依赖）；业务模块在
   **自己的契约包**里声明**受限数据源接口**（只有读集合 / 元数据 / 可筛值，写方法不进接口）；
   `builder/core` 直接持有这些契约。**契约包不得反向 import `builder/core`**（反向即成环；也不得经
   `templates` 等中间包间接引入 —— 已由 `scripts/check-contract-deps.sh` 按 `go list -deps` 传递闭包判定，
   豁免须在 `scripts/contract-deps-allow.txt` 写明理由，且「曾登记、如今不再命中」同样判失败）。
   接入六步见 [`docs/04-B-dynamic-development-guide.md`](docs/04-B-dynamic-development-guide.md) §1.4。

### 控制面与访问面

```text
控制面：Database + CMS + Builder + Build Worker + ArtifactStore
访问面：PublicationStore 激活结果 + Static Server/CDN + Runtime Fragment Endpoint
```

- 普通访客请求不得查询 `pages.active_artifact_id` 后再选择模板
- 数据库指针用于控制、审计与故障恢复；实际 URL 必须由 PublicationStore 映射到已激活的静态文件
- 库存、购物车、登录状态等实时能力优先通过 HTMX Runtime Fragment 提供；只有纯客户端状态才用
  Client Enhancement

### 关键协议辨析

```text
CMS 内容实例 ≠ DocumentSnapshot        Page ≠ CMS 展示模板
PresentationInstance ≠ Page（前者自动，后者手工）
Blueprint = Page Document 初始化工具（用完即弃）
ContentTemplate = DocumentSnapshot 的版本化结构来源（仅参与构建期）
Blueprint ≠ 构建期或运行时模板          Page Document ≠ CMS Content
Artifact ≠ 可编辑源码
```

## 模块现状

> **模块清单的权威在 [`docs/13-module-inventory.md`](docs/13-module-inventory.md)** —— 各模块完整职责、
> 不变量与落地细节都在那里。本文件**不重复模块表**（同一份清单留两份真源必然漂移）。
> 新增 / 更名模块：先改那份清单，代码与清单同一批提交。

- `build` 模块只承载**构建任务队列**（调度与可见性）；编译内核在 `internal/builder`，发布内核在
  `internal/pipeline` —— **编译逻辑不在 build 模块**。
- `permission/role/menu/dept/datarule` 已并入 `admin` 大模块，不再独立。

### 命名约束

- `menu` = 管理后台权限菜单；`navigation` = 公开站点导航。两者不可混用。
- `admin` = 管理控制面账号；`user` = 访问面访客账号。**两个独立领域**：各自的表、cookie、会话命名空间与
  鉴权链，**不允许互相复用**（共用会让访客 cookie 顶掉后台登录态、两套 id 空间相互污染）。
- `build → artifact → publication` 是单向流水线，后者不得反向导入前者实现。
- 跨模块只使用 `contract` 和不可变 DTO；不得导入其他模块的 `service/model`。

## 核心约定

### 启动与关闭

`config.Init()` → `config.InitComponents()`（Critical 优先：database → cache → auth → casbin → …）→
`config.CloseComponents()`（逆序）。

组件不自行决定进程退出，只返回 `error`；配置校验在各自 `pkg.Init()` 内部完成。
**auth fail-fast**：`redis.enabled=false` 时启动失败，release 模式弱 `session_secret` 拒绝启动。

### 本地开发与迁移

- 开发依赖使用本机 PostgreSQL / Redis 服务，`Makefile` 不负责通过 Docker 启停依赖；`make migrate` 只用
  `pg_isready` 检查本机 PostgreSQL，然后执行 `go run ./cmd -migrate-only`。
- 迁移由 `database.run_migrations` 控制：`true` 时服务启动（包括 air 热重载后的每次重启）自动执行结构迁移与 seed；
  `false` 时启动跳过迁移，必须使用管理连接手动执行 `make migrate` 或 `go run ./cmd -migrate-only`。
- 业务连接使用非超级角色时必须保持 `run_migrations: false`，迁移命令通过环境变量覆盖管理连接，避免把 DDL 权限交给运行服务。
- `make dev` / `scripts/dev.sh` 不隐式修改数据库；是否让 air 自动迁移只由 `database.run_migrations` 配置决定。

### 模板渲染（Jet v6）

- 后台页面由 Go 服务端 Jet v6 渲染，实现 `gin.HTMLRender` 包装为 Gin 标准 Render；模板位置
  `internal/templates/admin/`（后台页面）、`internal/templates/components/`（构建期组件，go:embed）
- 开发模式 `jet.DevelopmentMode(true)` 禁用缓存；**生产模式必须关闭**（由部署配置驱动）
- **CSRF 只有一条取值链**：数据键 `csrf_token`，经 chain 索引 `{{ .["csrf_token"] }}` 取出；
  `{{csrf := .["csrf_token"]}}` 再复用是事实主流写法，两者等价。`{{csrf}}` 是**模板内 let 变量，不是全局
  函数** —— 未声明就裸用会报 `identifier "csrf" not available …`，声明必须在使用之前。
- **禁止 `{{.csrf_token}}`（点号无索引）**：缺 key 会运行时报错、整个响应失败（渲染器先渲到 buffer，
  失败走 `http.Error(500, …)` 并丢弃半截内容；htmx 片段因 5xx 不 swap 而**毫无反应**）。
  判断存在性用 chain 写法：`{{if .["DevLogin"]}}`。
- 注入点：后台页面 `shell.Prepare`、访客页面 `user.Handle.render`；**fragment 模板例外** ——
  `fragments/*.jet` 的 data 是 struct，只能写 `{{ .CSRFToken }}`。细节见 `internal/templates/CLAUDE.md`。

### 交互方式（HTMX）

前台交互**优先**用 HTMX 属性驱动；JS 只做 HTMX 覆盖不到的控件层，收敛在
`internal/templates/static/js/ui/` 与 `rich-editor/`（媒体库选择器 `media-lib.js`，构建器前端 `workbench/`）。
**不新增这几处之外的散落业务 JS**。存量散落清单（全量登记，括号内是性质与收敛方向）：
`admin.js`（后台业务逻辑：菜单候选过滤等，控件已迁基座，剩余随页面迁移逐步清零）、
`media-admin.js`（媒体库页面左树右库业务逻辑，依赖 `media-lib.js`）、
`product-create-form.js`（商品新建表单增强，须自挂 `htmx:afterSwap`，见 `internal/templates/CLAUDE.md`）、
`automation/`（自动化画布 graph/canvas）、`enhance.js`（组件增强迁移残余，目标清空）、
`track.js`（访问归因采集，构建期内联产物，长期保留）、
`icons.js`（Lucide 图标数据，vendor 性质，不算业务 JS）。

CSRF：HTMX 请求经 `<body hx-headers='{"X-CSRF-Token":"{{ .["csrf_token"] }}"}'>` 继承；原生表单必须显式加
`csrf_token` 隐藏域；fetch 请求必须带 `X-CSRF-Token` 头（`workbench.js` / `media-lib.js` 已封装）。

### 认证与鉴权（三层链）

`Session + Cookie` 认证（cookie 只存 user_id/username/session_id/issued_at，资料走 Redis）→
`CSRF`（所有 POST 写操作强制校验）→ `Casbin`（`Enforce(user_id, path, method)`）。

| 路由组 | Session | CSRF | Casbin |
|---|---|---|---|
| `/api/captcha`、`/api/admin/login` | 豁免 | 豁免 | 豁免（login 另挂 IP 限流） |
| `/api/admin/{logout,profile,routes}` | ✅ | ✅ | 豁免（声明 `permission.Exempt`） |
| `/api/*` 其余业务接口（`authorizedAPI` 组） | ✅ | ✅ | ✅ |
| `/admin/*` 页面、`/`、`/workbench*` | ✅ | ✅ | —（页面路由） |
| `/_fragments/{type}`、`/analytics/collect`、`/payment/callback` | 公开面（各自判定） | 公开面 | 不走 |

实际前缀以装配代码与 `internal/routers/testdata/routes.snapshot` 为准。
Cookie 属性：`HttpOnly`、`Secure`（release 自动启用）、`SameSite=Lax`。

### 登录安全

- 密码 bcrypt；验证码图片化（`/api/captcha` 只返回 `captcha_id` + `captcha_image`，答案绝不下发）
- 登录失败 ≥5 次只写 `locked_until_time = now+30min`（自动过期），**绝不修改 Status**
- 失败计数必须用**原子 SQL**（`count = count + 1`），禁止读-改-写回

### 路由

- **只用 `GET` 和 `POST`**；禁止 RESTful 路径参数，全部用 Query 参数
- 主路由聚合在 `internal/routers/routes.go`；模块路由在 `internal/module/<模块>/inbound/http/`
- 健康检查 `GET /livez`、`GET /readyz`（组件级就绪）
- 静态面：`/site`（激活产物，`http.Dir` 只读）、`/storage`（媒体上传）、`/static`（后台静态资源）
- CORS 白名单来自 `server.cors_allowed_origins`；release 无白名单拒绝跨域；release 的 TrustedProxies
  为 nil（不信任 XFF）

### 响应与错误处理

| 请求类型 | 响应格式 |
|---|---|
| HTMX 请求（`HX-Request: true` / `Accept: text/html`） | Jet 渲染的 HTML 片段 |
| JSON API | `Response{Code,Message,Data}`（`pkg/response`） |

- 未登录页面请求 302 到 `/admin/login`；API 请求返回 401 JSON
- 业务模块统一通过模块 `enums` 提供响应消息；`pkg` 和系统包直接用中文提示或原始 `err`
- **后台页面 handler 禁止直出内部错误**，三种形态一起管：① 响应写入（`c.String(500, err.Error())` /
  `*.ErrorWithMessage`）；② 重定向 query（`?err=` 里塞 `err.Error()`）；③ 模板数据（`data.Errors = …`）。
  走 `shell.PageError` / `pkg/response` + enums。门禁 `bash scripts/check-no-internal-error-leak.sh`，
  配**带理由的豁免清单**（条目不再命中即失败，禁止只增不减）。
- **每个模块要有「错误文案三件套」**：① 可透出业务文案的**白名单**（`XxxFacingMessages`，与 enums 常量
  一一对应，用 AST 对账测试钉住）；② **归口文案**（未命中时返回的可翻译 key，如 `adminenums.ErrInternal`；
  `sys_i18n` 主键是 `(item_key, lang)`）；③ **结构化日志**（原文只进日志，带场景与 user_id）。
  样板见 `internal/module/admin/inbound/http/admin_err.go`、`navigation_err.go`、`orderFacingText`。
- 判据是**形状**不是字面量：新增 handler 先问「这个 err 会不会进响应」，而不是等门禁逐个堵接收方别名。

### AI 工具（MCP）开发

工具定义在 `internal/module/<mod>/inbound/mcp/`，读用 `mcp.New`、写用 `mcp.NewWrite`
（后者自动往 schema 追加 `confirm` 与 `idempotencyKey` 并置 required —— **工具作者不要
在自己的入参结构体里重复声明**）。装配点在 `internal/routers/assembly.go`，
**注意局部变量的可见性**：`analyticsSvc` / `commentSvc` 这类要等到建它的那一行之后才存在，
`pageService` 只在 `assembly_publish.go` 里可见（放错位置报 `undefined: xxxSvc`）。

- **工具的输出必须带上下一步动作所需的必填参数**（本仓实测出现 6 次）：`stock_find` 漏
  `variantId`、`stock_reasons` 漏 `id`、建活动缺 `accountId`、分类写工具缺 `parentId`、
  `page_find` 漏 `kind`、退货详情缺明细 —— 症状都是**模型停在原地问用户要**，而它本可以
  自己走完。落地做法：**写工具之前先把清单工具补上**，不要等真机暴露。
- **只有 `mcp.Result.Text` 会回到模型**（`internal/module/ai/service/ai_session_chat.go` 取
  `runRes.Text`；`Data` 只进审计事件）—— 所有细节（id、金额、时间、状态）都要写进 `Text`。
- **权限点先查有没有带 `permission.X` 参数的路由**：`rg -n 'permission\.XxxYyy'
  internal/module/<mod>/inbound/http/*.go`。`CasbinMiddlewareForPath(obj)` **只做中间件、
  不登记权限声明**，于是 `permission.RoutesOf` 返回空 → 工具调用一律 forbidden
  （`ai.err.toolForbidden`），**而页面本身完全正常**。没有路由 = 没有权限点 = fail closed，
  不要凑一个相近的权限点。样板：`internal/module/mail/inbound/http/mail_page_router.go` 的
  `declareMailPageObjects()`。
- **只读接口一律单独声明**，绝不复用带写方法的既有 port（`CustomerQueryReader` 而不是
  `CustomerAdminPort`，后者还带 `SetCustomerStatus`）。
- **「可视化页面选组件」这条边界要按「必填参数是不是 AST」判**，不是按模块名判：
  page 的 `draftDocument`、block 的 `document`、contenttemplate 的 `DraftDocument` 都是
  编辑器维护的结构 —— 让模型盲写只会产出编辑器里打不开的东西。同类模块的工具只做
  **元数据**（名称、分类、复用模式、发布、改网址），建的时候给一个能被编辑器打开的空文档，
  改的时候把读回来的 document 原样带回去。空文档的形状与 `builder.Page` 对齐：
  `{"settings":{},"root":[]}`（**settings 是对象、root 是数组**，写成 `{"root":{...}}`
  会在解析期失败，而工具侧只看得到「执行失败」四个字）。
- 测试传参一律 `map[string]any` —— Go 结构体序列化会把零值写成 `""`，而 `mcp.Enum` 的
  白名单**拒绝空串**（模型不传可选参数时 JSON 里根本没有那个键，两者不是一回事）。
  `mcp.ArgsError` 是 **struct**（写 `&mcp.ArgsError{Msg: …}`）；`mcp.Tool` 的调用方法是
  `Invoke` 不是 `Call`，取 schema 是 `SchemaJSON()`。
- 真机验证记 `ai_tool_call_log`（状态在 `status` 列，**没有 `ok` 列**）；判断新二进制是否
  生效要 `pkill -x gowp-dev` 后**轮询等进程真退出**，否则端口被旧进程占着、新进程起不来
  而 `curl` 照样 200。


## 数据库

论证、实测数据与操作步骤见 [`docs/rules/database.md`](docs/rules/database.md)。

- 查库走 dbx MCP 并显式传 `connection_name`；schema 权威是 [`docs/schema-snapshot.md`](docs/schema-snapshot.md)。
  查询一律参数化，context 必须传播（`WithContext`）。
- **时间列**：**新增表**的管理时间列命名 `create_time` / `update_time` —— **不要再用 `created_at` / `updated_at`**
  （存量各有 119 / 73 处，按现状为准、不做批量改名）；**业务时刻列**沿用既有 `*_at` 风格
  （`published_at` / `registered_at` / `viewed_at` / `started_at` / `orders.paid_at` /
  `membership_assignments.assigned_at` 等 —— 「何时发生」与「何时被改」是两类列，
  不要为了统一名字把业务时刻改成 `*_time`）；类型 `timestamptz` + Go `time.Time`
  （不再引入 int64 时间戳）；**对外 JSON 只到秒**，dto 时间字段一律 `utils.JSONTime`。
- **软删除列名**统一 `deleted_at`。
- **model 一律不声明列型**（`internal/architecture/model_gorm_tag_test.go` 守门）。例外：gorm 无法推断的
  `json.RawMessage` / `JSONMap` / `StringArray` 等必须保留 `type:` 或 `serializer:`。
- **新增 `authorizedAPI` 下的接口：加权限点常量 + 在路由注册处声明，不写 seed 迁移**（常量加在
  `internal/permission/codes.go`，作为 `RouteGroup.GET/POST` 第二个参数）。漏权限点会让**含超管在内全员
  403**。另跑 `bash scripts/check-permission-gaps.sh` 抓注册期看不见的缺口。
- **迁移**：`public/migrations/` 版本化 SQL（幂等），`register.go` 注册；seed 用 ConditionSQL。
  · **删能力时要连 seed 的 SQL 与幂等条件一起收口** —— Migrations 台账先跑、Seeds 台账后跑，
  停在 `register` 里的「删除」会被 `registerSeed` 的 seed 赢回去（122/104 的真实故障）。
  · 改列名时同步改**触发器 / plpgsql 函数体**与 **seed SQL**（历史迁移 SQL 保持原样）。
  · `CheckSQL` 的 `?` 由迁移器传入的是**表名**，其它判定值必须写进 SQL 字面量。
  · **seed 门槛判据必须枚举「本批自己的对象」（上界封闭）**：`item_key IN (…)` / `permission_code IN (…)`
  逐条列举，**禁止用 `LIKE` 前缀或全库总量**。两个方向都发生过真实故障：前缀下已有别的批次的行 →
  计数虚高 → **本批被静默跳过**（058）；将来新增同前缀 key → 计数永远追不平 → **每次启动都重跑**
  （076）。判据的偏差方向要刻意选「宁可重跑，不可静默跳过」。
  · **新迁移 SQL 上线前用 `PREPARE` 静态校验**：`mustSQL` 只校验 embed 文件**存在**、不校验 SQL 可执行，
  所以「Go 编译通过」≠「迁移跑得起来」；而迁移链一旦中断会堵住其后**全部**注册项。用 dbx 跑
  `PREPARE chk AS <去掉注释的 SQL>`（数据库做语法与表/列语义分析、**不执行 DML**）再 `DEALLOCATE`，
  无需起服务。
  · **幂等条件会让「跳过」看起来像「通过」**：带 `ConditionSQL` 的 seed 在已有这些对象的库上会直接跳过，
  于是它的 SQL 从未被执行过 —— 要到**新建空库**（`support.NewMigratedPGTestDB`，每次复制模板库）
  或全新部署时才第一次真跑，而那时失败会中断其后整条注册链。540 实测：`VALUES` 列表一处**缺**逗号、
  一处**多**逗号，PG 的报错位置落在 `ON CONFLICT` 那一行，很容易误判成冲突子句写错。
  判据：动过 seed / 迁移 SQL 后，要么 `PREPARE`、`psql -f` 干跑，要么跑一次会建空库的 feature 测试；
  「跑过测试」不等于「这条 seed 能跑」（条件命中就没跑）。
  · **结构性错误（逗号、括号）有两个方向**：「缺」与「多」各自是一类缺陷，只扫一个方向的正则
  （例如只查「不以逗号结尾的行」）给出的是**假绿**。这类校验一律交给真解析器（psql / PREPARE），
  不要用计数或单向匹配代替。
- **主键选型按「这个 id 会不会出现在系统边界之外」判**：对外实体用 **uuid**（应用层 `uuid.NewString()`），
  纯内部流水与字典用 **bigint identity**；两套并存是设计。只增的分区流水表用 **UUIDv7**
  （`utils.NewTimeOrderedID()`），对外实体继续 v4（v7 的时间前缀会透露创建时间）。
  判据只约束新表，**存量按现状为准**。
- **界面上出现的枚举类取值，先查字典表再写代码**：货币符号、国家名、语言名这类
  「运营会增删的取值」已经有表（`sys_dict`，按 `dict_type` 分组：`currency` / `language`，
  每行带 `symbol` / `url_code` / `ui_available`），**不要在代码里另建一份映射**。
  两条真源必然漂移，而漂移的表现是「后台加了港币、页面上仍显示三字母代码」——
  不报错、测试也不红，且没人知道该改哪一处。取数走 `sysconfig` 的 `ListDictOptions(ctx, dictType)`
  （`DictOption` 已带 `Code` / `Label` / `Symbol`），**不要从 `Label` 里切**
  （标签格式「CNY ¥」一改就静默切错）。
  读不到时降级而不是报错：金额照常显示、只是没有符号前缀 —— 把整张卡打空比少一个符号糟得多。
- **列表达式与实参用 `selectExpr` 绑定，个数必须相等**（`internal/module/order/model/order_model.go` 的
  `selectExpr`）：GORM 的 `Select(串, 实参...)` 用 `strings.Count(v, "?") >= len(args)` 决定分派 ——
  `?` 少于实参时它**不报错**，而是把实参当**追加列名**拼到 SELECT 列表末尾，生成
  `... AS amount,received,completed,paid` 这种语法错的 SQL 且**一个参数都没绑上**；错误现场离拼串处很远
  （PG 的一句 syntax error）。实测：`spentTotalsSelect` 1 个 `?` 配 2 个实参，报的是
  `syntax error at or near ","`。
  · 反过来也不能改用 `gorm.Expr` / `clause.Expr` 传给 `Select`：GORM 的 `Select` 只认 `string` 与
  `[]string`，传表达式会得到 `unsupported select args`（实测）。
  · 参数对不上属于**编程错误**（不是数据问题），所以 `selectExpr` 直接 panic —— 写对了永不触发，
  写错了第一次跑到就炸，而不是等某个字段恰好为空才露出来。
- **一张表只有一个模块读写它**：别的模块要它的数据走**契约**（`internal/module/<X>/contract`），
  不要在 model 里写别人的表名 —— 表名与列名一旦被第二个模块引用就成了跨模块接口，改一列不会有编译错误、
  只会在那边静默读到空值。已改：`projects`（6 处 → project 契约）、`page_artifacts`（4 处 →
  `artifactcontract.PageArtifactReader`）。
  · `page_artifacts` **没有 `project_id` 列**，所以「产物属于哪个工程」必须经 `page_id` 再查 `pages` ——
  这一步拆成两次读：产物行 → 页面问 artifact 契约，页面 → 工程留在 page 模块（各自只读自己的表）。
  · 例外：PG **系统表**查询没有 Entity 可映射，保持裸 SQL —— 在 `internal/module/` 之外的是
  `internal/partition/partition.go`（`pg_class` / `pg_inherits` 与两处 RLS 覆盖统计），
  在范围内的有 `internal/module/plugin/model/plugin_model.go`（`pg_namespace` + `pg_class`，
  外加两处插件 schema 的 DDL）。
  · 门禁是 `internal/architecture/module_boundary_test.go` 的 `TestNoCrossModuleServiceModelImport`
  —— **它连 `internal/module/<X>/**` 下的 `_test.go` 一起查**：测试里要造跨模块的读时，写一个实现该
  contract 的 stub，不要去 import 对方的 model / service 包。

- **裸 SQL 只减不增**：`internal/module/` 的生产代码不得出现 gorm 的 `Raw(` / `Exec(`
  （`internal/architecture/raw_sql_boundary_test.go` 守门）。列名表名写成字符串就没有编译期保护，
  `selectExpr` 那类参数守卫也只在链式路径上生效。
  · 登记表两张：`rawSQLAllowedList`（设计上不该 GORM 化，如 PG 系统表与 schema DDL）、
  `rawSQLDebtList`（待还存量，**登记的是此刻的处数**）。**两个方向都失败** —— 处数变多（新写裸 SQL）、
  处数变少却没同步删条目。**改完一个文件就删掉它那一条**，别让登记表退化成注释。
  · 判据只认 `CallExpr` 且排除接收者以 `.m` 结尾的调用：前者是为了不把 `r.Index.Raw`
  这类「字段名恰好叫 Raw」的读法算进来，后者是为了不把 model 自己包装的
  `func (m *Model) Exec(ctx, sql string) error`（service 侧写 `s.m.Exec(…)`）当成 gorm 调用。
  · 存量清单（本次整改推进中，改完同步删条目）：ai/model 6、mail/model 7。

- **数据域（datarule）白名单由拥有该表的实体声明**：字段上写 `datarule:"label=…;ops=…"`，经
  `pkg/datarule.DomainFromEntity` 派生，装配入口注册（且在注册路由之前）。**没有 tag 的字段不在白名单里**
  （fail-closed）—— 不要另抄一份字段表。
- **RLS（DB-009）**：策略已铺（迁移 215），但**换连接角色之前不生效**（超级用户绕过 RLS）。顺序不能反：
  先包 `pkg/rls.InProjectScope`，再换 `database.user` 为非超级角色（`bash scripts/rls-role-setup.sh`），
  并把 `database.require_rls_role` 置 `true`。详见 [`docs/rls-role-cutover.md`](docs/rls-role-cutover.md)。

## 聚合口径（跨模块只读）

表隔离下，同一个业务事实的取数口只属于拥有那张表的模块；消费方（概览页 / AI 工具 / 外部
MCP / 客户页）只拿结论、不参与计算（`order_range_model.go` / `order_top_product_model.go` 的文件头
各记了一次）。

- **同一张页面上的两个数字必须同源**：若它们回答的是同一批行（例如「区间商品销售总量」与
  「热销商品榜」都由 `order_items` 按同一条件聚合），筛选条件要抽成一个共享常量
  （`orderItemScopeSQL`），不要各写一份 `WHERE`。两份各自维护的失败模式是「榜单排除了取消单、
  总量忘了排除」—— 两个数字互相矛盾，而每一处单独看都对、也都不报错。
- **同源不止「同一批行」，还包括「同一个桶」**：一张图上多个序列（销售额 / 订单数 / 浏览量）
  共用一根横轴时，各序列的**聚合粒度必须一致**。实测：概览页趋势图切到「按小时」后，订单侧给
  `YYYY-MM-DDTHH:00` 的桶、浏览侧仍按天给 `YYYY-MM-DD` —— 两者被分桶函数归进**不同的桶**，
  图上多出一根来路不明的柱子，而两条曲线各自看都对。判据是「一个序列改了粒度参数，
  另一个序列必须在同一个请求里被改」：粒度属于**窗口的一部分**，和 `from`/`to` 同级，
  由调用方统一推导后传给每个取数口，不要让各端口自己默认。
  同一粒度下还要注意**预聚合表是否有该粒度的行**：`page_views_daily` 的最小粒度是天，
  小时粒度必须落到明细表（`page_views`，按月分区），用汇总行凑小时桶会得到「一堆空桶 +
  一个总量塞在某小时」，形状全错而总数看着还对。
- **桶键读回来必须还是同一个时刻**：`date_trunc(...)` 与 `::date` 的**返回类型不同** ——
  前者是无时区 `timestamp`，后者是 `date`。驱动把 `timestamp` 读成 `time.Time` 时按
  **本地时区**贴位置：本机（+08）下 `2026-10-05 01:00` 读出来是 `2026-10-05T01:00+08`，
  调用方再 `.UTC()` 就是 `2026-10-04T17:00`，桶键整整偏一个时区。实测两种症状：图上多出一根
  「昨天 17 点」的柱子（并集后桶数 24 → 25，窗口是 10-05 而那根柱是 10-04），以及
  **按小时 map 查找静默落空、那一小时变 0**（订单明明落在 UTC 05:00，页面上是空的，
  不报错、日志干净）。修法是聚合列里 `AT TIME ZONE 'UTC'` **写两遍**（第二遍标回
  timestamptz），`::date` 不用（`date` 的语义就是「某一天」，没有时刻可漂）。
  判据：`TestDayAndHourBucketsReturnUTCWakeClock`（order）与
  `TestViewBucketsReturnUTCWakeClock`（analytics）—— 断言读回来的桶键 UTC 挂钟等于插入时刻，
  去掉第二遍、或把 `::date` 顺手改成 `date_trunc`，两条都会变红。**这条不能只写进注释**：
  两种写法在同一段 SQL 里长得几乎一样，「统一一下」是最容易发生的重构。
- **这条要有会变红的判据**：feature 测试直接断言两侧的等式（榜单各项之和 == 总量，
  见 `TestOrderSoldQuantitySharesScopeWithTopProducts`），而不是只断言各自的值 ——
  只断言各自的值时，改了一侧仍然全绿。粒度这类有一条更省的等价判据：**同一组数据在两种粒度下
  的桶集合必须能对上**（把粗粒度的桶按细粒度展开后求和，应与细粒度逐桶之和相等）。
- **参数顺序写进注释**：拼接会改变 SQL 文本，但不该改变 `?` 的顺序。参数错位不编译报错，
  只会给一个看起来合理的数字。

## model 层定位（评审与开发共同遵守）

`model` 是**表访问单元（Repository）**，不是 DDD Domain Model。

- ✅ 允许：本模块表的 CRUD、聚合与**聚合内原子组合**（如 `CreateWithRevision` 在同一事务内写
  `pages` + `page_revisions`）；查询条件一律以参数传入
- ❌ 禁止：跨 model 调用、业务规则/决策（谁能删、状态机）、**跨聚合/跨模块事务**、方法内写死业务条件
- 跨聚合/跨模块事务在 service 层编排：model 暴露 `Transaction()` 透传（或方法接受外部 `*gorm.DB`），
  由 service 决定事务边界与回滚
- `DB(ctx)` / `RevisionDB(ctx)` 等裸句柄是 model 的**内部实现细节，只允许被本 model 的仓储方法消费**；
  service 禁止调用它们拼查询 —— 新增查询需求 = 给 model 加方法
- 评审拦截项：`internal/module/*/service` 命中 `\.DB(ctx)` / `\.RevisionDB(ctx)` 即打回（含先存变量写法）。
  机器校验 `bash scripts/check-service-db-boundary.sh`
- `contract/` 只放模块对外接口；`service` 依赖其他模块能力时直接引用对方 `contract`

**admin 豁免条款**：`admin` 是管理面 CRUD 大模块（六领域合并、同包直调），service 层经 `DB(ctx)` 直查
**明文豁免**。边界：仅限 admin 模块、仅限本模块表、跨表事务仍须 `Transaction()` 编排、简单 CRUD 之外的
业务查询仍走 model 方法。**新增模块一律禁止直查。**

## 写操作的事务与回滚（评审必查）

**判据**：一次用户可感知的写操作（保存 / 删除 / 状态推进 / 批量 / 导入安装发布），只要涉及**两处及以上
持久化写入**，就必须落在**同一个数据库事务**里；任一步失败整体回滚，不留半截状态 —— 「有主实体没关联行」
「有单据没库存」「有实体没流水」「有商品没库存记录」都算。持久化写入 = 主实体 + 关联行 + 流水/变更记录 +
计数 + 权限策略/菜单 + Redis。

- **事务边界由 service 决定，句柄从 model 往下传**：跨模块只把 `*gorm.DB` 传给对方的 `…Tx` 方法
  （先例 `masterdata.RecordChangesTx`）—— **不共享表、不跨库**。对端没有 `…Tx` 就加一个，
  不要用「先写 A 再补偿 B」蒙混。
- **`rls.InProjectScope` 自带事务**：方法里若既有它、又有**它外面**的写，外面那一处就是缺事务的。
- **读-改-写必须有行锁或原子 SQL**：`SELECT … FOR UPDATE`（按标识升序加锁避免死锁），或把守卫写进 WHERE
  的原子更新（`SET x = x + ? WHERE x + ? <= limit`，受影响 0 行即拒绝）。禁止「先读出来算完再写回去」。
- **补偿只用于跨库/外部系统**（文件、Redis、第三方接口）：必须**幂等 + 留痕 + 可重放**，并在注释里写明
  「为什么不能用事务」。跨模块的数据库写入不属于这一条。
- **冲突与数据不一致一律打回给人**：唯一键撞车、存量纠正要在错误里列出**可定位的数据**（哪张表 / 哪个仓 /
  哪个码 / 哪几行 id），让操作者决定；**不允许**自动加后缀、静默合并、丢弃其中一行。
- **门禁**：`public/test/architecture/tx_boundary_scan_test.go` 扫「一个 service 函数里 ≥2 处写调用却看不到
  事务标记」的候选，命中要么包进事务、要么在允许清单写明理由（过期条目会让测试失败）。
  · 它是**启发式**（命名不在识别表里会漏）—— **「门禁绿」不等于「事务没问题」**，评审仍要人看一遍。

## 测试

基建实测结论与验证方法论见 [`docs/rules/testing.md`](docs/rules/testing.md)。

- 默认跑现有测试，不新增额外测试框架。接口优先维护 feature 链路测试（`public/test/`，真实 PostgreSQL），
  复杂逻辑补 unit；PG/Redis 不可用时 `t.Skip`。全量测试并发跑：`make test` 走 `-p $(nproc)`
- **测试的表结构一律来自生产迁移**：`support.NewMigratedPGTestDB(t)`（复制模板库）管需要真实 schema 的用例；
  `support.NewPGTestDB(t)`（空库）只给「自建表 / 故意构造旧 schema」的用例。
  **禁止手抄 `CREATE TABLE` 伪造「看起来像生产」的表** —— 会与生产静默分叉。并发敏感代码跑 `-race`。
- **计数与命名不是判据**：`rg -c` / `grep -c` 只是筛查起点，写进结论前必须回读上下文确认语义 ——
  常量名可能不等于值、关键词计数可能不是目标数量、判据本身可能选错。「门禁绿」也不等于「没问题」。
- **所有组件必须适配多端**（硬规则）：产出必须在桌面 / 平板 / 手机上正确渲染，在鼠标 / 滚轮与触摸板 /
  触屏 / 键盘下都能操作。宽度写 `min(100%, <设计宽度>)`、绝对值按视口封顶、触屏等价形态用
  `CSSBuckets.AddHoverNone`。三条视口（1440 / 768 / 375）× 四种输入各验收一次。
  完整规范见 [`docs/rules/frontend.md`](docs/rules/frontend.md)
- **交互改动**：逐种输入方式各测一遍（鼠标拖拽 / 滚轮 / 触屏滑动 / 键盘 / 点击）—— 程序化调用覆盖不到
  真实输入路径。**动画 / 观察者 / 时间线类改动**必须读到「值在变化」，只读属性名或计算值不算验证，
  且触发条件要真的成立。**无头环境不出渲染帧**（`IO` / `rAF` / `scroll` 都不触发）—— 验证时先截图强制
  出帧，实现时优先同步几何计算 + `setTimeout` 节流。
- **读到「过渡中的计算样式」不等于读到终态**：给元素加上触发过渡的类之后立刻读
  `getComputedStyle(el).maxHeight`，拿到的是**过渡起始值**（实测 6000px 而不是 0px），
  据此会得出「规则没生效」或「改完反而坏了」这两个都是错的结论 —— 规则其实是对的。
  判据：读过渡属性前先等 `transitionDuration`（或读 `transitionDuration` 再按它等），
  或者干脆改读**类名是否在 + 规则是否存在**这类不随时间变化的量。实测两次误判都出在这里。
- **「用类名控制布局」的功能，必须问一句「页面重新加载后这个类还在吗」**：只存在于内存里的类名
  （提问后加的 `.is-asking`、展开态、选中态）在刷新或从别的页面跳回来时会丢，而**依赖它让位的内容
  会被历史回填重新铺开** —— 症状是「隐藏不生效 / 内容被挤压」，根因却在另一个地方。修法是让回填
  数据的那段代码把类补上（`loadHistory` 拿到非空历史时加 `.is-asking`），不是去调那些隐藏规则。
- **CSS 的视觉层次是三层，不是两层**：页面底 + 卡片面 + 描边。页面底与卡片同为纯白、只靠描边
  区分时，整页读起来是「灰扑扑的一片」，而每一处单独看都没有问题 —— 这不是配色偏好，是缺一层底色。
  落地形态：浅灰页面底、纯白卡片、极浅描边，再加一层几乎看不见的阴影；整套色值收在一个作用域变量组里
  （如 `.stack.dash-page` 的 `--dash-*`），**亮暗两套都要给**（只给亮色时暗色主题会变成白底黑字）。
- **读「过渡中的计算样式」/「类名内存态」/「CSS 三层层次」**三条的实测过程与验收判据见
  [`docs/rules/frontend.md`](docs/rules/frontend.md) 末三节；上面每条的判据是它的浓缩。
- 一次「覆盖全部能力的实例页」是性价比最高的集成验证。

## Git 与工具约定

- 每次 commit 用中文注明改动文件路径和修改内容简述
- **删除能力时**：grep 该能力名称（表名、权限点、枚举、注释关键词）并同步清理相关注释与文档
- 文档更新与代码提交分开；修改规则文件（本文件及子目录 CLAUDE.md）前先重新读取，桌面端可能并发改写
- GitHub 操作优先 `gh` CLI；Go 项目发版优先 GoReleaser
- 语言运行时版本由 vfox 管理（`~/.vfox`），禁止 Homebrew/apt/系统包安装运行时；Node.js 依赖优先 pnpm，
  Python 用 uv
- **用 `run_code` 拼 Go 代码时不要套 JS 模板字符串**：Go 的 struct tag 与原始字符串都用反引号，
  而反引号会**提前终止**模板串。报错是 `Expected ';', '}' or <eof>` / `Expected ',', got '{'`
  这类语法错，看不出与反引号有关（本仓实测连续三次踩到，每次都先怀疑别的原因）。改用 `write` /
  `edit` 工具直接写文件 —— 它们的参数是 JSON，反引号安全；批量改动再考虑脚本。

## 文档导航

**规则**： [`docs/rules/`](docs/rules/README.md)（数据库 / 测试 / 组件前端细则）·
[`docs/agents/parallel-batch-rules.md`](docs/agents/parallel-batch-rules.md)（批次执行者）·
各目录 `CLAUDE.md`

**项目内容**： [`docs/01-overview.md`](docs/01-overview.md)（概览与冻结边界）·
[`docs/13-module-inventory.md`](docs/13-module-inventory.md)（**模块清单权威**）·
[`docs/03-pipeline.md`](docs/03-pipeline.md)（发布管线）·
[`docs/04-B-dynamic-development-guide.md`](docs/04-B-dynamic-development-guide.md)（动态能力 How-To）·
[`docs/02-C0-component-base-spec.md`](docs/02-C0-component-base-spec.md) 及 `docs/02-*`（组件规格）·
[`docs/schema-snapshot.md`](docs/schema-snapshot.md)（schema 与枚举）·
[`docs/06-plugin-system.md`](docs/06-plugin-system.md) 与 `docs/06-*`（插件体系 / 路线图 / ADR）·
[`docs/03-A-workbench.md`](docs/03-A-workbench.md)（工作台）·
[`docs/05-implementation-plan.md`](docs/05-implementation-plan.md)（阶段计划）·
[`docs/api-stability.md`](docs/api-stability.md)（API 稳定性分级）·
`docs/agents/`（Issue / Triage / 术语）· `docs/audit/`（审计报告）
