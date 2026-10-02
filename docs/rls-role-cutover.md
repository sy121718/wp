# RLS 角色切换与回滚（DB-009 / DB-04）

> 适用范围：把应用的业务连接从管理角色（超级用户）切到非超级的次级管理员角色
> （默认 `go_wp_app`），让迁移 199 / 215 铺下的工程隔离策略真正生效。
> 配套工具：`scripts/rls-role-setup.sh`（建角色 + 授权 + 现场验证）、
> `pkg/rls`（作用域原语 + 连接身份探针）、`database.require_rls_role`（启动门禁）。

## 1. 为什么必须切角色

迁移 215 给带 `project_id` 的对象装了 `ROW LEVEL SECURITY` + `FORCE`
（覆盖数量以 `pkg/rls` 的连接身份探针读数 `Identity.RLSTables` 为准 —— 迁移仍在追加对象，
文档里不写死数字），
策略谓词读会话变量 `app.project_id`（未设置即行不可见，fail closed）。但
**PostgreSQL 的超级用户总是绕过 RLS** —— `FORCE` 约束的是表属主，约束不了
superuser / `BYPASSRLS` 角色。应用原先连的是 `root`（`rolsuper=t`），
所以策略一行都挡不住。

实测（本地库，`project_locales` 有 3 行、分属 3 个工程）：

- `root` 未设 `app.project_id` → 读出 3 行（策略完全无效）
- `go_wp_app` 未设 `app.project_id` → 读出 0 行（fail closed）
- `go_wp_app` 事务内设 `app.project_id=P1` → 本工程 1 行；同作用域下查 P2 → 0 行

## 2. 两角色分工

| 用途 | 角色 | 需要的能力 |
|---|---|---|
| 业务连接（`database.user`） | `go_wp_app`（NOSUPERUSER NOBYPASSRLS） | `USAGE` on schema、DML、序列 `USAGE/SELECT` |
| 迁移 / 运维（`-migrate-only`、脚本） | 管理连接（超级用户） | DDL、`BYPASSRLS`（迁移要建表、seed 要写内置行） |

两角色分工是**结论不是临时妥协**：`FORCE` 让属主也受 `WITH CHECK` 约束，
seed 与 DDL 若跑在业务连接上会被策略挡住或需要在每个迁移里手工设作用域。
角色**不由迁移创建**（migration ledger 里没有角色这一概念，运维动作走脚本）。

## 3. 顺序不变量（反了会静默丢数据）

**先给各模块的读写路径包上 `pkg/rls.InProjectScope`，再切 `database.user`。**

反过来时，没包 scope 的路径会**返回 0 行而不报错**（fail closed 是策略的设计目标），
表现为「功能突然查不到数据」且日志里没有任何错误 —— 这是本项目对 DB-009 定下的硬顺序，
`public/test/rls/rls_scope_test.go` 专门钉住它。

## 4. 切换步骤

### 4.1 建角色并授权（幂等，可重复执行）

```bash
# 口令不进命令行：优先环境变量，其次 ~/.config/go_wp/rls-role.env（权限 600）
export GOWP_RLS_ROLE_PASSWORD="$(openssl rand -hex 16)"
bash scripts/rls-role-setup.sh go_wp_app
```

脚本做四件事：建 / 改角色（显式 NOSUPERUSER NOBYPASSRLS）、授权（含
`ALTER DEFAULT PRIVILEGES`，将来新建的表自动授权）、核对角色属性、
再用该角色连库验证「未设变量 0 行 / 设了变量只见本工程 / 别的工程不可见」。
含口令的 SQL 只经 stdin 交给 psql，因此不会出现在 `ps` 的 argv 里。

### 4.2 盘点并补齐工程作用域

这一步属于 DB-05（各模块 model 接线），本文件只给可重复执行的检索命令（见 §7）。
判断标准不是「包里有 import」，而是**每一条访问带 `project_id` 表的路径**都在
`rls.InProjectScope` / `rls.ScopeTx` 里执行。

### 4.3 关掉「启动时迁移」（业务角色没有 DDL 权限）

```yaml
database:
  run_migrations: false
```

**为什么必须关**：非超级应用角色不是任何表的属主、在 `public` schema 上也没有 CREATE
权限（实测 `has_schema_privilege('go_wp_app','public','CREATE') = false`），而 `migrations`
是 Critical 组件、启动链上无条件跑 DDL。不关掉这一项，应用在启动阶段就会被
`permission denied for schema public` 直接打回 —— 连 `CREATE TABLE IF NOT EXISTS` 都过不去。

关掉的范围只有两处：`migrations` 组件与两处 `migrations.RunSeeds`
（`cmd/main.go`、`internal/routers/assembly.go`）。装配末尾的 `permission.SyncToDB`
（权限点幂等 upsert）**不是 seed**，每次启动照跑 —— 权限点缺了会全站 403，不能一起关。

### 4.4 两步流程：先管理连接迁移，再用业务角色起服务

**第一步 · 管理连接迁移**（迁移走管理连接，业务流量还没切过来）：

```bash
GOWP_DATABASE_USER=root GOWP_DATABASE_PASSWORD=root go run cmd/main.go -migrate-only
```

`-migrate-only` 是**显式的迁移命令**，不受 `database.run_migrations` 约束
（见 `config/migrations_switch.go` 的 `ForceMigrations`）：同一份配置里写着
`run_migrations: false` 也照样迁移并跑 seed。它迁移完立即退出，退出码 0 即表示结构
与 seed 都到了最新；日志末行是 `数据库迁移与 seed 完成（-migrate-only），未启动 HTTP 服务`。

**第二步 · 换业务连接 + 打开启动门禁**：

```yaml
database:
  user: go_wp_app
  password: ""            # 走环境变量，别把明文写回 config.yaml
  run_migrations: false
  require_rls_role: true
```

```bash
# 启动前注入（口令仍然不落盘）
export GOWP_DATABASE_PASSWORD="$(grep -E "^GOWP_RLS_ROLE_PASSWORD=" ~/.config/go_wp/rls-role.env | cut -d= -f2-)"
go run cmd/main.go
```

启动日志里应先有

```text
INFO  按 database.run_migrations=false 跳过结构与 seed 迁移，请确认已用管理连接执行过 -migrate-only
```

再有探针结论

```text
INFO  连接角色 go_wp_app 不绕过 RLS：N 个对象上的工程隔离策略生效
```

启动期探针始终以 INFO 打印 `session_user` / `current_user` /
`pg_roles.rolsuper` / `rolbypassrls` / 策略覆盖的表数；
`require_rls_role=true` 时，连接角色仍会绕过 RLS 就**直接拒绝启动**（返回 error）。
默认 `false` 只打 WARN —— 迁移与运维复用同一个 `database` 组件、走管理连接，
默认 fail fast 会把正常运维挡在门外。**换完业务连接必须置 true**，这道门禁才算闭环。

**第一步还会顺手补齐时间序列表的未来分区**（`partition.EnsureAhead`）。
分区维护与结构迁移同属 DDL，业务角色跑不了：换角色后启动日志里每轮都会刷出

```text
ERROR 创建月分区失败  table=page_views partition=page_views_2026_10
      error=ERROR: permission denied for schema public (SQLSTATE 42501)
```

（page_views / master_data_changes / inventory_stock_movements × 未来若干个月，共 15 条。）
这是 **fail soft**：服务照常启动、数据落到 DEFAULT 分区不会丢，但「按月分桶」的收益
悄悄没了（桶永远建不出来）。把 `-migrate-only` 当**周期任务**跑（每月一次即可）就闭环了，
不需要给业务角色补 `CREATE` 权限 —— 那会把 DDL 门禁一起拆掉。

### 4.5 验收

1. 启动日志里出现 `连接角色 go_wp_app 不绕过 RLS：N 个对象上的工程隔离策略生效`；
2. 后台逐模块抽样：本工程数据读得到、另一工程的数据读不到（列表 / 详情 / 导出都要看）；
3. 跨工程写入被拒（策略的 `WITH CHECK`）；
4. 后台任务与调度器（构建、发布、清理、投递）逐条跑一遍 —— 它们最容易漏作用域。
5. `go test -count=1 ./public/test/rls/...`（非超级角色下才有意义）。

## 5. 回滚步骤

1. `config.yaml` 的 `database.user` 改回管理角色（root），
   `require_rls_role` 改回 `false`，重启应用 —— 这一步立刻恢复原行为，
   因为超级用户绕过全部策略。
   `run_migrations` **不必**一起改回：`false` 时启动不再迁移，但仍可用
   `-migrate-only` 走管理连接迁移（它不受该开关约束）。要完全回到旧行为
   （启动顺带迁移）再把它改回 `true`。
2. （可选）删角色：`DROP OWNED BY go_wp_app; DROP ROLE go_wp_app;`（脚本只授权不建依赖，
   删角色不需要动任何业务对象）。
3. 若已在 4.4 置 true 却忘了改 user，应用会启动失败 —— 这是**设计如此**：
   比「以为有隔离、实际没有」安全，错误信息里带角色名与命令，照着做即可。

## 6. 换角色后需要人工注意的每一处（本分支实测，含真实起来跑一遍的结果）

环境：本机 wp 库，业务连接 go_wp_app（NOSUPERUSER NOBYPASSRLS，不是任何表的属主、
public 上无 CREATE），管理连接 root；database.run_migrations=false +
require_rls_role=true，服务真实起在 127.0.0.1:8099 并用 dev-login 会话打了一遍
代表接口（列表 / 详情 / 后台页面）。

### 6.1 已修（本分支）

| 路径 | 换角色后的行为 | 修法 |
|---|---|---|
| product 的分类 / 品牌 / 标签 / 属性组列表与计数（ListCategories / ListBrands / ListTags / ListAttributes / ListAttributesByProject / CountCategoryChildren / DeleteBrand） | **实测复现**：GET /api/product/category/list?projectId=有数据的工程 返回 []，而库里那一行确实存在（分类页整页空白、属性组下拉为空，无任何错误日志） | 全部改走 rls.InProjectScope；需要新增 projectID 形参的入口由调用方传入（调用方手里都有） |
| product 的实体字段源（ResolverFor 里的 4 处 GetXxxWithoutScope） | 发布 / 预览时商品、分类、品牌、标签、属性组的字段渲染成空（产物对应区块缺失） | 读 core.BuildProjectID(ctx)（presentation 侧已在调用前注入）并改走带作用域的入口；拿不到工程则**显式报 ErrMissingProjectContext**，不再静默 0 行 |
| product.ProductTranslationCandidates | 翻译工作台打开单个商品报「商品不存在」 | 契约补 projectID 形参，调用方（翻译页）传入手里的当前工程；GetWithoutScope 自此没有生产调用方 |
| publication 的 GetRoute / ListRoutePathsByPage / ListRoutePathsByPresentation / IsPathOccupied | 路径占用预检恒答「没被占用」（重复占用被放行）；页面 / 实例删除时读不到已激活路径，active 目录里的符号链接不会被解除 —— 页面删了线上还在服务 | 全部包 rls.InProjectScope |
| publication 的非事务 DeleteRoutesByPage / DeleteRoutesByPresentation / DeactivateRoute | 取消激活是**空操作**（DELETE 匹配 0 行、不报错） | 同上；*Tx 变体保持不动，由调用方事务负责 |
| page 的 ListSiteSlotsByPage / SiteSlotRefsOfPage | 页面删除前的槽位引用检查一律答「没有引用」，删除放行、留下悬空绑定 | 补 projectID 形参（契约同步）+ rls.InProjectScope |
| inventory 的 ListSources / CountSources / SummarySources | 货源列表整页空白、采购单货源下拉为空、关联方报表两组计数恒为 0 | 同上一并包 InProjectScope（f.ProjectID / projectID 由 service 必填） |

### 6.2 无策略表（**不会** fail closed，但没有隔离）

1. **build_jobs**：两张「有 project_id、没有 ROW LEVEL SECURITY」的表之一（另一张见下条；project_id 由迁移 295 新增，215 的名单早于它）。切角色后它的查询既不报错也不被限制 —— Claim / ReclaimStale 按状态**跨工程**捞取是**有意**的队列语义，必须保持；而面向运维的 Stats / List / Retry 原本是「全队列视角」。本分支给它们加了**可选**工程过滤（GET /api/build/queue?project=、GET /api/build/jobs?project=、POST /api/build/retry 的 projectId），不带参数时行为与改造前逐字一致。**是否要把默认收敛成「只看本工程」是产品取舍，未擅自改。** 另：当前**没有**「某页面的构建任务列表」这类接口；将来要加，工程过滤必须写在 SQL 里（build/model 包注释已写死这条）。
2. **product_outbox_events**：同样是队列（按状态跨工程领取），没有生产侧的按工程读路径；ListOutboxEvents / CountPendingOutbox 只有测试与诊断调用。不需要动作。
3. **projects** 表本身没有 project_id、不在迁移 215 的策略名单里 —— ListAllProjectIDs 这类「逐工程定位」的兜底清单读它，**不受换角色影响**（实测：块 / 导航 / 订单 / 页面的作用域探测正常）。

### 6.3 构建 / 发布期读取的结论

internal/pipeline 与 internal/builder/core **没有任何直接数据库访问**（grep -rn "gorm.DB|database.GetDB" internal/pipeline internal/builder 为空），它们只经契约取数 —— 所以「会不会静默 0 行」完全取决于被调用的契约实现是否带作用域。逐条走下来的结论：

- **唯一的真实缺口**是 Registry.ResolverFor：它在 builder.Compile **之前**被 presentation 的 renderHTML 调用，而工程 id 原先只在 Compile 内部注入 —— 本分支已修（presentation 侧早前已在 buildCtx 上补 core.WithBuildProjectID，商品侧随之改走带作用域的读）。
- builder.Compile 自己会把 cfg.projectID 放进 buildCtx，集合源（商品集合、筛选选项）、实体字段源、内容解析器读的都是它 —— 已带作用域。
- 站点级注入（导航 Tree / TreeByID、槽位 ResolveSitePages、主题 GetActiveTheme、站点语言、站点设置）都以显式 projectID 入参并落在 InProjectScope 里。
- pipeline.SiteCompileOptions 统一注入 builder.WithProjectID(p.ProjectID)，page 与 presentation 两条构建路径共用这一份。

### 6.4 仍需人工 / 产品决定

1. **时间序列表的分区维护是 DDL，业务角色跑不了**（实测）：partition.StartScheduler 在装配期用业务连接建分区，换角色后每轮 + 每日都报 permission denied for schema public（3 张表 × 未来若干月 = 15 条）。服务照常启动、数据落 DEFAULT 分区不丢，但「按月分桶」静默失效。本分支把 partition.EnsureAhead 并进了 -migrate-only（管理连接），按周期跑该命令即可；若希望应用自己维护分区，就得给 go_wp_app 补 CREATE on schema public —— **那会把 DDL 门禁一起拆掉，是产品取舍，未擅自改**。
2. **跨工程的后台清理**：internal/retention 是声明目录，真正执行在各模块的 Sweep（按时间列批量删）。这类语句天然跨工程，**不能**简单包 InProjectScope：要么按工程逐个展开后带作用域执行，要么明确走管理连接。page/service/page_retention.go 已经是「逐工程展开」的样板；其余按时间删 scoped 表的路径要逐个确认。
3. **/admin/blocks 返回 500（与换角色无关，预先存在）**：实测 root 与 go_wp_app 两种连接下都是 500，日志是模板运行时错误：Jet Runtime Error ("/admin/blocks.html":73): cannot evaluate index (Path) in type int。本分支未改（不在改动域内），记录在此以免被误判成隔离问题。
4. 已接线的 11 个模块里仍可能有**个别方法**绕过包装（直接拼 DB(ctx)）；静态扫描只给候选，按 §7 的命令定期复扫。

## 7. 复核命令

```bash
# (a) 带 project_id 但没有 RLS 的表（当前应只剩 build_jobs 与 product_outbox_events 两张队列表）
psql -h 127.0.0.1 -U root -d wp -tAc "SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p') AND NOT c.relrowsecurity AND EXISTS (SELECT 1 FROM information_schema.columns col WHERE col.table_schema='public' AND col.table_name=c.relname AND col.column_name='project_id') ORDER BY 1"

# (b) 已接线的包 / 模块
grep -rl "go_wp/pkg/rls" --include=*.go internal/ | xargs -n1 dirname | sort | uniq -c | sort -rn

# (c) model / service 里以 gorm 访问 scoped 表、但所在包没 import pkg/rls（候选缺口）
grep -rln "ProjectID" --include=*.go internal/module/*/model internal/module/*/*/model | xargs -n1 dirname | sort -u | while read -r d; do grep -rq "go_wp/pkg/rls" "$d" || echo "候选: $d"; done

# (d) 当前连接身份（结论与启动探针一致）
psql -h 127.0.0.1 -U root -d wp -tAc "SELECT session_user, current_user, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user"

# (e) 策略覆盖对象数
psql -h 127.0.0.1 -U root -d wp -tAc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p') AND c.relrowsecurity"
```
