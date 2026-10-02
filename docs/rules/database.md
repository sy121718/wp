# 数据库规则细则

`AGENTS.md` 的「数据库」节给判据；本文件给论证、实测与操作步骤。

## 时间列

**命名**：统一 `create_time` / `update_time`（审计 DB-019，迁移 205 收口）。全库已无 `created_at` / `updated_at`。

**类型**：统一 `timestamptz`（迁移 212 收口）。全库 191 个时间列现在都是 `timestamp with time zone`。

最后 4 个例外是 webhook 两张表的 `create_time` / `update_time` —— 199 建表时用 BIGINT 存 `time.Now().Unix()`，
205 只改了列名没改类型，于是它们成了仅有的例外；212 用 `USING to_timestamp(...)` 转换过来。

**新表一律 `timestamptz` + Go 的 `time.Time`**，不要再引入 int64 时间戳：

- 丢掉亚秒精度（投递日志同秒内排序不稳定）；
- 无法直接用 PG 的时间运算与区间索引（BRIN / `date_trunc` 分组要先转换）；
- 与其它表的列比较必须显式转换。

**对外 JSON 只到秒（`utils.JSONTime`）**：库里的时间是微秒精度（`timestamptz(6)`，全库 199 列口径一致），
但对外 JSON **不该把存储精度透出去** —— Go 的 `time.Time` 默认按 RFC3339Nano 序列化
（`2026-09-16T13:57:50.123456+08:00`）：同一秒内的两次写入看起来不同、前端做秒级比较 / 分组要自己截断、
每条记录多 7~10 字节（列表接口乘起来很可观），而且协议会跟着存储走。

- dto 的时间字段一律用 `utils.JSONTime`（可空用 `*JSONTime`）；
- 序列化 RFC3339 **到秒**；零值与 nil 给 `null`（不是 `0001-01-01T00:00:00Z`）；
- 解析比标准库宽松：RFC3339 / `2006-01-02 15:04:05` / `2006-01-02`（后两种是后台原生表单与既有客户端在用的）；
- 写库仍走 `time.Time` 保留微秒；
- service 在 model 与 dto 之间转换：去程 `utils.NewJSONTime` / `utils.NewJSONTimePtr`，回程 `.Time()` / `.TimePtr()`；
- 布局常量收在 `utils.LayoutSecond` / `LayoutDay` / `LayoutJSON`（此前十余处硬编码 `"2006-01-02 15:04:05"`）。

## 软删除列名

统一 `deleted_at`（审计 DB-020，迁移 208 收口）。`sys_menus` 原本的 `deleted_time` 已改名。

`sys_attachment` 用 `status` 表达删除属**存量例外**，新表不要照抄。

## model 不声明列型

（2026-09 收口，架构测试 `internal/architecture/model_gorm_tag_test.go` 守门）

列的类型由迁移决定，model 标签不重复声明。重复声明就等于**两份真相** —— 抄错时没有任何东西会报错：

- `sys_admin.status` 真实是 `smallint`、标签写着 `tinyint(4)`；
- 时间列真实是 `timestamptz(6)`、标签写着 `timestamp(3)` 无时区 + 毫秒。

而任何 AutoMigrate 路径会照标签把错的列型建出来。本轮清掉 598 处 —— 其中 57 处 `type:timestamp(3)`
与 17 处 `type:datetime(3)` 是**上一轮清过又长回来的**（当时没有测试兜底），所以这次连红线一起立。

**唯一例外**：gorm 无法自行推断列型的字段（`json.RawMessage` / `JSONMap` / `StringArray` 等）必须保留
`type:`（或改用 `serializer:`）指明映射 —— 那说的是「Go 值怎么变成 SQL 值」，不是列型真相，
删掉会直接报 unsupported data type。

## 迁移台账的坑

### 改列名时不会自动跟随的两类对象

- **触发器 / plpgsql 函数体**：函数体是字符串，RENAME 后仍按旧名解析（迁移 206 修的就是它）；
- **seed SQL**：seed 可重复执行，必须同步改；历史迁移 SQL 保持原样。

索引表达式、视图、约束由 PG 自动重写。

### `CheckSQL` 的 `?` 是表名

迁移的 `CheckSQL` 里 `?` 由迁移器传入的是**表名**；判定要用的其它值（权限点代码等）必须写进 SQL 字面量，
否则判定恒为 0、迁移每次启动都重跑（178 踩过）。

### `PREPARE` 校验只对 DML 有效，结构迁移得靠「执行 + 回读 + 重跑」

AGENTS.md 里「新迁移 SQL 上线前用 `PREPARE` 静态校验」这条**只适用于 DML 迁移**（INSERT / UPDATE / DELETE / SELECT）。

`PREPARE` 走的是「可优化语句」通道，**`ALTER TABLE` / `CREATE TABLE` / `COMMENT ON` 这类 DDL 一律直接报错**：

```
PREPARE chk491 AS ALTER TABLE pages ADD COLUMN IF NOT EXISTS excluded_langs text[] NOT NULL DEFAULT '{}';
→ ERROR: syntax error at or near "ALTER"
```

这个报错**不说明 SQL 有问题**（同一条语句直接执行成功）—— 只是 `PREPARE` 不接受 DDL。
而 `mustSQL` 只校验 embed 文件**存在**、不校验可执行，于是「Go 编译通过 + PREPARE 报错」
很容易被读成「迁移坏了」，也可能反过来：因为怕报错而干脆不校验结构迁移。

**结构迁移的验证手段**（三者都要做，缺一都会漏）：

1. **真实执行**一次（开发库），确认没有语法 / 权限 / 约束错误；
2. **目录回读**：从系统目录确认对象真的长成了要的样子 ——
   `pg_attribute`（列名 / `format_type` / `attnotnull` / 默认值）、`pg_constraint`（约束）、
   `pg_indexes`（索引）、`col_description`（注释）；
3. **幂等重跑**一次，确认 0 行且不报错（`ADD COLUMN IF NOT EXISTS` 会给出
   `NOTICE: column … already exists, skipping`，那是**成功**不是失败）。

**实例（2026-10-02，迁移 491 给 `pages` 加 `excluded_langs text[]`）**：`PREPARE` 报
`syntax error at or near "ALTER"`，改用上面三步 —— 执行 0 行（DDL 无行计数）、
`pg_attribute` 回读确认 `text[]` / NOT NULL / 默认 `'{}'::text[]` / 注释在位、
重跑给出 `already exists, skipping` 且**不影响存量行**（13 行全为空数组、无 NULL）。

**附带一条同类陷阱**：结构迁移若指向**已存在的表**，注册时**不能填 `TableName`** ——
`migrator.apply` 的默认 `CheckSQL` 是「表存在即跳过」，会整条跳过、ALTER 永不执行，
于是全新库有这一列、存量库没有（484–486 与 491 都为此刻意不填，代价是文件里只能放幂等语句）。

### Migrations 先跑、Seeds 后跑

**在 `register` 里做的删除，永远赢不过在 `registerSeed` 里重建它的 seed**（2026-09 实测的真实故障）。

`migrator.go` 的 `runAll(All())` 与 `RunSeeds(AllSeeds())` 是两个独立循环，各自按版本排序；
所以「先删、后被插回」不取决于版本号大小，只取决于它在哪个台账里。

实例：迁移 122（`register`）负责删除库存缓存下线后遗留的 `inventory:cache_sync` / `cache_reconcile`
权限点，而 104（`registerSeed`）的 seed 与幂等条件里**还留着这两个码**，条件是「本票 10 个权限点齐了才跳过」。
于是每轮启动：122 删 2 个 → 104 条件不满足 → 重新插回 → 库里**一直存在**指向不存在路由的死授权
（后台勾选毫无作用，误导配置者）。现象极具欺骗性：122 的日志写着「迁移完成」，
104 的日志写着「种子数据已存在，跳过」，两边的日志都正常。

**判据**：删能力时要连 seed 的 SQL 与幂等条件一起收口（条件计数与 key 列表同批改），不能只删词条/权限点。
停在 `register` 里的「删除」只是看起来删了。

**回归**：`public/migrations/register_retired_permission_test.go` —— 任一迁移 SQL 里删除的权限点代码，
都不得再出现在任何 seed 的 SQL 里（含判据自身的命中/误报自检）。它是启发式（只认
`permission_code IN (…)` 这一种写法），漏掉不代表没问题。

**同类**：`order.msg.cancelledStockWarning` 的死常量之所以只能「废弃但保留」，就是因为它的 seed 判定按
「本批 key 计数且包含本 key」；180 的两行与 `register_admin_i18n.go` 的判定 key 列表、门槛
（`>=62` → `>=61`）同批改掉之后，常量才连同词条一起删净。

## 主键选型

**判据：这个 id 会不会出现在系统边界之外。**

- **对外实体**（`projects` / `pages` / `products` / `blocks` / `themes` / `content_templates` 等有对外接口，
  或 id 进了 Page Document / 产物元数据 / 导出物的）用 **uuid**；
- **纯内部流水与字典**（`page_views`、`build_jobs`、`publication_receipts`、`page_site_slots`、
  `inventory_change_reasons`、`sys_*` 全系）用 **bigint identity**。

**两套并存是设计，不是待消除的不一致** —— 缺判据才是问题。新表按此选型，别为了「统一」把对外实体改成自增
（id 一旦可枚举就少一层纵深，与 DB-009 想要的隔离方向相反）。

判据只约束**新表**，**存量按现状为准**：`master_data_changes` / `inventory_stock_movements` 是 uuid 存量
（后者 id 已进对外列表投影 `MovementRow`），说明「流水必然内部」这个直觉不成立 —— 别拿判据去反推存量。

### 代价是实测过的，别凭感觉排优劣

本地 PG 18.6，100 万行同结构同 payload：

| 主键 | 插入 | 主键索引 |
|---|---|---|
| `bigint identity` | 1.90s | 21MB |
| `uuid` v7 | 2.81s | 30MB |
| `uuid` v4 | 5.42s | 38MB |

**点查三者无差别**（都在测量噪声内 —— 别拿它当任何一方的论据）。

所以 uuid 不是「更好的主键」，而是为「id 不可枚举」付的写放大（v4 随机插入导致 B-tree 页分裂，
写放大 2.85 倍）：付它的唯一依据就是上面那行判据，量级越大的内部表越该用 bigint；
对外实体真要用 uuid 就用 v7，实测能追回六成以上代价。

### 只增的分区流水表用 UUIDv7

`inventory_stock_movements` / `master_data_changes`，统一经 `utils.NewTimeOrderedID()`：
写入点集中在索引右端，实测把 v4 的写放大砍掉一半（插入 5.42s → 2.81s、主键索引 38MB → 30MB）。

与上面的判据不冲突 —— 判据决定「bigint 还是 uuid」，v7 决定「内部表用哪种形状的 uuid」。

两个反作用要记住：

- **v7 的时间前缀会透露创建时间**，所以对外实体（`projects` / `pages` / `products` / `blocks`）继续用
  v4 的 `uuid.NewString()`，别顺手替换；
- 从 v4 切到 v7 后「按 id 排序」会从无序变成等价于创建先后，原先靠 id 排序读创建顺序的写法要显式改用
  `create_time`。

### uuid 在应用层生成

新表选 uuid 时**在应用层生成**（`uuid.NewString()`，见 project / block 的创建路径）：DDL 的
`DEFAULT gen_random_uuid()` 只是兜底 —— gorm 对 string 主键的零值会**显式写入空串**（不像 int 那样交给
identity），依赖 DB 默认值会踩 22P02。

内部流水表用 UUIDv7 同理（`uuidv7()` 是 PG 18 函数而 CI 是 PG 16，所以一律走应用层）。

### 改主键类型时，引用会渗进文档内容

`blocks.id` 同时存在于三处、分布在 10 个 JSONB 列：

- `props.blockId`（root 树任意深度）；
- `settings.structure.headerBlockId` / `footerBlockId`；
- `settings.slots.*`。

存储点含 `page_revisions` / `document_snapshots` 历史快照与 `page_artifacts.source_document`，
再加 `page_dependencies.dependency_key`。改这类 id 之前先用键名把存储点摸全
（迁移 209 的注释列了完整清单），否则会静默留下断裂引用。

## 工程隔离 RLS（DB-009）

### 现状：策略已铺，但在换连接角色之前不生效

迁移 215 给带 `project_id` 的对象装了 `ROW LEVEL SECURITY` + `FORCE`
（覆盖多少张**不写死数字**：迁移 462 / 466 等还在追加，以 `pkg/rls` 的连接身份探针读数
`Identity.RLSTables` 为准 —— 写死的数字一定会再次过时，且过时是静默的），
策略谓词读会话变量 `app.project_id`（未设置即行不可见，fail closed；`inventory_change_reasons` /
`sys_translation` 额外放行 `project_id IS NULL` 的全局行）。

**但 PostgreSQL 的超级用户总是绕过 RLS** —— `FORCE` 只约束到表属主，约束不了 superuser / `BYPASSRLS`
角色，而应用连接用的是超级用户 `root`，所以策略目前一行都挡不住。

实测（`themes` 表 1 行数据）：root 未设变量读出 1 行，普通角色未设变量读出 0 行。

### 顺序不能反

先给各模块的读写路径包上 `pkg/rls.InProjectScope`，**再**把 `config.yaml` 的 `database.user` 换成
非超级的**次级管理员**角色：

```bash
bash scripts/rls-role-setup.sh    # 默认角色 go_wp_app
```

- 口令走 `GOWP_RLS_ROLE_PASSWORD` 或 `~/.config/go_wp/rls-role.env`（权限 600），**不再经命令行参数**；
- 含口令的 SQL 一律经 stdin 送 psql；
- 脚本含 `ALTER DEFAULT PRIVILEGES`，让将来新建的表也自动授权。

反过来的话，没包 scope 的路径会**静默返回 0 行**（fail closed 不报错），表现为「功能突然查不到数据」
而没有任何错误日志。

### 「策略铺好了」≠「隔离生效」，判据只能从库里读（DB-04）

启动期探针 `database.CheckRLSIdentity` 每次都读 `session_user` / `current_user` /
`pg_roles.rolsuper|rolbypassrls` / 当前 schema 的策略覆盖数并以 INFO 打印结论，连接角色会绕过 RLS 时补一条
WARN；把 `database.require_rls_role` 置 `true` 后，这两种角色会让**启动直接失败**。

默认 false 是刻意的（迁移与运维脚本复用同一个 database 组件、走管理连接），
**换成应用角色之后必须置 true**，这道门禁才算闭环。切换与回滚步骤见 `docs/rls-role-cutover.md`。

### `require_rls_role=true` 与「跑迁移」互斥，以及怎么解

这两件事在同一个进程里是**互相排斥**的，实测下来三步都堵：

1. 服务侧正确配置是 `require_rls_role: true` + `database.user = go_wp_app`（非超级、
   `rolbypassrls=false`）；
2. 迁移要 DDL 权限，而 `go_wp_app` 没有 —— `scripts/rls-role-setup.sh` 刻意只给它 DML 与
   `USAGE`（**这是设计，不是遗漏**：服务连接不该有 DDL 权限）；
3. 换成超级用户（`sky`，`rolbypassrls=true`）跑迁移，又会被探针拒（`require_rls_role=true`）；
   而 `require_rls_role` **曾经不在 `config.envBindableKeys` 里**，环境变量覆盖不了
   ——于是「换个角色也跑不了迁移」。

**解法是让迁移能关掉探针，而不是给应用角色加 DDL 权限**：

- `database.require_rls_role` 已纳入 `config/config.go` 的 `envBindableKeys`（它在「换个环境
  就得换值」这一类：服务侧要 true、迁移场景要 false）；
- `Makefile` 的 `migrate` 目标带 `GOWP_DATABASE_REQUIRE_RLS_ROLE=false`（**只影响这一条命令**，
  不动服务启动路径与 `database.run_migrations` 的语义），依据就是 `pkg/database/rls_probe.go`
  的那句「默认 `require_rls_role=false` 是刻意的：迁移与运维脚本用管理连接（超级用户）执行 DDL」。

**不要用「给应用角色加 `GRANT CREATE`」来解决**：那等于把工程隔离拆掉换方便 —— 服务连接一旦
有 DDL 权限，任何一处被攻破（SQL 注入 / 依赖漏洞 / 配置泄露）都能改结构，而 RLS 恰恰是
「应用角色权限尽量小」这条链的最后一环。迁移的 DDL 权限属于**管理连接**，不属于运行服务的角色。

### 分区子表必须单独设

**PG 的 `ENABLE` / `FORCE` 不递归到分区**（实测父表 `relrowsecurity=t`、子表全为 `f`），
新分区的策略由 `internal/partition.EnsureAhead` 建表后补。

### 覆盖面

`pkg/rls.InProjectScope` 已接到 11 个模块的 model / service —— analytics / block / contenttemplate /
masterdata / navigation / order / page / presentation / product（含 inventory）/ project / publication；
样板见 `internal/module/project/model/locale_model.go`（`project_locales` 是 199 的试点）。

### 换角色前先看两个已知缺口

- 有 `project_id` 却**没有**策略的表有**两张**：`build_jobs` 与 `product_outbox_events`。
  两张都是「按状态跨工程领取」的队列，消费者一次领取所有工程的待办 —— 装了策略会让 worker
  看不见别的工程的待办，所以豁免是**有意**的（`product_outbox_events` 的迁移 309 注释自己也写了
  「本表不装 RLS 策略」，`build_jobs.project_id` 由迁移 295 新增，215 的名单早于它）。
  **代价与别的表相反**：切角色后它们的查询既不报错也不被限制，工程过滤必须显式写在 SQL 里；
- 其余带 `project_id` 的表已实测全部 `ENABLE` + `FORCE` + 有读 `app.project_id` 的策略
  （**张数以运行时探针为准**，不在文档里写死：迁移一直在追加对象）；
- `internal/pipeline` / `internal/builder/core` 自身不 import `pkg/rls`（构建期的依赖读取带不带作用域
  取决于被调用方）。

> 这句「除白名单外全部 `ENABLE` + `FORCE`」有**可执行**判据：
> `public/test/rls/rls_policy_coverage_test.go` —— 扫生产迁移建出的 schema，
> 白名单（该测试内逐条列出的豁免表 + 理由）之外的「带 `project_id` 的基表」必须有
> `relrowsecurity` + `relforcerowsecurity`。新增无策略表而不登记 → 测试红。

精确检索命令见 `docs/rls-role-cutover.md` §7。

## 权限点与 seed

**新增挂在 `authorizedAPI` 下的接口：加一条权限点常量 + 在路由注册处声明，不写 seed 迁移。**

常量加在 `internal/permission/codes.go`，并在该路由的注册处把 `permission.Perm` 作为
`RouteGroup.GET/POST` 的第二个参数给出（漏写是编译错误，拼错在装配期 panic）；装配末尾
`permission.SyncToDB` 把声明**幂等 upsert** 进 `sys_permission` 与超管（`is_admin=1`）策略（审计 SEC-011）。
030/031 是存量权限点台账，新权限点不再往 seed 里加。

**为什么权限点必须齐**：该组统一挂 `CasbinMiddleware()`，按**实际请求路径** enforce，权限点缺失时没有
任何策略能匹配，**含超管在内全员 403**（072/077/078/079 各踩过一次，151 又补了 page:delete 与
block:clone）。

**为什么还要跑 `bash scripts/check-permission-gaps.sh`**：声明式注册只覆盖「编译期写了常量 + 装配期声明过」
这一侧，脚本拿**运行时路由表**跟库里的 `sys_permission` 比对，专门抓注册期看不见的缺口
（人工在库里删了某条权限点、有人绕过 `RouteGroup` 直接往授权组挂路由、代码删了权限点但库里还在漂移）。

## 数据域（datarule）

**白名单由拥有该表的实体声明**，不另抄一份字段表。

实体字段上写 tag：

```go
DeptID uint64 `gorm:"column:dept_id" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
```

经 `pkg/datarule.DomainFromEntity` 派生（表名取实体 `TableName()`，列名取 gorm `column` 标签），
由模块装配入口注册（样板：`internal/module/admin/inbound/http/datarule_bootstrap.go`）。

- **没有 tag 的字段不在白名单里**（fail-closed）；
- 抄错列名**不会报错**，只会在运行时表现为「过滤条件被静默丢弃 / Omit 一个不存在的列」——
  规则看起来生效、实际什么都没拦；
- 规则配置的字段与操作符在写入侧按域声明逐项校验（dto 声明形状与枚举，service 判定是否属于该域）。

其余细节：

- 插件字段引用按方言（PG 双引号 / MySQL 反引号）；部门范围整段精确匹配；
- 域声明写错（未知操作符、缺 label、字段名非法）一律装配期失败，不要降级成「这个域没有白名单」；
- 注册动作必须在**注册路由之前** —— 域没注册上的表不会被任何规则拦住（引擎按表名匹配域，
  匹配不到就直接放行）；
- 运行时模板见 `internal/module/admin/service/datarule_crud.go` 的 `validateRuleConfig`。

## 查库

开发 / 审计查库统一走 dbx MCP：连接名与库名以本机 DBX 配置为准（勿在文档里写死连接名），
调用时显式传 `connection_name`。

应用运行时库名见 `config.yaml` 的 `database.dbname`（示例 `config.yaml.example` 默认为 `wp`）。
主库 PostgreSQL，最低版本以 CI（`.github/workflows/go-test.yml` 的 postgres 服务）为准。

当前 schema 权威说明见 `docs/schema-snapshot.md`（`init_builder_schema.sql` 仅为历史快照）。
