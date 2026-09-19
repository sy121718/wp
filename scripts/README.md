# scripts 目录说明

本目录保留构建、部署和辅助脚本。

## 当前状态

现有前端打包脚本来自基础框架迁移，但当前仓库不包含它们依赖的 `web/` 前端源码，也没有脚本引用的 GoReleaser 配置。因此以下脚本暂不属于可用发布链路：

- `build-all.sh` / `build-all.ps1`
- `build-frontend.sh` / `build-frontend.ps1`
- `prepare-embed.sh` / `prepare-embed.ps1`
- `deploy.sh`

在前端源码位置、嵌入策略和发布产物格式确定前，不应使用这些脚本生成正式发布包。

当前检查入口：

```bash
go test ./...
go build ./...
go vet ./...
bash scripts/check-workbench.sh
bash scripts/check-permission-gaps.sh   # 需要本地数据库
```

`check-permission-gaps.sh` 审计「有路由、但没有对应权限点」的接口：把运行时装配出来的路由表（`WP_DUMP_ROUTES=1 go test ./internal/routers/`）与 `sys_permission` 比对。这类接口在 `authorizedAPI` 组下会让**任何账号（含超管）**都被拒 403 —— 新增接口忘了 seed 权限点时，它是最便宜的发现手段。有意豁免的接口（登录 / 验证码 / 当前用户资料）列在脚本顶部。

`check-workbench.sh` 要求开发环境有 Node：检查 Go 生成契约是否漂移、工作台 ESM 能否解析及加载、公共控件资产和实际面板编辑链路。Node 不参与生产资产构建。修改对齐重复项声明后，在仓库根目录运行 `go run ./cmd/workbench-contracts` 更新生成文件。

ArtifactStore、PublicationStore 和静态交付链路已经实现；失效的旧脚本仍不代表当前发行流程。生产构建用 `go build -o app ./cmd`，后台模板和静态文件须一并交付。发行包设计见 `docs/11-foundation-and-open-source.md`。
## 巡检与守卫脚本

CI 与上线前跑的门禁（改了权限点 / service 层数据访问 / 后台文案 / 内部错误出口 / 多端 CSS / 库存 SKU 之后，本地先过一遍再推）：

| 脚本 | 拦什么 | 依赖 |
|---|---|---|
| `check-permission-gaps.sh` | 有路由但没有对应权限点（该组挂 CasbinMiddleware 按实际路径 enforce，缺权限点时**含超管全员 403**） | 本地数据库 |
| `check-service-db-boundary.sh` | 非 admin 的 service 直查数据库（**含事务回调句柄上的裸查询全族**：`.Model(` / `.Where(` / `.Create(` / `.Exec(` …）、跨模块引用 model/service | 脚本内置豁免清单 `service-db-boundary-allow.txt` |
| `check-i18n-coverage.sh` | 后台模板里的硬编码中文超出基线（基线数值在 `i18n-coverage-baseline.txt`） | 无 |
| `check-multidevice-css.sh` | 组件产出里的裸 `:hover`、写死宽度等违反多端硬规则的写法（`SKY_CSS_GUARD=error` 时是硬门禁） | 无 |
| `check-no-internal-error-leak.sh` | 后台 handler 把 `err.Error()` 直出——响应写入 / 重定向 query / 模板数据**三种形态一起判**（豁免按文件 + 形态记账） | 无 |
| `check-stock-sku-prefix-collisions.sh` | 迁移 262 剥掉仓库 SKU 的前缀后会撞成同一 `(warehouse_id, sku_code)` 的存量行（迁移会在启动时 RAISE，脚本把它提前） | psql + `config.yaml` |
| `check-inventory-sku-format.sh` | 库存侧 SKU 的**存量格式**三类问题：空编码 / 带仓码前缀 / 采购行同类（只读巡检，命中即打回给人） | psql + `config.yaml` |

每个门禁怎么跑、失败意味着什么：

- `bash scripts/check-no-internal-error-leak.sh`（无依赖，秒级）。exit 1 = 某处把内部错误原文写进了响应、重定向 query 或模板数据；改走模块的归口助手（如 `admin_err.go`）或 `shell.PageError`，确实受控的写进脚本顶部 `EXEMPT`（按**文件 + 形态**记账，条目不再命中也会失败）。
- `bash scripts/check-service-db-boundary.sh`（无外部依赖，秒级）。exit 1 = ① 非 admin 的 service 出现 `DB(ctx)`/`RevisionDB(ctx)`；①b 它在**事务回调的句柄**上直接拼查询（裸查询全族：`tx.WithContext(ctx).Model(...)` / `tx.Create(...)` / `tx.Where(...)` / `tx.Exec(...)` …）；② 跨模块 import 对方 model/service。修法都是「给 model 加具名方法」（要落在同一事务里就加 `…Tx` 变体、句柄由 service 透传）。①b 的暂缓条目写进 `scripts/service-db-boundary-allow.txt`，**每条必须写理由**，条目不再命中脚本会失败（只增不减等于没有门禁）；**能改成 model 具名方法的一律改，不要拿豁免记账**。**判据 ①b 是启发式**：句柄名只认 tx/txn/trx/session/dbtx/dbx/db/t，且经 model 裸句柄的直查（如 publication 的 `RouteDB(ctx)`）、先取句柄存到局部再用、`.Session(...)` 链式包装仍是同类漏网 —— 脚本绿 ≠ 边界干净。
- `bash scripts/check-stock-sku-prefix-collisions.sh` 与 `bash scripts/check-inventory-sku-format.sh`（需要 `psql`，库连接从 `config.yaml` 的 `database` 段读取，脚本里不另存口令）。两个都是**只读 SELECT**：exit 1 = 有命中（明细已打印），先把人拉进来定「留哪一行、改成什么码」——迁移与脚本都不做自动改码（自动加后缀 / 静默合并 / 丢行都是数据篡改）。第一个要在跑迁移 262 之前跑，第二个用于日常巡检存量。

运维脚本：

- `rls-role-setup.sh <角色> <密码>` —— 建非超级角色并授权（含 `ALTER DEFAULT PRIVILEGES`，让将来新建的表也自动授权）。这是 DB-009 把 `config.yaml` 的 `database.user` 换成非超级用户之前的准备步骤；**顺序不能反**，先换角色会让未包工程作用域的路径静默返回 0 行。
- `index-usage-report.sh` —— 索引使用量快照与增量对比（审计 IDX-018）。无参数打印当前快照；`--save <名字>` 建基线、`--diff <名字>` 列出零增量候选、`--list` 列已有快照。**单次快照没有区分度**：空表上连唯一约束索引都是 0 次扫描（本地实测 376 个索引里 206 个零增量），真正有判据的是「生产库上间隔一个完整业务周期的两次快照之差」。增量为零只意味着进入待复核清单，删除前必须逐个确认：没有唯一 / 主键约束语义、没有 EXPLAIN 断言测试依赖它、且有替代索引覆盖同一查询面。快照落在 `public/logs/index-usage/`（已在 `.gitignore` 内）。
