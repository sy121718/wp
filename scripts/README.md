# scripts 目录说明

本目录保留构建、部署和辅助脚本。

## 构建与发布

本仓**没有前端打包链路**：模板与静态资产由 `internal/templates/static_embed.go` 的 `go:embed` 打进二进制，
发布只需 `go build -o app ./cmd`。`deploy.sh` 是部署辅助，同样不生成前端产物。

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

生产构建用 `go build -o app ./cmd`，后台模板和静态文件须一并交付。

## 巡检与守卫脚本

CI 与上线前跑的门禁（改了权限点 / service 层数据访问 / 后台文案 / 内部错误出口 / 多端 CSS / 库存 SKU 之后，本地先过一遍再推）：

| 脚本 | 拦什么 | 依赖 |
|---|---|---|
| `check-permission-gaps.sh` | 有路由但没有对应权限点（该组挂 CasbinMiddleware 按实际路径 enforce，缺权限点时**含超管全员 403**） | 本地数据库 |
| `check-service-db-boundary.sh` | 非 admin 的 service 直查数据库（**含事务回调句柄上的裸查询全族**：`.Model(` / `.Where(` / `.Create(` / `.Exec(` …）、跨模块引用 model/service | 脚本内置豁免清单 `service-db-boundary-allow.txt` |
| `check-i18n-coverage.sh` | 后台模板里的硬编码中文超出基线（基线数值在 `i18n-coverage-baseline.txt`） | 无 |
| `check-multidevice-css.sh` | 组件产出里的裸 `:hover`、写死宽度等违反多端硬规则的写法（`SKY_CSS_GUARD=error` 时是硬门禁） | 无 |
| `check-no-internal-error-leak.sh` | 后台 handler 把 `err.Error()` 直出——响应写入 / 重定向 query / 模板数据**三种形态一起判**（豁免按文件 + 形态记账） | 无 |
| `check-stock-sku-prefix-collisions.sh` | 仓库 SKU 带前缀时，剥前缀会撞成同一 `(warehouse_id, sku_code)` 的存量行；脚本提前把冲突列出来 | psql + `config.yaml` |
| `check-inventory-sku-format.sh` | 库存侧 SKU 的**存量格式**三类问题：空编码 / 带仓码前缀 / 采购行同类（只读巡检，命中即打回给人） | psql + `config.yaml` |
| `check-htmx-api.sh` | htmx 4 的静默失效面：v2 已删属性（`hx-disabled-elt` 等）、v2 驼峰事件名、v2 扩展 API，以及**反向核对**模板里每个 `hx-*` 是否真在 vendored bundle 里存在 | 无 |
| `check-js-controls.sh` | 前端控件的 `*.test.cjs` 行为测试（逐文件串行，避免 vm 夹具互相干扰） | node |

每个门禁怎么跑、失败意味着什么：

- `bash scripts/check-no-internal-error-leak.sh`（无依赖，秒级）。exit 1 = 某处把内部错误原文写进了响应、重定向 query 或模板数据。修法是走**统一 JSON 错误出口**（业务错误在声明处给 key + 中文兜底 + 状态码，出口取词产生 `message`，见 [AGENTS.md](../AGENTS.md)「取词与文案」），不把 `err.Error()` 直出给用户。确实受控的写进脚本顶部 `EXEMPT`（按**文件 + 形态**记账，条目不再命中也会失败）。
- `bash scripts/check-service-db-boundary.sh`（无外部依赖，秒级）。exit 1 = ① 非 admin 的 service 出现 `DB(ctx)`/`RevisionDB(ctx)`；①b 它在**事务回调的句柄**上直接拼查询（裸查询全族：`tx.WithContext(ctx).Model(...)` / `tx.Create(...)` / `tx.Where(...)` / `tx.Exec(...)` …）；② 跨模块 import 对方 model/service。修法都是「给 model 加具名方法」（要落在同一事务里就加 `…Tx` 变体、句柄由 service 透传）。①b 的暂缓条目写进 `scripts/service-db-boundary-allow.txt`，**每条必须写理由**，条目不再命中脚本会失败（只增不减等于没有门禁）；**能改成 model 具名方法的一律改，不要拿豁免记账**。**判据 ①b 是启发式**：句柄名只认 tx/txn/trx/session/dbtx/dbx/db/t，且经 model 裸句柄的直查（如 publication 的 `RouteDB(ctx)`）、先取句柄存到局部再用、`.Session(...)` 链式包装仍是同类漏网 —— 脚本绿 ≠ 边界干净。
- `bash scripts/check-htmx-api.sh`（无依赖，秒级）。exit 1 = 模板或控件 JS 里出现了 htmx 4 不认的写法。这类错误的共同特征是**静默**：不报错、不 500、日志干净，只是那段交互从此不工作。本项目踩过两次——product 目录 9 处 `hx-disabled-elt`（实测对照：用旧属性时请求期间按钮 `disabled` 恒为 false，改 `hx-disable` 才 true），以及 `vendor/idiomorph/idiomorph-ext.min.js`（用 htmx 2 的 `htmx.defineExtension`，v4 下加载即 `TypeError`）。判据 4 是反向核对：从 bundle 抽出它认识的 `hx-*` 全集与模板求差集，抓的是「以为支持其实不支持」；它比「bundle 里 grep 得到该字符串」严格得多（后者的松判据实测会把 `hx-get` 误判成缺失）。排除名单只有 `hx-progress` / `hx-progress-slide`（本项目自定义 CSS 类）。
- `bash scripts/check-stock-sku-prefix-collisions.sh` 与 `bash scripts/check-inventory-sku-format.sh`（需要 `psql`，库连接从 `config.yaml` 的 `database` 段读取，脚本里不另存口令）。两个都是**只读 SELECT**：exit 1 = 有命中（明细已打印），先把人拉进来定「留哪一行、改成什么码」——迁移与脚本都不做自动改码（自动加后缀 / 静默合并 / 丢行都是数据篡改）。第一个要在跑迁移 262 之前跑，第二个用于日常巡检存量。

运维脚本：

- `rls-role-setup.sh <角色> <密码>` —— 建非超级角色并授权（含 `ALTER DEFAULT PRIVILEGES`，让将来新建的表也自动授权）。这是 DB-009 把 `config.yaml` 的 `database.user` 换成非超级用户之前的准备步骤；**顺序不能反**，先换角色会让未包工程作用域的路径静默返回 0 行。
### 结构与现有库接入

数据库结构以 [`public/schema/`](../public/schema/) 为唯一来源：一表一文件，改结构直接改表文件；`schema.Apply` 载入后写 `sys_baseline` 水位，同摘要跳过、摘要不符拒绝重放。

- `schema-adopt.sh --db DBNAME [--apply | --refresh --apply]` —— 现有库接入：混库检测（现有库多出的表直接判「不是本仓结构产物库」并点名）→ 与「空库载入 `public/schema` 再跑 `-migrate-only` 的临时库」对称比较结构（月分区与 pg_dump deparse 差异已归一）→ **必需业务键 anti-join**（词条 `(item_key, lang)` / 权限 / 菜单非空 `permission_code`）。水位摘要取自 `cmd/schema-digest`，与 `schema.Apply` 同源。默认只核验不写库；核验期间的 SQL 错误一律拒绝接入，回归用例 `tests/test_schema_adopt_guard.py` 用替身 `psql`/`pg_dump`/`go` 锁住失败面。
- `tests/test_schema_package.sh` —— `public/schema` 的静态门禁：包内测试（段序 / 切分 / `order.txt` 覆盖）与摘要稳定性，已进 `check-all.sh` 无依赖组。

### 环境清理

- `cleanup-test-dbs.sh [--probes] [--apply]` —— 列出并（带 `--apply` 时）删除夹具与历轮审计留下的临时库。默认模式匹配随机后缀库（`rls_t_*` / `t_*` / `go_test_*` / `wp_test_tpl_*`）；`--probes` 匹配 `wp_` 前缀的一次性探针库但保留 `wp_test*` 测试主库。两种模式都不碰业务库（`config.yaml` 的 `database.dbname`）、`postgres` 与 `artex` 系列（`artex_mig2` 是本仓的迁移验收库）。默认只列名单与体积。2026-11-15 实测：夹具库 340 个 / 5.7 GB、探针库 13 个 / 274 MB。

### 备份、恢复与视觉基线

- 备份：`bash scripts/backup.sh` → `.backup/<时间戳>/{db,site,manifest.env,SHA256SUMS}`。内容是数据库 `-Fc` dump + 每表行数快照 + `pg_dump` 日志 + 站点产物与**激活符号链接**（`tar -h` 打包，否则恢复后站点 404）+ 上传资产 + 版本/提交/迁移台账 + 校验和。环境变量 `DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME`（也接受 `GOWP_` 前缀）、`ARTIFACT_ROOT`、`STORAGE_DIR`、`BACKUP_DIR`。
- 演练：`bash scripts/restore.sh --drill .backup/<时间戳>` —— 校验和 → 建临时库 → `pg_restore --exit-on-error --single-transaction` → 逐表对账行数 → 删临时库，**全程不碰生产库**。**没跑过 `--drill` 的备份不算备份。**
- 灾难恢复：先手工 `createdb <新库>`（脚本不替你 DROP 生产库），再 `bash scripts/restore.sh --to <新库> .backup/<时间戳>`、`bash scripts/restore.sh --site .backup/<时间戳>`，最后核对 `manifest.env` 的迁移台账与代码一致；站点资产恢复后逐个检查激活符号链接，悬空即失败。
- `visual-baseline.py`：组件截图基线。`--save` 建基线、`--check` 比对、`--only card,heading` 只渲指定组件；基线在 `testdata/visual-baseline/`，差异图在 `.visual-diff/`。**允许 0 像素差异**——令牌替换不许改取值，有差异即属「改视觉」，单独评审。两个确定性陷阱：拦掉全部 `**://**` 外部请求（否则依赖网络时延与远端内容）；把 `iframe/video/audio/object/embed` 整体摘掉（只拦请求不够，元素仍参与布局，尺寸随 abort 时机抖动）。命令见 `python3 scripts/visual-baseline.py --help`。
- `index-usage-report.sh` —— 索引使用量快照与增量对比（审计 IDX-018）。无参数打印当前快照；`--save <名字>` 建基线、`--diff <名字>` 列出零增量候选、`--list` 列已有快照。**单次快照没有区分度**：空表上连唯一约束索引都是 0 次扫描（本地实测 376 个索引里 206 个零增量），真正有判据的是「生产库上间隔一个完整业务周期的两次快照之差」。增量为零只意味着进入待复核清单，删除前必须逐个确认：没有唯一 / 主键约束语义、没有 EXPLAIN 断言测试依赖它、且有替代索引覆盖同一查询面。快照落在 `public/logs/index-usage/`（已在 `.gitignore` 内）。