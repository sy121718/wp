# 贡献指南

面向第一次拿到这个仓库的人。读完这份能把项目跑起来、改一处代码、跑对门禁、把改动提出来。

英文摘要见 [docs/contributing.en.md](docs/contributing.en.md)；本文件是中文完整版，两者内容重叠但本文件更细。

## 1. 动代码前必读

| 文件 | 为什么先读 |
|---|---|
| [`AGENTS.md`](AGENTS.md) | 常驻红线清单：不变量、命名约束、路由动词、错误文案、迁移与 seed 纪律、时间列命名、软删除、RLS 顺序 |
| [`internal/module/CLAUDE.md`](internal/module/CLAUDE.md) | 模块分层与契约：目录骨架、contract 三段分工、service 不得持 `*gorm.DB`、装配与 datarule 注册 |
| [`internal/templates/CLAUDE.md`](internal/templates/CLAUDE.md) | Jet 模板约定：可选键必须 `isset`、缺键的两种后果、CSRF 取值链 |
| [`public/test/CLAUDE.md`](public/test/CLAUDE.md) | 测试目录组织、命名约定、测试配置与执行边界 |
| [`docs/rules/`](docs/rules/README.md) | 规则细则：`database.md`、`testing.md`、`frontend.md`、`i18n.md` —— 需要「为什么这么定」时读 |

`AGENTS.md` 只放判据与指针；判据背后的论证、实测数据与踩坑实例在 `docs/rules/`，按需读，不必通读。

## 2. 本地起步

### 2.1 依赖

- **Go 1.26+**（与 `go.mod` 的 `go 1.26` 一致）
- **GNU Make**
- **PostgreSQL**（本机实例；本机开发不依赖 Docker）
- **Redis**（必需组件，不是可选缓存 —— 会话依赖它）
- **Node.js**（仅 `scripts/check-workbench.sh` 需要；不参与生产资产构建）

版本基线见 [docs/getting-started.en.md](docs/getting-started.en.md)：开发与生产按 **PostgreSQL 18+ / Redis 7+** 准备；CI 的测试容器用的是 `postgres:16` / `redis:7`（`.github/workflows/go-test.yml`），更早的版本未经验证。`go.mod` 不表达数据库版本，示例配置见 [`config.yaml.example`](config.yaml.example)。

### 2.2 配置

```bash
cp config.yaml.example config.yaml
go mod download
```

`config.yaml` 的分组见 `config.yaml.example`：`server` / `database` / `redis` / `auth` / `casbin` / `queue` / `log` / `upload` / `analytics` / `cart`。

- 数据库连接默认读 `database` 段；`GOWP_DATABASE_*` 环境变量会覆盖同名字段。
- 生产口令不要写进 YAML：`database.password` 推荐用 `GOWP_DATABASE_PASSWORD` 注入，`auth.session_secret` 走 `GOWP_AUTH_SESSION_SECRET`。

### 2.3 迁移与 seed

```bash
make migrate
```

这一条目标同时做「结构迁移」与「业务 seed」，可重复执行。它做的事：用 `pg_isready` 轮询等待本机 PostgreSQL（30 次 × 2 秒），然后把 `PGHOST` / `PGPORT` / `PGUSER` / `PGPASSWORD` / `PGDATABASE` 映射成 `GOWP_DATABASE_*` 并执行 `go run ./cmd -migrate-only`。

`-migrate-only` 是显式的迁移命令，**不受 `database.run_migrations` 约束**；那个开关只管「服务启动时是否自动迁移」。迁移完成后立即退出，不监听端口。

迁移的真实入口：

- SQL 放在 [`public/migrations/`](public/migrations/)，版本化命名 `NNN_名称.sql`；整个目录经 `//go:embed *.sql` 一次嵌入。
- Go 侧注册体按主题拆在 `public/migrations/register*.go`，入口是 `register.go` 的 `init()`：新增一条迁移 = 新增一个 `.sql` + 在对应主题文件里加一条 `register(Migration{...})`。
- seed 用 `registerSeed(Seed{Version, TableName, ConditionSQL, SQL})`，`ConditionSQL` 是幂等判据（「本批对象是否已存在」，**不要**写成宽泛的前缀判断）。
- `TestEmbeddedSQLFilesAllRegistered` 对磁盘 `.sql` 与注册台账做双向一一对应校验 —— 写了 SQL 却忘了注册会被测试抓住。

> 迁移编号取现有最大值之后的号（当前最大为 `510`）。命名与判据写法见 [`docs/rules/database.md`](docs/rules/database.md)。

### 2.4 起服务

```bash
make dev     # 开发模式，air 热重载
make run     # 直接编译并运行，无热重载
```

访问 `http://127.0.0.1:8080`。健康检查：`GET /livez`（存活）、`GET /readyz`（就绪）。

不带参数运行 `make` 会列出全部目标。常用：`dev` / `build` / `run` / `test` / `test-short` / `lint` / `check` / `check-db` / `check-all` / `test-race` / `migrate`。

## 3. 门禁

`make check` 是本地跑门禁的统一入口。三个目标的分工：

| 目标 | 实际执行 | 覆盖 | 是否依赖数据库 |
|---|---|---|---|
| `make check` | `gofmt -l` + `go vet ./...` + `bash scripts/check-all.sh` | 无外部依赖的 12 个门禁脚本 | 否 |
| `make check-db` | `bash scripts/check-all.sh --db-only` | 需要 PostgreSQL 的 1 个门禁脚本（须先 `make migrate`） | 是 |
| `make check-all` | `bash scripts/check-all.sh --with-db` | 上两组全跑，共 13 个脚本 | 是 |
| `make test-race` | `go test -race -p 4 ...` | 五个核心域的竞态检测（`pkg/`、`pipeline`、`builder`、`publication`、`presentation`），超时 20 分钟 | 否 |
| `make test` | `go test -p $(nproc) ./... -count=1` | 全量（feature + unit），可用 `TEST_PARALLEL=8` 覆盖并发 | 部分用例 |
| `make test-short` | `go test ./pkg/... ./internal/... ./cmd/... ./config/... ./public/migrations/... -count=1` | 不依赖数据库的包 | 否 |
| `make lint` | `gofmt -l internal/ pkg/ public/ cmd/ config/` + `go vet ./...` | 格式与静态检查 | 否 |

<!-- TODO(开源前确认): `scripts/check-all.sh`、`scripts/check-contract-deps.sh`、`scripts/check-dto-immutability.sh`、`scripts/check-page-endpoint-authz.py`、`scripts/page-endpoint-authz-allow.txt`、`scripts/contract-deps-allow.txt` 在本节写作时仍是工作区未提交文件（`git ls-files` 无输出），`make check-db` / `make check-all` / `make test-race` 三个目标也只存在于工作区的 Makefile 改动里；必须确认它们已入库，否则克隆下来的仓库里不存在这些命令。 -->

统一入口脚本是 [`scripts/check-all.sh`](scripts/check-all.sh)，模式为默认（无依赖组）/ `--db-only` / `--with-db`，末尾打印「✓ 全部门禁通过（模式，共 N 个脚本）」。

无依赖组（12 个）：

```text
scripts/check-no-internal-error-leak.sh
scripts/check-i18n-coverage.sh
scripts/check-i18n-keys-seeded.sh
scripts/check-service-db-boundary.sh
scripts/check-page-endpoint-authz.py
scripts/check-multidevice-css.sh
scripts/check-empty-state-table-head.sh
scripts/check-inventory-sku-format.sh
scripts/check-stock-sku-prefix-collisions.sh
scripts/check-workbench.sh
scripts/check-contract-deps.sh
scripts/check-dto-immutability.sh
```

数据库组（1 个）：

```text
scripts/check-permission-gaps.sh
```

每个脚本拦什么、判据是什么、豁免怎么记账 → [docs/contributing/gates.md](docs/contributing/gates.md)。

`scripts/` 下还有不入门禁的脚本：`dev.sh`（`make dev` 调用）、`rls-role-setup.sh`（建非超级角色并授权）、`index-usage-report.sh`（索引使用量快照）、`site-preview.py`、`audit-*.py`，以及 `build-all.*` / `build-frontend.*` / `prepare-embed.*` / `deploy.sh` —— **最后这一批属历史脚本**，缺少它们依赖的前端源码与发布产物定义，不要拿来发版（见 [`scripts/README.md`](scripts/README.md)）。

## 4. 测试约定

### 4.1 两层

- **模块内就近单测**：`internal/module/**/*_test.go`。纯逻辑，不碰数据库、不走路由装配。
- **feature / 集成测试**：`public/test/**`。需要数据库、事务、HTTP 或完整装配的用例放这里。

判据是「这个判断错了会不会静默出错」：会静默出错的逻辑必须有测试，纯取值的浅层测试不写。

### 4.2 目录组织

```text
public/test/<模块>/feature/    接口链路测试（主）
public/test/<模块>/unit/       模块规则单测（辅）
public/test/feature/           跨模块顶层链路
public/test/architecture/      架构红线静态扫描（边界 / 事务 / model 标签）
public/test/rls|partition|security|seo|pipeline|pkg/   横切主题
public/test/support/           测试基建
public/test/fixtures/          静态夹具数据（只放静态数据）
```

目录名直接用模块名或领域名，不再额外分层（是 `public/test/order/feature/`，不是 `public/test/backend/order/`）。

### 4.3 命名

- 文件：`<模块>_<场景>_test.go`
- 函数：`Test<模块><场景><结果>`，例如 `TestAdminLoginSuccess`、`TestOrderCreateStockShortage`
- 每个核心场景一个顶层 `TestXxx`，内部分支用 `t.Run` 切子用例

### 4.4 测试表结构一律来自生产迁移

**禁止手抄 `CREATE TABLE`**。手抄的 DDL 与生产 schema 会静默分叉：迁移改了列名或主键类型后，用例要么集体变红看不出原因，要么在缺约束的表上继续绿、把真实缺陷放过。

用 `public/test/support/` 里的基建：

- `migrated_db.go`：`NewMigratedPGTestDB` —— 跑生产迁移得到结构（需要父行时用 `SeedProjectRow` 一类助手补）
- `pgtest.go`：`NewPGTestDB` —— 空库，给自建表或刻意构造旧 schema 的用例
- `test_bootstrap.go`：只做环境初始化与清理，不启动生产服务进程
- `test_client.go`：HTTP 客户端

### 4.5 执行

```bash
go test ./public/test/...                                  # 全部集成测试
go test ./public/test/order/feature -v                      # 单个模块
go test ./public/test/admin/unit -v                          # 模块规则单测
go test ./public/test/order/feature -run TestOrderCreate -v  # 单个场景
make test                                                    # 全量
```

数据库与 Redis 用 `PGHOST` / `PGPORT` / `PGUSER` / `PGPASSWORD` / `PGDATABASE` 和 `TEST_REDIS_ADDR` 指向专用测试实例；部分用例支持容器回退。

**退出码 0 不能证明用例真跑过**：环境缺失时用例会 skip。下结论前看 skip 原因与环境依赖缺口，把它写进改动说明。

## 5. 提交约定

- **一次改动一个主题**：不与无关清理、依赖升级、大范围格式化混在一起，diff 才可读、可 review。
- **拆分大文件要说明等价性**：证明「只切段、不改行为」的证据是仓库里已有的路由清单快照测试（`internal/routers` 的路由快照用例，`WP_DUMP_ROUTES=1` 可输出装配出的路由表）——装配是线性的，重排等于引入编译期看不见的顺序漂移。
- **新增门禁脚本要接进 `scripts/check-all.sh`**：无外部依赖的进 `PLAIN_SCRIPTS`，需要 PostgreSQL 的进 `DB_SCRIPTS`。散在 `scripts/` 下不被任何入口调用的门禁等于没有门禁（当前这个入口本身就是为此补的）。
- **改动某模块的落地细节时，同批更新 `docs/13-module-inventory.md`**（模块落地后同步该文件是它的明文要求）。
- **提交前跑**：

```bash
gofmt -w path/to/changed.go
go build ./...
go vet ./...
make check
make test-race      # 触及 pkg/ 或核心五域时
```

- **模块纪律摘要**（细节在 `internal/module/CLAUDE.md`）：跨模块只经 `contract` 与不可变 dto；service 不持 `*gorm.DB`、不切别人的表；两处及以上持久化写入必须同一事务。
- **权限点**：新增接口在路由注册处声明权限点（`permission.RouteGroup` 的 `GET` / `POST` 第二参数），启动期幂等 upsert 入库，**不需要写 seed 迁移**；`internal/permission/codes.go` 的常量字符串不要手工改名（改名等于新建权限点，旧授权失效）。
- **路由动词只用 `GET` 与 `POST`**。
- **不要碰**：`git reset --hard` / `git checkout --` 一类会丢弃他人改动的命令；工作区里已有的无关改动保持原样。

## 6. 已知限制

- 前端打包脚本（`build-all.*` / `build-frontend.*` / `prepare-embed.*` / `deploy.sh`）当前不构成可用发布链路，详见 [`scripts/README.md`](scripts/README.md)。
- 生产构建用 `go build -o app ./cmd`，后台模板与 `/static` 须一并交付 —— 不能只复制一个二进制。
- 开源发行仍待补可复现发行包与更完整的自动检查。
