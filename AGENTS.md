# AGENTS.md

go_wp = CMS + Visual Website Builder + Static Publishing Engine。本文件是全仓唯一规则源：判据、边界、流程。
项目背景见 README.md / DESIGN.md。开发阶段不兼容旧代码：有问题直接重构。

## 语言
回复、分析、计划、报告、注释一律简体中文；代码标识符、命令、上游数据保留原文。

## 常用命令
```bash
go run cmd/main.go
go build -o app ./cmd        # 生产构建用包路径，且在 git 工作区内执行
go test ./...                 # 全量（可 -race）
go vet ./...
bash scripts/check-all.sh     # 门禁全集（--with-db 含需要库的那组）
```

## 查证（不确定就先查，禁止凭记忆断言）

任何「某个库/框架能不能做 X」「它的签名/行为是什么」的判断，三档依次走，命中即停：

1. **本机源码与文档**：`$GOMODCACHE/<mod>@<ver>/`（README、`docs/`、`_test.go` 里的用法）—— 版本与 `go.mod` 对齐，最可信。
2. **context7**：本地未命中时按 `context7.json` 的映射查（`jet → /cloudykit/jet`），接口 `https://context7.com/api/v1/<owner>/<repo>?topic=..&tokens=..&type=txt`；查到新事实回填该文件。
3. **最小可跑样例**：文档说不清时写 10 行 `go run` 实测，把输出贴进结论。

判据：结论里出现「某库不支持 / 做不到 / 只能这样」而手里没有文档路径或样例输出 → 判失败。

## 分层判据（全仓唯一一处）

**Go = 后端（internal/** 除 templates、pkg/**、cmd/**）；Jet 模板 + HTML/JS = 前端（internal/templates/**、public/static/**）。**

### 业务模块（internal/module/<mod>/）

| 目录 | 只准 | 一律禁止 |
|---|---|---|
| dto | 请求绑定与校验（tag、Validate、FormBinder） | 展示字段（Title/Menu/Label/Tone/Text/Error/LoadFailed/HTTPStatus/Pagination）、gin.H、import service/model |
| service | CRUD、list、数据库聚合、多表拼装成对象、事务边界 | 取词、格式化（time/money/pagination/拼句）、*PageResp、import i18n/text/templates/adminui |
| model | 本模块表的 CRUD、聚合内原子组合 | 业务规则、跨聚合事务、别人的表名、import service |
| contract | 接口 + 不可变 DTO | import 别人的 service/model、反向依赖 builder/core |
| enums | 枚举、错误 key + 中文兜底 + 状态码、界面 key | 取词、拼句（兜底是原文，不是成品调用） |
| inbound/http | binding.* → 调 service → return（JSON 出口） | 第二段业务逻辑、拼 map/串、取词、adminui.Render*、模板名、编译归一 |
| inbound/mcp | 工具定义与参数校验 | 重抄 dto 字段表、直连 model |
| outbound | 外部系统适配 | 业务规则 |

### 平台与横切（internal/ 其余）

| 包 | 只准 | 一律禁止 |
|---|---|---|
| routers | 装配、路由注册、页面出口（adminui.Page/Bare） | 业务逻辑、SQL |
| adminui | 后台渲染出口（Prepare / Page / Bare / 错误出口转发） | 业务语义、数据派生 |
| text | 界面文案线（注入 t/tf、InternalMsg、FactOf） | 业务语义、读模型 |
| builder/** | 构建期编译与组件 | 控制面 DB 写、访客请求处理 |
| pipeline | 发布内核（编译→产物→激活） | 反向 import build/artifact 实现 |
| permission middleware mcp seo shell siteurl uispec backref retention partition | 各自横切能力 | 业务规则、跨模块写 |
| architecture | 门禁断言 | 被生产代码 import |
| templates/** | 前端层（见 internal/templates/CLAUDE.md） | 任何 Go 代码 |

### 基础设施（pkg/）

| 包 | 只准 | 一律禁止 |
|---|---|---|
| binding | 绑定唯一入口（JSON/Form/Query/Field/ListQuery/CSRFToken） | 业务校验 |
| facing | 错误事实（key + 兜底 + 状态码） | 取词、日志 |
| response | JSON 出口取词与状态码 | 业务语义 |
| view | 视图结果接口（PageStatus/PageNoStore） | 渲染实现 |
| i18n logger database cache rls jsonx utils money pagination pathkit safepath | 基础设施 | import internal/** |
| auth casbin crypto captcha mailer queue upload validate sitehttps sitetz datarule reqparams backpath bulkids enums | 能力包（provider 型带 provider/ 实现） | 业务语义、import internal/** |

### 前端层与入口

| 位置 | 只准 | 一律禁止 |
|---|---|---|
| internal/templates/** | 标题、菜单、徽标分档、空态、时间/金额显示、分页、取词、抽屉初值 | 业务规则的第二份实现、金额求和、Go 代码 |
| public/static/** | JS/CSS（HTMX 基座、ui/json_write.js） | 业务计算 |
| cmd/** | 进程入口、装配、迁移命令 | 业务逻辑 |
| public/migrations | 版本化幂等 SQL + seed 注册 | Go 业务逻辑 |
| public/test/** | feature 链路测试 | 被生产代码 import |

- 依赖方向单向：cmd → routers → module/* → pkg/*；pkg 不 import internal；contract 不被越级穿透。
- 两种契约：JSON {code,message,data}（非页面请求的唯一响应）与「现成数据对象」（页面）。
- 一屏 = 若干现成数据对象；后端不合并、不派生、不为模板造第二种形状。
- 判据是形状不是字面量：新增处理器先问「这段计算属于哪层」，而不是等门禁逐个堵。

## 架构不变量（注释以「AGENTS.md 不变量 N」引用）

1. **控制面与访问面分离**：访客请求不查库、不执行 Jet、不解释 AST；URL→文件由 PublicationStore 状态决定。
   例外仅两处：POST /analytics/collect（只写一条、响应恒 204）与 AccessGuard 只读访客会话（不查 PG、不写、不续期、fail closed）。
2. **两条发布路径共享同一管线**：Page 与 PresentationInstance 都走 Publish Compiler → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃**，ContentTemplate 每次构建参与。
4. **Binding 不是 Query DSL**：Document 只存白名单 FieldBinding/CollectionSource/MediaBinding。
5. **确定性构建**：同一输入产生相同 Artifact 字节。
6. **冻结边界不可越权**：模块、组件、协议各有负责与禁止范围（module/CLAUDE.md 与 templates/CLAUDE.md）。
7. **构建期数据源依赖方向**：共享形状在 internal/builder/source；契约包不得反向 import builder/core（scripts/check-contract-deps.sh 判传递闭包）。

## 数据库

- 表只被一个模块读写；跨模块走 contract。model 不声明列型（internal/architecture/model_gorm_tag_test.go）。
- 时间列：新表用 create_time/update_time；业务时刻沿用 *_at；timestamptz + time.Time；对外 JSON 到秒（utils.JSONTime）。软删除统一 deleted_at。
- 迁移：版本化幂等 SQL + register.go；seed 门槛必须逐条枚举本批对象（禁用 LIKE/全库总量）；改列名同步触发器与 seed SQL；新 SQL 上线前 PREPARE 校验；Migrations 台账先跑、Seeds 后跑。
- 裸 SQL 只减不增（raw_sql_boundary_test.go）；唯一 JSON 入口 pkg/jsonx（json_import_boundary_test.go）。
- 主键：对外实体 uuid（应用层生成）；纯内部流水与字典 bigint identity；只增分区流水用 UUIDv7。
- 权限点：一条 API 路由一个独立权限点（常量进 internal/permission/codes.go，注册处声明），页面路由可复用。
- RLS 顺序：先包 pkg/rls.InProjectScope → 再把连接角色换成非超级 → 最后 require_rls_role=true。
- 聚合口径：同一屏上的两个数字若回答同一批行，筛选条件抽成共享常量；多序列共用横轴时聚合粒度必须一致且由调用方统一传；桶键读回来必须还是同一时刻（AT TIME ZONE 'UTC' 写两遍，::date 不用）。

## 写操作

一次用户可感知的写操作涉及两处以上持久化写入时，必须同一事务；事务边界由 service 决定，句柄从 model 下传（…Tx 方法）。
读-改-写必须行锁或原子 SQL。跨库/外部系统的补偿必须幂等、留痕、可重放并写明理由。
冲突一律打回（列出可定位的数据），不自动加后缀、不静默合并。
**批量要么全成要么全败**：不推算 skipped、不返回逐条回执；拒绝时把 id / 名称 / 原因放 data.items。
门禁：public/test/architecture/tx_boundary_scan_test.go（启发式，绿灯不等于没问题）。

## 响应与文案

| 格 | 何时 | 形态 |
|---|---|---|
| JSON | 所有写动作、所有非页面请求、页面 GET 失败 | {code,message,data} + 真实状态码 |
| 页面视图 | 页面 GET 成功 | 控制器 return 视图, nil；router 用 adminui.Page/Bare 渲染 Jet |
| 字节流 | 构建器预览、runtime fragment、下载、SSE | 直接写 body 后 return nil, response.ErrHandled |

- 状态码在错误声明处给（facing.WithStatus），出口只调 response.ErrorStatusAuto：400 参数 / 401 未登录 / 403 无权限 / 404 不存在 / 409 冲突与版本 / 500 其余。
- 成功写动作只回 JSON；导航由前端 data-write-nav 决定，后端不发 302 / HX-Redirect。
- 文案两条链：响应走 pkg/response（MessageOf → facing.FactOf → i18n），界面走模板的 t/tf。业务层只交事实（key + 中文兜底）。
- 兜底跟声明处走：模块 enums 的 errFallbacks + Err/ErrDetail；明细用半角 ": "；模块内不许自带白名单表。
- 界面 key 域：admin. / shell. / user. / workbench. / site. / msg_ 前缀或 Msg*；错误 key（*.err.*）不得当界面文案。

## 测试

- 分层：模块内 unit 测纯逻辑；接口链路走 public/test/<mod>/feature（真实 PG）；组件测试在组件包内。
- 表结构一律来自生产迁移（support.NewMigratedPGTestDB）；禁止手抄 CREATE TABLE。
- 断言等式而非各自的值；错误断言状态码 + 对外文案且不含内部原文；页面断言真实 HTML 片段。
- 计数与命名不是判据：rg -c 只是筛查起点，结论前回读语义；门禁绿不等于没问题。
- PG/Redis 不可用时 t.Skip；并发敏感代码加 -race。

## 流程

1. **规划先行**：一次改动先写「层归属表」（每处改动属于哪层、该层允许什么）；判据不齐不动代码。
2. **一个对话 = 一个任务单元 = 一个 git worktree**；开工建、收尾合并后删。
3. **批次**：一屏/一域一个提交；先删旧形状，再建新形状；每批只放该批改动。
4. **验收四件**：go build 干净 · 相关包 + feature 测试绿 · bash scripts/check-all.sh 绿 · 门禁台账减条。
5. **收口**：反查过期注释、重复实现、形状漂移；做完的区域比动手前更少。改到的文件顺手清掉指向已删文档的指针（docs_pointer_test.go 只减不增），并同步下调预算。
6. **提交**：中文说明改动文件与内容；精确暂存（逐文件 add），暂存里出现不认识的文件先查证。
7. 改规则文件前重新读取（桌面端可能并发改写）；文档更新与代码提交分开。

## 门禁（会变红的判据）

internal/architecture/：controller_shape_test.go（文件名/绑定/方法行数/渲染出口/UI 线 import/参数读取/编译归一）、text_domain_test.go（界面词条域）、html_boundary_test.go（后端无 HTML 字面量）、template_logic_test.go（模板不算金额）、module_boundary_test.go、raw_sql_boundary_test.go、json_import_boundary_test.go、model_gorm_tag_test.go。
scripts/：check-all.sh、check-no-internal-error-leak.sh、check-service-db-boundary.sh、check-contract-deps.sh、check-controller-i18n-literals.sh、check-permission-gaps.sh、check-i18n-*、check-empty-state-table-head.sh、check-shell-migration.sh。

## 指针

internal/module/CLAUDE.md（模块目录与分层细则）· internal/templates/CLAUDE.md（模板契约、取词、多端）· pkg/CLAUDE.md（基础设施）· public/CLAUDE.md（迁移与静态资源）· public/test/CLAUDE.md（feature 测试）。