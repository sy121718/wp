# 新增一个业务模块

以「照着真实代码路径走一遍」为准。规则条文在 [`../../internal/module/CLAUDE.md`](../../internal/module/CLAUDE.md)，这里给的是操作顺序与落点。现有模块可以直接当模板：契约三段看 `internal/module/order/contract/`，路由自装配看 `internal/module/order/inbound/http/order_router.go`。

先把契约定死，再写实现 —— 顺序反过来会得到「先把表查出来、再倒推接口」的模块，跨模块依赖会立刻失控。

## 1. 建目录骨架

```text
internal/module/<模块>/
  contract/         必选，跨模块唯一入口
  inbound/http/     必选，路由与 handler
  outbound/         可选，需要「翻译」时才建子包
  service/          必选
  model/            必选
  dto/              必选，跨模块形状
  enums/            必选（错误码与文案归口）
```

文件命名：`<模块>_service.go`、`<模块>_handle.go`、`<模块>_router.go`、`<模块>_model.go`、`<模块>_req.go`、`<模块>_resp.go`。import 别名用 `<模块名小写>dto` / `model` / `contract` / `enums`（例如 `ordermodel`、`productcontract`）。

## 2. 写 contract

`contract/` 只放三类东西：

1. **对外能力**：`XxxService` 及收窄子接口（其他模块能调什么）。
2. **跨模块形状**：本模块 dto 的重导出 —— `type X = xdto.X`，不是复制一份结构。
3. **索要的端口**：`XxxPort` / `XxxReader` / `XxxSource` / `XxxResolver` / `XxxEnqueuer` 一类，由**对方**实现并放它们的 `outbound/<本模块名>/`，在那里做编译期断言。

契约文件拆分的唯一理由是「它是另一条契约」（收窄端口、构建期数据源、能力域各算一条）；不到 200 行又不属于这三类就留在主契约文件里，不要为了「看起来整齐」拆。

样板：`internal/module/order/contract/order_service.go` 与 `internal/module/order/contract/data_source.go`。

## 3. 写 model / service / enums

- **model**：`Entity` + `NewXxxModel(db)` + `DB(ctx)`（`m.db.WithContext(ctx).Model(&Entity{})`）+ 具名查询方法。条件、分页、排序按参数传入；不放业务规则。
- **service**：`xxx_service.go` 只放 `Service` / `NewService`；用例拆到 `xxx_<action>.go`。Service 只持本模块 model 与契约接口，**不持 `*gorm.DB`**；两处及以上持久化写入必须在同一事务里（要落在同一事务的写路径加 `…Tx` 变体，句柄由 service 透传）。
- **enums**：该模块全部对外文案与错误码归口在这里；handler 的文案一律从 enums 取，不写字面量。

判据：`service` 里出现 `.Table()` / `.Model()` 切到别的模块的表，或出现裸 `DB(ctx)` 查询 —— 前者是设计错误，后者会被 `scripts/check-service-db-boundary.sh` 拦下（见 [`gates.md`](gates.md)）。

## 4. 写 inbound/http 与路由自装配

`router.go` 是模块的唯一装配入口：自己创建 model 与 service、拿 `*permission.RouteGroup`、注册路由、返回契约。handler 只做三件事：绑定参数、调 service、输出响应。批量操作必须用 `shell.BulkIDs(c)` 取 id（去空白、去重、有数量上限），逐条走单条路径，单条失败不中断整批，结论按「成功 N / 跳过 M」回带。

```go
func SetupXxxRoutes(rg *permission.RouteGroup, db *gorm.DB, /* 依赖的契约 */) xxxcontract.XxxService {
    svc := xxxservice.NewService(xxxmodel.NewXxxModel(db), /* 收窄端口 */)
    h := NewHandle(svc)
    g := rg.Group("/xxx")
    g.GET("/list", permission.XxxList, h.List)
    g.POST("/create", permission.XxxCreate, h.Create)
    return svc
}
```

路由动词只用 `GET` 与 `POST`。后台页面（`/admin/*`）由装配层传入的页面组注册，写动作复用模块 API 的权限点 —— 用 `builtin.CasbinMiddlewareForPath("/api/xxx/create")` 声明，权限点路径一个字符都不能改。样板见 `internal/module/order/inbound/http/order_router.go`。

## 5. 装配接线

三处：

1. `internal/routers/assembly.go`（或 `assembly_publish.go`）：在 `SetupRoutes` 的线性顺序里加一次调用。**注意顺序** —— 跨模块契约由被依赖模块先装配、再把返回值传给依赖方：

```go
projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db, a.sysConfigDict)
blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
```

需要跨段共享的实例挂到 `assembly` 结构体字段上（该结构体字段**只增不减**）。

2. `internal/routers/wiring.go`：如果新模块对外提供「装配期注入的可空端口」，在 `wiringManifest` 里加一条 `wiringEntry`，选对 `wiringKind` 并写清 `Consequence`（未注入时用户/运维看到什么）。端口名在 `routes.go` 里用常量 `marks.mark(...)` 标记，提供方用 `RequireWiringPort(port, ok)` 断言。三类 Kind 的语义见 [`architecture.md`](architecture.md) 第 6 节。

3. 数据权限域：域声明属于**拥有该表**的模块。样板在 `internal/module/admin/inbound/http/datarule_bootstrap.go`（`RegisterDomain` → `SetProvider` → `RegisterPluginWithDB`）。注册必须发生在**注册路由之前**；声明写错要在装配期直接失败 —— 域没注册上的后果是行级过滤整体静默失效（fail-open），不能等到「规则存进去了却一条都没拦住」才发现。白名单来自实体字段 tag：

```go
DeptID uint64 `gorm:"column:dept_id" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
```

## 6. 权限点

新增接口要在 `internal/permission/codes.go` 加**两处**：

1. 常量块里加一条：`XxxCreate Perm = "xxx:create"`（命名规则 `模块:动作`）。
2. 文件末尾的 `specs` 表里加对应条目：`XxxCreate: {module: "xxx", name: "新建…"}`。

少了第二处，装配期 `permission.Declare` 会 panic（「权限点未登记」）—— 这是刻意的：拼错的权限点不可能静默上线。**不要手工改已有常量的字符串**：它必须与库中既有 `permission_code` 逐字一致，改名等于新建权限点，旧的角色/用户授权会失效。

然后在路由注册处把常量作为 `RouteGroup.GET` / `POST` 的第二参数声明。启动期 `permission.SyncToDB` 幂等 upsert 入库，**不需要写 seed 迁移**；显式豁免用 `permission.Exempt`，它会打进启动日志（豁免必须是看得见的选择）。

## 7. 迁移与 seed

- 新建 `public/migrations/NNN_名称.sql`（编号取现有最大值之后的号，当前最大为 `510`）。整个目录经 `//go:embed *.sql` 嵌入。
- 在对应的 `public/migrations/register_*.go` 里注册：`register(Migration{Version: "...", TableName: "...", SQL: mustSQL("NNN_名称.sql")})`；seed 用 `registerSeed(Seed{Version, TableName, ConditionSQL, SQL})`。
- `ConditionSQL` 的判据必须**枚举本批对象**（「这批权限点/菜单是否已存在」），不要写成「这个模块下有没有任何权限点」这类宽条件 —— `RunSeeds` 在所有结构迁移之后才跑，宽条件在新库上会已被后续迁移弄成真，整支 seed 被静默跳过（这个坑踩过：全新库缺 29 条基础权限点，连超管都 403）。
- 写错会被 `TestEmbeddedSQLFilesAllRegistered` 抓住（磁盘 `.sql` 与注册台账双向一一对应）。
- schema 规则（主键、时间列、软删除、RLS、索引）见 [`../rules/database.md`](../rules/database.md) 与 [`../AGENTS.md`](../../AGENTS.md)。

## 8. 模板与静态资产

- 后台页面模板放 `internal/templates/admin/<模块>/`，Jet 语法约定见 [`../../internal/templates/CLAUDE.md`](../../internal/templates/CLAUDE.md)（**可选键必须 `isset`**，缺键会中断整页渲染）。
- 访客侧动态片段模板放 `internal/templates/fragments/`，注意那里的 data 是 struct，只能写 `{{ .CSRFToken }}` 一类字段访问。
- 组件实现与就近资产放 `internal/builder/components/<组件>/`，经 `go:embed` 打进二进制。
- 后台文案禁止硬编码中文，走词条；`scripts/check-i18n-coverage.sh` 与 `scripts/check-i18n-keys-seeded.sh` 分别守「模板里不新增硬编码」与「用到的 key 真的被 seed」。

## 9. 测试落点

| 层次 | 位置 | 内容 |
|---|---|---|
| 模块内单测 | `internal/module/<模块>/**/*_test.go` | 纯逻辑，不碰库、不走装配 |
| 接口链路 | `public/test/<模块>/feature/` | 需要 HTTP、事务、完整装配 |
| 模块规则 | `public/test/<模块>/unit/` | 模块内的规则性断言 |
| 架构红线 | `public/test/architecture/` | 边界、事务、model 标签的静态扫描 |

命名：文件 `<模块>_<场景>_test.go`，函数 `Test<模块><场景><结果>`。**表结构一律来自生产迁移**（`public/test/support/migrated_db.go` 的 `NewMigratedPGTestDB`），禁止手抄 `CREATE TABLE`；空库场景用 `public/test/support/pgtest.go` 的 `NewPGTestDB`。详见 [`../../public/test/CLAUDE.md`](../../public/test/CLAUDE.md)。

## 10. 合并前自查

- [ ] `gofmt -w` 改动文件；`go build ./...`、`go vet ./...` 通过
- [ ] `make check` 通过（无外部依赖的那 10 个门禁）
- [ ] `make check-db` 通过（权限点与库一致）
- [ ] 触及 `pkg/` 或核心五域时 `make test-race` 通过
- [ ] 新增权限点后重启进程再验接口（Casbin 策略启动时载入内存，不自动重载）
- [ ] 触及按 `project_id` 取数的路径时，`public/test/rls/` 用例通过
- [ ] 本模块的职责与不变量同步进 [`../13-module-inventory.md`](../13-module-inventory.md)
- [ ] 新增门禁脚本已接进 `scripts/check-all.sh` 的 `PLAIN_SCRIPTS` 或 `DB_SCRIPTS`
