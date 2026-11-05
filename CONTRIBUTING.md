# 贡献指南

面向第一次拿到这个仓库的人。读完这份能把项目跑起来、改一处代码、跑对门禁、把改动提出来。


## 1. 动代码前必读

| 文件 | 为什么先读 |
|---|---|
| [`AGENTS.md`](AGENTS.md) | **唯一规则源**：架构（MVC + Service）、分层判据、响应协议、取词规则、架构不变量、迁移与 seed 纪律、RLS 顺序 |
| [`internal/module/CLAUDE.md`](internal/module/CLAUDE.md) | 模块目录骨架与落地细节：控制器渲染两种等价写法、service 不取词、service 不得持 `*gorm.DB` |
| [`internal/templates/CLAUDE.md`](internal/templates/CLAUDE.md) | Jet 模板约定：取词写法、可选键必须 `isset`、写基座 `data-write-*`、htmx v4 属性、多端硬规则、前端模板统一封装待实施清单 |
| [`public/test/CLAUDE.md`](public/test/CLAUDE.md) | 测试目录组织、命名约定、测试配置与执行边界 |

## 1.1 架构一句话

**MVC + Service**：Controller 选模板并带业务 data → Jet 渲染；Service 承载业务规则、事务与查询；Model 管本模块表；View 是 Jet 模板 + HTMX。
响应协议（页面 GET 成功回模板，其余回 JSON `{code, message, data}`）与全部分层判据见 [`AGENTS.md`](AGENTS.md)「架构」「响应协议」两节，本文不复制。

## 2. 本地起步

### 2.1 依赖

- **Go 1.27+**（与 `go.mod` 的 `go 1.27.0` 一致）
- **GNU Make**
- **PostgreSQL**（本机实例；本机开发不依赖 Docker）
- **Redis**（必需组件，不是可选缓存 —— 会话依赖它）
- **Node.js**（仅 `scripts/check-workbench.sh` 需要；不参与生产资产构建）

版本基线：**开发、CI、生产必须同一大版本**。版本号在 `public/test/support/versions.go` 声明一次，
CI workflow 的 services 镜像与它由 `versions_test.go` 对账，漂移即红。

改动数据库版本时三处同步（漏一处会有测试告诉你）：
1. `public/test/support/versions.go` 的 `TestPostgresMajor` / `TestRedisMajor`
2. `.github/workflows/go-test.yml` 的 `services.*.image`
3. 该文件的 `GOWP_PRODUCTION_VERSIONS`，以及部署机的实际版本

`GOWP_PRODUCTION_VERSIONS` 是**生产版本的唯一声明处**（形如 `postgres=18,redis=8`）。它为空时
对账测试会明确报告「生产版本未声明，一致性未经核对」而不是假装对齐；本地开发不设它也没问题，
CI 的 integration job 必须设。`go.mod` 不表达数据库版本，示例配置见 [`config.yaml.example`](config.yaml.example)。

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

> 迁移编号取 `public/migrations/` 里现有最大数字之后的号（不要在本文件记具体数字，会过期）。命名与判据写法见 [`AGENTS.md`](AGENTS.md) 的数据库一节。

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
| `make check` | `gofmt -l` + `go vet ./...` + `bash scripts/check-all.sh` | `check-all.sh` 的无依赖组（`PLAIN_SCRIPTS`） | 否 |
| `make check-db` | `bash scripts/check-all.sh --db-only` | 需要 PostgreSQL 的那组（`DB_SCRIPTS`，须先 `make migrate`） | 是 |
| `make check-all` | `bash scripts/check-all.sh --with-db` | 上两组全跑 | 是 |
| `make test-race` | `go test -race -p 4 ...` | 五个核心域的竞态检测（`pkg/`、`pipeline`、`builder`、`publication`、`presentation`），超时 20 分钟 | 否 |
| `make test` | `go test -p $(nproc) ./... -count=1` | 全量（feature + unit），可用 `TEST_PARALLEL=8` 覆盖并发 | 部分用例 |
| `make test-short` | `go test ./pkg/... ./internal/... ./cmd/... ./config/... ./public/migrations/... -count=1` | 不依赖数据库的包 | 否 |
| `make lint` | `gofmt -l internal/ pkg/ public/ cmd/ config/` + `go vet ./...` | 格式与静态检查 | 否 |

统一入口脚本是 [`scripts/check-all.sh`](scripts/check-all.sh)，模式为默认（无依赖组）/ `--db-only` / `--with-db`，末尾打印「✓ 全部门禁通过（模式，共 N 个脚本）」。
**具体脚本清单以 `scripts/check-all.sh` 的 `PLAIN_SCRIPTS` / `DB_SCRIPTS` 为准**，本文件不抄名单（会随增删过期）；脚本清单与用途见 [`scripts/README.md`](scripts/README.md)。

每个脚本拦什么、判据是什么、豁免怎么记账 → 见各脚本的说明与 `scripts/check-all.sh`。