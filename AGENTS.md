# AGENTS.md

本文件描述 go_wp 仓库的实际开发约定，是 DSH 会话的最高项目级规则。
兼容说明：旧 `CLAUDE.md` 内容已并入本文；子目录规则见 `internal/module/CLAUDE.md`、`pkg/CLAUDE.md`、`public/CLAUDE.md`、`docs/agents/`。
强制内容：本系统开发阶段，不需要兼容任何老的代码，有问题直接重构

## 语言要求（最高优先级）

- 所有回复、分析、总结、计划、报告一律使用简体中文
- 推理/思考过程也使用简体中文
- 工具输出、代码、上游数据即使是英文，回复仍必须是中文；代码标识符、专有名词、命令保留原文

## 项目概览

go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`。

控制面（CMS + Builder + Build Worker）把可编辑的 Page Document 和 CMS 内容编译为不可变静态 Artifact；访问面（Static Server / CDN + Runtime Fragment Endpoint）只读取已激活的 HTML/CSS/JS。Go + Jet 只在 Preview/Publish 构建阶段运行，访客请求不执行任何模板或数据库查询。

**技术栈**：

| 层 | 选型 | 职责 |
|---|---|---|
| Web 框架 | Gin | HTTP 路由、中间件链、请求绑定 |
| 后端与构建器 | Go | CMS、BuildContext、Publish Compiler、版本与发布状态机 |
| 构建期模板 | Jet v6（`github.com/CloudyKit/jet/v6`） | 发布阶段把受限 Fragment 与 BuildContext 渲染为最终 HTML；后台页面 SSR |
| Admin 交互 | HTMX（**本地 vendor**：`internal/templates/static/js/ui/htmx.min.js`） | 草稿、预览、构建、发布、回滚请求 |
| 认证 | Session + Cookie（gin-contrib/sessions + Cookie 存储） | 替代旧 JWT 方案，HTMX 请求自动携带 Cookie |
| 鉴权 | Casbin（自研 persist.Adapter） | Enforce(user_id, path, method)；业务 API 已挂载 |
| 公开动态片段 | HTMX + Go Handler | 按 Registry capability 返回受控 HTML Fragment（`runtimefragment`，挂载 `/_fragments/{type}`）；**产物按需内联 htmx** —— 页面 HTML 里出现任一 `hx-*` 属性时由 builder 注入（`ui_script.go` 的前缀命中），一个都没有就一个字节都不注入（htmx 是行为库，不需要控件基座与 `ui.css`） |
| 富文本编辑器 | Trix 2.x（本地 vendor：/static/vendor/trix/） | 文章内容编辑；白名单清洗 + h1 降级 h2 |
| 数据库 | PostgreSQL（主库） | CMS 内容、Page 草稿、Artifact 元数据和依赖索引；MySQL 为历史兼容；SQLite/SQL Server 驱动已移除 |
| 会话存储 | Redis（pkg/cache） | 用户会话、封禁标记、在线心跳（**Critical 组件，配置必须启用**） |
| Artifact 存储 | 本地文件系统 / 对象存储 | 不可变构建文件与内容寻址资源 |
| 访问（公开站点） | Static Server / CDN | 直接提供激活后的 Artifact |

> ~~Vue 3 / vue-pure-admin~~ 已废弃并移除。所有后台界面由 Go 渲染 Jet 模板 + HTMX 片段实现。

## 常用命令

```bash
# 后端
go run cmd/main.go
go build -o app ./cmd        # 生产构建用「包路径」形式，且在 git 工作区内执行（见「组件更新与重建」）
go test ./...
go test -race ./...          # 并发回归
go vet ./...
```


### 组件更新与重建

组件（Go 实现 + `internal/templates/components/*.jet` 模板）编译进二进制，**部署新组件后已发布的
产物仍然是旧组件渲染的字节**。系统不会自动重建，但会在启动时给出准确的影响面：

```text
启动 → builder.RegistryVersion() 与 page_artifacts.registry_version 比对
     → 差异页面标记 stale（只标记、不重建，避免拖住启动链）
     → 日志：检测到组件已更新：相关页面已标记待重建（count / registryVersion）
     → 运维经 page.RebuildStale 重建，或由后续编辑/发布自然覆盖
```

`RegistryVersion` = 构建指纹（`vcs.revision`+`vcs.modified`）+ 组件清单指纹（类型 + Props 的
json/ct 标签结构 + 可翻译白名单）。两者的分辨力互补：

| 构建方式 | vcs.revision | Go 代码改动（BuildView/CompileCSS） | Props/模板改动 |
|---|---|---|---|
| `go build -o app ./cmd`（git 工作区内） | ✅ | ✅ | ✅ |
| `go build -o app cmd/main.go`（单文件） | ✅ | ✅ | ✅ |
| `go run …` / 无 git 环境 | ❌ | ❌ | ✅ |

无 VCS 信息时退化为「组件清单指纹」单独生效：**能发现字段与控件声明变化，发现不了只有 Go 代码
变了的改动**。生产环境请确保二进制带 VCS 信息（在 git 工作区内构建即可，Go 1.18+ 默认嵌入）。

> 坑：不要用 `bi.Main.Path` 之类的构建期变量给指纹兜底 —— 它随构建方式变化（`go run cmd/main.go`
> 是 `command-line-arguments`，包方式是模块路径），会让同一个 commit 因构建命令不同算出不同版本，
> 表现为「每次切换构建方式就误判全站待重建」。

> 产物文件丢失（误删/磁盘损坏）不属于重建范畴：用 `POST /api/page/artifact/rebuild` 按元数据里的
> `source_document` 重建，并以返回的 `hashMatched` 判断是否原样恢复（组件已更新时会为 false）；
> `GET /api/page/publication/audit` 可巡检 active 目录里的悬空链接。

## 架构约束（核心不变量）

以下不变量贯穿全系统，违反任意一条即为设计缺陷。详细论证见 `docs/01-overview.md` 等文档。

1. **控制面与访问面分离**：访客请求不查询数据库、不执行 Jet、不解释 AST。URL → 文件映射由 PublicationStore 文件系统状态决定，不由数据库指针决定。
   · **唯一例外：访问统计打点**（BIZ-8）。访问面是静态产物直出，Go 不在请求路径上 —— 计数只可能来自客户端，因此 `POST /analytics/collect` 是访客浏览器发起的**唯一写库路径**。它的边界写死在三个地方：接口形状只有「写一条记录」（没有查询 / 删除能力）、**不参与任何页面渲染**（响应恒为 204 空体）、失败一律静默（写库失败只记日志，绝不影响访客页面）。它落库的也全是匿名派生值：IP / 会话 / 访客标识一律带盐哈希，UA 只存粗粒度分类。
2. **两条发布路径共享同一管线**：Page（手工）与 PresentationInstance（自动）走同一 Publish Compiler → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃，ContentTemplate 每次构建参与**（0-B/0-A2 不变式）：Blueprint 只初始化 Page Document，后续修改不传播。
4. **Binding 不是 Query DSL**：Document 只保存白名单 FieldBinding / CollectionSource / MediaBinding，不能保存 SQL、过滤表达式或任意 endpoint。
5. **确定性构建**：同一 Page Document + BuildContext + Registry + Compiler 产生相同 Artifact 字节（有 determinism/fuzz 测试背书）。
6. **冻结边界不可越权**：每个模块、组件、协议都有明确的「负责 / 禁止」边界，详见 `docs/01-overview.md` §5 冻结边界速查。
7. **构建期数据源的依赖方向**（issue #35）：共享形状放 `internal/builder/source`（零依赖，谁都能 import）；业务模块在**自己的契约包**里声明**受限数据源接口**（只有读集合 / 元数据 / 可筛值，写方法不进接口）；`builder/core` 直接持有这些契约接口。**契约包不得反向 import `builder/core`** —— 一旦反向即成环（`core → 契约 → core`），core 就再也无法持有业务契约。新领域接入的六步与两条不变量见 `docs/04-B-dynamic-development-guide.md` §1.4。

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

## 模块现状

> 各模块的完整职责、不变量与落地细节见 [docs/13-module-inventory.md](./docs/13-module-inventory.md)。
> 本文件只保留一句话边界；模块落地后更新那份清单，不要在这里展开实现细节。

| 模块 | 一句话职责 | 不负责 |
|---|---|---|
| `admin` | 管理控制面：管理员、角色、权限点、菜单、部门、数据权限 | CMS 内容、公开站点用户 |
| `common` | 公共业务入口（验证码） | 通用基础设施 |
| `workbench` | 可视化编辑器平台：仪表盘首页、画布预览、检查器（schema→表单）、结构树 | 业务域页面（各模块自注册） |
| `media` | 附件与文件分类 | — |
| `project` | 站点工程、SiteSettings、多主题 | — |
| `page` | 手工 Page 与 Page Document | 槽位指向页面的外观排版 |
| `block` | 复用资产（全局块） | — |
| `artifact` | Artifact 元数据与内容对象闭包 | — |
| `build` | 构建任务队列 | 队列只做调度，编译内核在 `internal/builder` |
| `content` | 固定 CMS 内容（`article`） | — |
| `contenttemplate` | DocumentSnapshot 的版本化结构模板 | — |
| `presentation` | 自动发布实例 | 手工 Page |
| `blueprint` | Page Document 初始化工具（用完即弃） | 构建期 / 运行时模板 |
| `navigation` | 公开站点菜单 | 管理后台权限菜单 |
| `plugin` | 插件体系（组件注册、能力分层） | — |
| `product` | 商品域：商品 / 变体 / 属性 / 分类 / 品牌 / 标签 / 定价 / 捆绑 | 库存流水与扣减、采购、订单、客户 |
| `product/inventory` | 仓库 / 库存真源 / 库存变动 / 货源 / 采购 | 订单 / 客户（销售侧） |
| `masterdata` | 主数据字段级变更记录（append-only） | 数量库存的增减；商品与货源的业务规则 |
| `mail` | 邮箱模块：发信账号、邮件模板 | 短信 / 站内信等其它通知渠道；访客账号 |
| `user` | 访问面访客账号 | CMS 内容与后台管理；会员等级 / 权益 |
| `order` | 订单（销售侧） | 购物车与结算页；真支付网关对接；库存真源 |
| `cart` | 购物车与访客结算 | 订单持久化与状态机；商品与库存真源 |
| `analytics` | 站点访问统计 | 页面渲染与业务逻辑；实时行为分析；保留期归档 |
| `webhook` | 外部集成通道：端点白名单（事件类型 × 目标 URL）+ 投递日志 + 异步签名投递 | 业务事件的产生与内容；重试上限之外的人工补偿 |

> `build` 无独立模块目录：编译内核在 `internal/builder`，发布内核在 `internal/pipeline`。
> `permission/role/menu/dept/datarule` 已并入 `admin` 大模块，不再独立。
> 模块落地后必须同步更新 `docs/13-module-inventory.md`；新增模块代码与规则文件在同一批提交中更新，禁止只加代码不更新清单。

### 命名约束

- `menu` = 管理后台权限菜单；`navigation` = 公开站点导航，两者不可混用
- `admin` = 管理控制面账号；`user` = 访问面访客账号。两者是**两个独立领域**：各自的表、cookie、会话命名空间与鉴权链，不允许互相复用（曾经的危险捷径是「让访客共用 admin 表与会话」，那会让访客 cookie 顶掉后台登录态、并让两套 id 空间相互污染）
- `build → artifact → publication` 是单向流水线，后者不得反向导入前者实现
- 跨模块只使用 `contract` 和不可变 DTO；不得导入其他模块的 `service/model`（不可变 DTO 允许跨模块传递，对齐 `internal/module/CLAUDE.md` 表隔离约定）

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

### 认证与鉴权（三层链）

- `Session + Cookie` → 认证（gin-contrib/sessions + Cookie 存储）：cookie 只存最小认证信息（user_id/username/session_id/issued_at），用户资料走 Redis
- `CSRF` → 所有 POST 写操作强制 token 校验（登录成功返回 token；`X-CSRF-Token` 头或 `csrf_token` 表单域）
- `Casbin` → 鉴权（Enforce(user_id, path, method)；业务权限点见迁移 030/031 seed，超管 is_admin=1 全量策略）

挂载矩阵：

| 路由组 | SessionAuth | CSRF | Casbin |
|---|---|---|---|
| `/api/captcha`、`/api/admin/login` | 豁免 | 豁免 | 豁免 |
| `/api/admin/*` 六领域 | ✅ | ✅ | ✅ |
| `/api/{media,project,block,page,artifact,publication}/*` | ✅ | ✅ | ✅ |
| `/api/{content,contenttemplate,presentation,blueprint,navigation,plugin,inventory}/*` | ✅ | ✅ | ✅ |
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
- 后台页面 handler 禁止 `c.String(500, err.Error())` 直出内部错误，必须走 `shell.PageError` / `pkg/response` + enums

### 数据库

- 开发/审计查库统一走 dbx MCP：连接名与库名以本机 DBX 配置为准（勿在文档里写死连接名）；应用运行时库名见 `config.yaml` 的 `database.dbname`（示例 `config.yaml.example` 默认为 `wp`）。主库 PostgreSQL，最低版本以 CI（`.github/workflows/go-test.yml` 的 postgres 服务）为准；调用 dbx 时显式传 `connection_name`
- 当前 schema 权威说明见 `docs/schema-snapshot.md`（`init_builder_schema.sql` 仅为历史快照）
- 查询一律参数化；context 必须传播（`WithContext`）
- 迁移：`public/migrations/` 版本化 SQL（幂等），`register.go` 注册；seed 用 ConditionSQL（030 权限点 / 031 超管策略）
- **新增挂在 `authorizedAPI` 下的接口，必须同批 seed 权限点 + 超管策略**：该组统一挂
  `CasbinMiddleware()`，按**实际请求路径** enforce，权限点缺失时没有任何策略能匹配，
  **含超管在内全员 403**（072/077/078/079 各踩过一次，151 又补了 page:delete 与 block:clone）。
  改完跑 `bash scripts/check-permission-gaps.sh` 审计「有路由、无权限点」的接口
- datarule 插件字段引用按方言（PG 双引号 / MySQL 反引号）；部门范围整段精确匹配
- **数据域（datarule）白名单由拥有该表的实体声明**：实体字段上写 `datarule:"label=用户名;ops=EQ,NEQ,LIKE"`，
  经 `pkg/datarule.DomainFromEntity` 派生（表名取实体 `TableName()`，列名取 gorm `column` 标签），
  由模块装配入口注册（样板：`internal/module/admin/inbound/http/datarule_bootstrap.go`）。
  **没有 tag 的字段不在白名单里**（fail-closed），不要另抄一份字段表 —— 抄错列名不会报错，
  只会在运行时表现为「过滤条件被静默丢弃 / Omit 一个不存在的列」，规则看起来生效、实际什么都没拦。
  规则配置的字段与操作符在写入侧按域声明逐项校验（dto 声明形状与枚举，service 判定是否属于该域）
- **时间列命名统一为 `create_time` / `update_time`**（审计 DB-019，迁移 205 收口）：全库已无 `created_at` / `updated_at`，新表新列一律用 `*_time`，不要再引入 `*_at`
- **时间列类型统一 `timestamptz`**（迁移 212 收口）：全库 191 个时间列现在都是 `timestamp with time zone`。最后 4 个是 webhook 两张表的 `create_time`/`update_time`（199 建表时用 BIGINT 存 `time.Now().Unix()`，205 只改了列名没改类型，于是它们成了仅有的例外），212 用 `USING to_timestamp(...)` 转换过来。**新表一律 `timestamptz` + Go 的 `time.Time`**，不要再引入 int64 时间戳：它丢掉亚秒精度（投递日志同秒内排序不稳定）、无法直接用 PG 的时间运算与区间索引（BRIN / `date_trunc` 分组要先转换）、与其它表的列比较必须显式转换
- **软删除列名统一为 `deleted_at`**（审计 DB-020，迁移 208 收口）：`sys_menus` 原本的 `deleted_time` 已改名。`sys_attachment` 用 `status` 表达删除属**存量例外**，新表不要照抄
- **工程隔离的 RLS 策略已铺，但在换连接角色之前不生效（DB-009）**：迁移 215 给 53 个带 `project_id` 的对象（40 张基表 + 分区子表）装了 `ROW LEVEL SECURITY` + `FORCE`，策略谓词读会话变量 `app.project_id`（未设置即行不可见，fail closed；`inventory_change_reasons` / `sys_translation` 额外放行 `project_id IS NULL` 的全局行）。**但 PostgreSQL 的超级用户总是绕过 RLS** —— `FORCE` 只约束到表属主，约束不了 superuser / `BYPASSRLS` 角色，而应用连接用的是超级用户 `root`，所以策略目前一行都挡不住。实测（`themes` 表 1 行数据）：root 未设变量读出 1 行，普通角色未设变量读出 0 行
  · **要让 RLS 真正生效，顺序不能反**：先给各模块的读写路径包上 `pkg/rls.InProjectScope`，**再**把 `config.yaml` 的 `database.user` 换成非超级角色（`bash scripts/rls-role-setup.sh <角色> <密码>` 建角色并授权，含 `ALTER DEFAULT PRIVILEGES` 让将来新建的表也自动授权）。反过来的话，没包 scope 的路径会**静默返回 0 行**（fail closed 不报错），表现为「功能突然查不到数据」而没有任何错误日志
  · 分区子表必须单独设：**PG 的 `ENABLE` / `FORCE` 不递归到分区**（实测父表 `relrowsecurity=t`、子表全为 `f`），新分区的策略由 `internal/partition.EnsureAhead` 建表后补
  · 样板：`internal/module/project/model/locale_model.go`（`project_locales` 是 199 的试点，也是当前**唯一**接了 scope 的表）
- **对外时间默认只到秒（`utils.JSONTime`）**：库里的时间是微秒精度（`timestamptz(6)`，全库 199 列口径一致），但对外 JSON **不该把存储精度透出去** —— Go 的 `time.Time` 默认按 RFC3339Nano 序列化（`2026-09-16T13:57:50.123456+08:00`）：同一秒内的两次写入看起来不同、前端做秒级比较 / 分组要自己截断、每条记录多 7~10 字节（列表接口乘起来很可观），而且协议会跟着存储走。dto 的时间字段一律用 `utils.JSONTime`（可空用 `*JSONTime`）：序列化 RFC3339 **到秒**、零值与 nil 给 `null`（不是 `0001-01-01T00:00:00Z`）、解析比标准库宽松（RFC3339 / `2006-01-02 15:04:05` / `2006-01-02` —— 后两种是后台原生表单与既有客户端在用的）、写库仍走 `time.Time` 保留微秒。service 在 model 与 dto 之间转换：去程 `utils.NewJSONTime` / `utils.NewJSONTimePtr`，回程 `.Time()` / `.TimePtr()`。布局常量收在 `utils.LayoutSecond` / `LayoutDay` / `LayoutJSON`（此前十余处硬编码 `"2006-01-02 15:04:05"`）
- **model 里不要再写 `gorm:"type:timestamp(3)"`**：那是**无时区 + 毫秒**，与实际列型（timestamptz 微秒）不符 —— 已清理 57 处。任何 AutoMigrate 路径会照它建出错的列型；时间列的类型由迁移决定，model 标签不重复声明
- 改列名时注意两类**不会自动跟随**的对象：**触发器 / plpgsql 函数体**（函数体是字符串，RENAME 后仍按旧名解析，迁移 206 修的就是它）与 **seed SQL**（seed 可重复执行，必须同步改；历史迁移 SQL 保持原样）。索引表达式、视图、约束由 PG 自动重写
- 迁移的 `CheckSQL` 里 `?` 由迁移器传入的是**表名**；判定要用的其它值（权限点代码等）必须写进 SQL 字面量，否则判定恒为 0、迁移每次启动都重跑（178 踩过）
- **主键选型按「这个 id 会不会出现在系统边界之外」判**（DB-020 复核结论）：对外实体（`projects` / `pages` / `products` / `blocks` / `themes` / `content_templates` 等有对外接口，或 id 进了 Page Document / 产物元数据 / 导出物的）用 **uuid**；纯内部流水与字典（`page_views`、`build_jobs`、`publication_receipts`、`page_site_slots`、`inventory_change_reasons`、`sys_*` 全系）用 **bigint identity**。**两套并存是设计，不是待消除的不一致** —— 缺判据才是问题；新表按此选型，别为了「统一」把对外实体改成自增（id 一旦可枚举就少一层纵深，与 DB-009 想要的隔离方向相反）。判据只约束**新表**，**存量按现状为准**：`master_data_changes` / `inventory_stock_movements` 是 uuid 存量（后者 id 已进对外列表投影 `MovementRow`），说明「流水必然内部」这个直觉不成立 —— 别拿判据去反推存量
- **主键类型的代价是实测过的，别凭感觉排优劣**（本地 PG 18.6，100 万行同结构同 payload）：插入 `bigint identity` 1.90s / `uuid` v7 2.81s / `uuid` v4 5.42s，主键索引 21MB / 30MB / 38MB，**点查三者无差别**（都在测量噪声内 —— 别拿它当任何一方的论据）。所以 uuid 不是「更好的主键」，而是为「id 不可枚举」付的写放大（v4 随机插入导致 B-tree 页分裂，写放大 2.85 倍）：付它的唯一依据就是上面那一行判据，量级越大的内部表越该用 bigint；对外实体真要用 uuid 就用 v7，实测能追回六成以上代价
- **只增的分区流水表用 UUIDv7**（`inventory_stock_movements` / `master_data_changes`，统一经 `utils.NewTimeOrderedID()`）：写入点集中在索引右端，实测把 v4 的写放大砍掉一半（插入 5.42s → 2.81s、主键索引 38MB → 30MB）。与上一条判据不冲突 —— 判据决定「bigint 还是 uuid」，v7 决定「内部表用哪种形状的 uuid」。两个反作用要记住：**v7 的时间前缀会透露创建时间**，所以对外实体（`projects` / `pages` / `products` / `blocks`）继续用 v4 的 `uuid.NewString()`，别顺手替换；从 v4 切到 v7 后「按 id 排序」会从无序变成等价于创建先后，原先靠 id 排序读创建顺序的写法要显式改用 `create_time`
- 新表选 uuid 时**在应用层生成**（`uuid.NewString()`，见 project / block 的创建路径）：DDL 的 `DEFAULT gen_random_uuid()` 只是兜底 —— gorm 对 string 主键的零值会**显式写入空串**（不像 int 那样交给 identity），依赖 DB 默认值会踩 22P02。内部流水表用 UUIDv7 见下一条（`uuidv7()` 是 PG 18 函数而 CI 是 PG 16，所以一律走应用层）
- **改主键类型时，引用会渗进文档内容**：`blocks.id` 同时存在于 `props.blockId`（root 树任意深度）、`settings.structure.headerBlockId/footerBlockId`、`settings.slots.*` 三处，分布在 10 个 JSONB 列（含 `page_revisions` / `document_snapshots` 历史快照与 `page_artifacts.source_document`）加 `page_dependencies.dependency_key`。改这类 id 之前先用键名把存储点摸全（迁移 209 的注释列了完整清单），否则会静默留下断裂引用

### model 层定位（重要，评审与开发共同遵守）

`model` 是**表访问单元（Repository）**，不是 DDD Domain Model：

- ✅ 允许：本模块表的 CRUD、聚合与**聚合内原子组合**（如 `CreateWithRevision` 在同一事务内写 `pages` + `page_revisions`）；查询条件一律以参数传入，方法内不得写死业务条件
- ❌ 禁止：跨 model 调用、业务规则/决策（谁能删、状态机）、**跨聚合/跨模块事务**
- 跨聚合/跨模块事务必须在 service 层编排：model 暴露 `Transaction()` 透传（或方法接受外部 `*gorm.DB`/`*gorm.Session`），由 service 决定事务边界与回滚
- `DB(ctx)` / `RevisionDB(ctx)` 等裸 gorm 句柄是 model 的**内部实现细节，只允许被本 model 的仓储方法消费**；service 禁止调用它们拼接查询
- service 对持久化的唯一入口是 model 的具名方法；新增查询需求 = 给 model 加方法，而不是在 service 里写 `.Where().Create()`
- 评审拦截项：`internal/module/*/service` 中命中 `\.DB(ctx)` 或 `\.RevisionDB(ctx)` 即打回（含先存变量的 `query := x.DB(ctx)` 写法；仅 admin 豁免，见下）
- `contract/` 只放模块对外接口；`service` 依赖其他模块能力时直接引用对方 `contract`

**admin 豁免条款**：`admin` 为管理面 CRUD 大模块（六领域合并、同包直调），service 层经 `DB(ctx)` 直查**明文豁免**。豁免边界：仅限 admin 模块、仅限本模块表、跨表事务仍须 `Transaction()` 编排、简单 CRUD 之外的业务查询仍走 model 方法。新增模块一律禁止直查。

## 测试

- 默认跑现有测试，不新增额外测试框架
- 接口优先维护 feature 链路测试（`public/test/`，真实 PostgreSQL 环境），复杂逻辑补 unit
- 测试基建已迁移到本地 PostgreSQL（sqlite 驱动已移除）；PG/Redis 不可用时相关用例 `t.Skip`
- **全量测试可以并发：`make test` 走 `-p $(nproc)`**（本机 16 核实测 65s；串行 966s）。2026-09 之前只能 `-p 1`，
  两道拦路虎都已拆掉：
  · `pg_trgm` 是**库级唯一**的扩展，装在哪个 schema 只有 search_path 含它的连接才解析得到
    `gin_trgm_ops` —— 过去串行时靠「测试结束 DROP SCHEMA 把扩展一并删掉、下个 schema 重新装」
    侥幸通过，一并行就互相踩（后来者 `CREATE EXTENSION IF NOT EXISTS` 静默跳过，随后整条迁移
    报 operator class does not exist）。现在它固定装在有专用 schema **`ext_shared`**（迁移 210
    负责既有库搬迁），迁移器 `Run` 统一把该 schema 补进 search_path —— 任何调用方都不会再踩。
  · **测试不再为每个用例重跑全部迁移**（现 216 条，见下面「模板库」那条），并发时的锁表压力随之消失。
    这正是当初 `-p 8` 会随机几个包 `out of shared memory` 的原因（失败包每次都不同，别误读成
    「某个包坏了」）。要再往上提并发，先确认 `max_locks_per_transaction`（默认 64）够用。
- **按对象名查 catalog 的 SQL 必须限定 `current_schema()`**：`pg_class` / `pg_indexes` 是**全库**的，
  并发（或库里残留了旧 schema）时同名表 / 索引会被一并查到。迁移判定与测试断言各踩过一次：
  167 的判定漏了 `schemaname` 会让整条迁移被静默跳过（该 schema 的 trgm 索引全缺），
  `p7_index_audit_test` 则把 35 个 schema 的同名索引键列拼成了一份。用 `'表名'::regclass` /
  `to_regclass` 锚定对象是安全的（按 search_path 解析），按 `relname` / `indexname` 过滤才需要显式限定。
- **迁移必须在单连接上跑**（`Run` 用 `db.Connection`）：`pg_advisory_lock` 是会话级的，而
  `db.Raw` / `db.Exec` 每次都从连接池取连接 —— 换连接会让 `unlock` 落到别的连接上（锁永不释放，
  几十个测试进程一起泄漏直接 `out of shared memory`），那把锁也根本保护不到迁移语句本身。
  锁键按 `current_database() || current_schema()` 派生：生产多实例同库同 schema 仍然互斥（原意），
  测试各用隔离 schema 时不再互相排队。
- **测试的表结构一律来自生产迁移**，两个 helper 分工明确，别混用：
  · `support.NewMigratedPGTestDB(t)` —— 需要真实 schema 的用例（feature / 链路 / 大部分 unit）。
    它复制一份**模板库**（`CREATE DATABASE ... TEMPLATE wp_test_tpl_<指纹>`，实测约 65ms），
    模板库由 `migrations.Run` 建成：结构与生产逐字节一致，只是不再为每个用例重付那 1.1s
    （admin 一个包 116 个用例过去就是 128s，现在 46s）。模板名带 `migrations.Fingerprint()`：
    迁移一改就换新名字重建，绝不会拿过期结构跑测试；旧模板在建模板时顺手清理。
  · `support.NewPGTestDB(t)` —— 建**空库**，给自己建表（`AutoMigrate` / 手抄 DDL）或故意构造旧
    schema 的用例用。**塞给它们完整生产结构反而会坏**：实测 `AutoMigrate` 会去对齐一个名字不同的
    约束而报 42704，手抄的最小 schema 没有外键、换成生产结构后 INSERT 立刻撞 FK。
  · 禁止手抄 `CREATE TABLE` 去伪造「看起来像生产」的表：会与生产静默分叉（`publication` 用例手抄的
    `publication_receipts` 停在 `uuid` + `created_at`，与生产迁移后的 `bigint` + `create_time` 脱节；
    同类手抄分布在 7 个 feature 目录）。测试只额外补**真实父行**（`support.SeedProjectRow` 等）。
- 组件测试在组件包内（`internal/builder/components/*`），含确定性构建与 fuzz 测试
- 并发敏感代码跑 `go test -race`
- **交互改动的验证清单**：涉及输入的改动，逐种输入方式各测一遍 —— 鼠标拖拽 / 滚轮与触摸板 /
  触屏滑动 / 键盘 / 点击。程序化调用（直接设状态、合成单一事件）**覆盖不到**「用鼠标滚一下」
  「用触屏滑一下」这类真实路径；本项目的多个交互缺陷（堆叠轮播无滚轮、纵向 `touch-action`
  写死）都只在真实输入下才暴露。产物层面的快速核对：用 CDP 发真实 `mouseWheel` /
  `PointerEvent(pointerType='touch')`，配合「先聚焦容器再按方向键」验证键盘路径。
- **所有组件必须适配多端**（硬规则，2026-09 确立）：新建或修改组件时，产出必须在
  桌面 / 平板 / 手机上都能正确渲染，在鼠标 / 滚轮与触摸板 / 触屏 / 键盘下都能操作。
  · **宽度不写死**：写 `min(100%, <设计宽度>)`，不要只写 `<设计宽度>`；
  · **绝对值带上限**：编译期算出的半径 / 位移 / 尺寸用 `min()` / `clamp()` 按视口封顶，
    且上限要让**元素自身尺寸**参与计算（`calc((100vw - <元素宽>) / 2 - 边距)`）——
    用 `40vw` 这类经验比例，在「大卡片 + 窄屏」的极端组合下照样溢出；
  · **触屏是独立环境**：`AddHover` 的规则包在 `@media (hover: hover)` 里，触屏上**根本不输出**。
    依赖 `:hover` 的任何形态都必须用 `CSSBuckets.AddHoverNone` 给出触屏等价形态，
    否则手机端该功能等于不存在；按压反馈用 `AddActive`（不带媒体查询）；
  · **验收**：三种视口（1440 / 768 / 375）各看一次、四种输入各操作一次；
    窄视口下在控制台确认 `document.documentElement.scrollWidth === clientWidth`，
    且不存在 `right` 越界的元素。完整规范见 `docs/02-C0-component-base-spec.md` §6.9。
- **做覆盖全部能力的实例页**是性价比最高的一次集成验证：单组件测试与四五个区块的小案例页
  都看不出模板截断、盒模型偏移这类缺陷，只有把全部模式铺在一个长页面里才暴露。
- **动画 / 观察者 / 时间线类改动**：必须读到「**值在变化**」（`transform` / `opacity` / `filter`
  在不同滚动位置或时刻下确实不同），**只读属性名、时间线名或计算值不算验证**。同时要保证
  **触发条件真的成立**再下结论：
  · `IntersectionObserver` 的 `root` 元素必须自身在页面视口内才会触发 —— root 在视口外时
    连初始回调都没有；
  · `animation-timeline: view()` 的元素必须真的处于滚动容器可视区内（且该场景下 view() 对
    **内嵌滚动容器**实测不驱动动画）；
  · 观察者回调是**异步**的，必须等一轮再读结果，不能在同一次求值里读。
  本项目已多次因「核对了自己写下的配置、没核对系统实际做的事」而误判通过
  （`:hover` 未真触发 / 长页面才暴露模板截断 / 动画值恒定不变 / observer 未触发）。
- **无头 + 自动化环境不出渲染帧**：`IntersectionObserver` 不回调、`requestAnimationFrame`
  不执行、`scroll` 事件不派发 —— 三者同源（都等下一次渲染帧），页面脚本创建的实例全都
  静默失效，容易误判成「实现有问题」。
  · **对策（验证用）**：操作之后调用一次**截图**（`ego_screenshot`，强制产生渲染帧），
    再读结果；或把手动创建的同参数实例与页面实例对比，能直接区分「环境问题」与「实现问题」。
  · **对策（实现用）**：增强逻辑优先用**同步几何计算 + `setTimeout` 节流**，而不是
    `rAF` / `IO` —— 前者在任何环境都可断言，后者只在真实浏览器里可靠。

## Git 与工具约定

- 每次 commit 用中文注明改动文件路径和修改内容简述
- **删除能力时**：grep 该能力名称（表名、权限点、枚举、注释关键词）并同步清理相关注释与文档，避免 CQ-024 类过时说明（例：121 删库存缓存后仍引用 `stock_total` / `CacheSync`）
- 文档更新与代码提交分开；修改规则文件（本文件及子目录 CLAUDE.md）前先重新读取，桌面端可能并发改写
- GitHub 操作（仓库/Issue/PR/Release）优先用 `gh` CLI；Go 项目发版优先 GoReleaser（`goreleaser`）
- 语言运行时版本由 vfox 管理（`~/.vfox`），禁止 Homebrew/apt/系统包安装运行时；Node.js 依赖优先 pnpm，Python 用 uv

## 文档导航

- `docs/01-overview.md` — 概览、边界、冻结边界速查
- `docs/13-module-inventory.md` — 模块实现清单（各模块完整职责与不变量；AGENTS.md 的「模块现状」只留一句话边界）
- `docs/03-pipeline.md` — 发布管线
- `docs/05-implementation-plan.md` — 阶段计划
- `docs/06-plugin-system.md` — 插件体系规范（三级能力分层/双轨制/表扩展/样式引擎）
- `docs/api-stability.md` — HTTP API 稳定性分级（stable / experimental / internal）
- `docs/schema-snapshot.md` — 当前 schema 查法与枚举约束（DDL vs Go 注册表）
- `docs/06-A-plugin-ecosystem-roadmap.md` — 插件生态路线图（本体收口/SEO 基建/首批插件清单/商品重轨+壳）
- `docs/06-B-dual-track-adr.md` — ADR：双轨制决策固化（分轨/admin 契约双口/商品定位/正文双视图单真源/SEO 分工）
- `docs/02-*` — 组件规格；`docs/03-A-workbench.md` — 工作台
- `docs/04-B-dynamic-development-guide.md` — 动态能力开发指南（How-To：静态绑定/Fragment/Client Enhancement 三路径）
- `docs/02-E-seo-scoring-engine.md` — SEO 评分引擎（rubric 权重卡/Yoast 复用策略/自研计算器/执行计划）
- `docs/agents/` — Issue 追踪、Triage 标签、领域术语
- `internal/module/CLAUDE.md` — 模块开发规范；`pkg/CLAUDE.md` — pkg 组件规范
