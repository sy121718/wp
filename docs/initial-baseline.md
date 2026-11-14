# 初始化基线收敛：设计与现状

**状态：阶段 1（结构快照）与阶段 2（真基线）均已落地，验收脚本可复跑。**
本文是这份收敛的唯一判据来源。任务书 [TASK-framework-foundation.md](../TASK-framework-foundation.md) 只引用，不复制。

## 1. 现状事实（实测）

- 初始化 = 跑完 `public/migrations` 的 701 个 SQL 文件（177 条注册迁移 + seed）。迁移执行器**没有版本台账**：每条迁移自带 `CheckSQL` 幂等检查，命中即跳过（[public/migrations/migrator.go](../public/migrations/migrator.go)）。`sys_migration_marker` 只记「一次性数据动作」，不是版本表。
- 隔离库实测：`go run ./cmd -migrate-only` 从空库到终态约 53 秒，退出码 0。
- 结构快照 [schema/CURRENT.sql](../schema/CURRENT.sql) 由 [scripts/schema-snapshot.sh](../scripts/schema-snapshot.sh) 生成，来源是「跑完本仓迁移的库」，当前 131 张表 / 10320 行。
- 分区表只有 3 张：`page_views`、`inventory_stock_movements`、`master_data_changes`（[173_partition_append_only_tables.sql](../public/migrations/173_partition_append_only_tables.sql)）。月分区窗口由两处生成：迁移 173 的 DO 块（建库时）+ [pkg/database/partition](../pkg/database/partition/partition.go) 的 `EnsureAhead`（启动与 `-migrate-only` 都会跑），范围 `AheadMonths=3`，即当前月 -1 至 +3，另有一张 `_default` 兜底。
- 因此**月分区名随生成时间漂移**（实测快照里是 2026_08/09 窗口，终态是 2026_10 ~ 2027_02），任何「含月分区的结构快照」都不能当基线。

## 2. 阶段 1：结构快照（已落地）

用法：

```bash
# 自建临时库、跑全量迁移、比对（默认；退出码即结论）
bash scripts/schema-snapshot.sh --check

# 复用已有终态库
GOWP_SNAPSHOT_DB=wp_baseline_probe bash scripts/schema-snapshot.sh --check

# 重新生成快照
GOWP_SNAPSHOT_DB=wp_baseline_probe bash scripts/schema-snapshot.sh
```

约束：

- 快照只与「跑完本仓迁移的库」比对，**不与 `config.yaml` 的业务库比对**。artex 里混着别的应用的表（`tools` / `task_templates` / `traffic_evidence_snapshots`）与本仓无关对象（`plugin_migration_ledger` / `analytics_daily_stats` 在迁移链里没有 CREATE），实测 6315 行噪声。
- 排除属主、授权、注释、`sys_migration_marker`（执行台账，不是结构）与 pg_dump 的随机标记，否则每次生成都不同。
- 结构快照是**回归判据**，不是初始化脚本：它不含数据，也不含分区窗口的正确生成规则。

## 3. 阶段 2：真基线（已落地）

目标：新环境初始化 = 载入基线 + 只跑基线之后的增量；终态与全量迁移逐行一致。

### 3.1 产物与生成

`schema/baseline/` 四件，缺一不可：

| 文件 | 内容 |
|---|---|
| `structure.sql` | 非分区对象全量（表/列/索引/约束/扩展/函数/触发器/RLS/default 分区）+ 3 张分区父表结构，剔除全部 `_YYYY_MM` 月分区 |
| `system_data.sql` | **迁移阶段产生的**系统数据（词条、权限、菜单、字典、默认配置），不含业务数据，也不含 seed 的产物 |
| `manifest.txt` | 基线覆盖的 177 个迁移 version，逐行一条，顺序即注册顺序 |
| `init.sql` | `\ir structure.sql` → `\ir system_data.sql` → 写 `sys_baseline` 一行 |

生成与校验：

```bash
bash scripts/baseline-generate.sh          # 自建临时库跑 -migrate-only -skip-seeds，生成四件，结束删库
bash scripts/baseline-generate.sh --check  # 产物与当前代码不一致则退出码 1（改过迁移必须重生成）
```

`--check` 的比对对象是「一阶」结果（跑完迁移的库直出结构），与产物生成路径完全一致，所以逐字节可比；只有「重放基线后的库」才会出现下面 3.6 的 deparse 差异。

### 3.2 为什么基线必须导「seed 前」状态

迁移与 seed 是两个阶段：`Run` 跑完全部 Migration，之后 `RunSeeds` 才跑 Seed。基线若导 seed 之后的终态，重放基线的库再跑 seed 时条件判断会变：

- 618 的 `ConditionSQL` 只判「新键 `admin.product_pricing.applied` 是否已有 2 行」；
- 基线已含这 2 行 → 整条 seed 跳过 → 同一条 seed 里的 `DELETE`（删旧 key `.lead` / `.tail`）不执行；
- 实测基线库比全量库多 4 行旧词条（2 个 key × 2 语言）。

导「seed 前」状态后，两边 seed 阶段的输入完全相同，终态一致。`cmd/main.go` 为此增加 `-skip-seeds`（只与 `-migrate-only` 一起用，只为基线生成）。

### 3.3 跳过判定

`baselineSkipSet`（[public/migrations/baseline_skip.go](../public/migrations/baseline_skip.go)）三条全满足才跳过 manifest 内的 version：

1. 仓库 manifest 非空；
2. 库里有 `sys_baseline` 表且至少一行；
3. 最新一行的 `manifest_sha256` 与仓库 manifest 的 SHA-256 相等。

任一不满足 → 返回 nil，回落全量迁移（安全默认）。**比哈希而不是比 version 列表**：manifest、structure.sql、system_data.sql 是同一批产物，只比 version 的话基线 SQL 内容变了也会被误判成「已覆盖」。

跳过不写任何台账（本来就没有版本台账）；seed 每次照跑，靠自身 `ConditionSQL` 幂等。

### 3.4 分区

基线里不写月分区名；初始化最后调用 `partition.EnsureAhead` 补齐 `-1..+3` 与 `_default`。`-migrate-only` 也会跑这一步，所以同一天建的基线库与全量库月分区窗口相同。

### 3.5 现有库接入

```bash
bash scripts/baseline-adopt.sh --db DBNAME            # 只核验
bash scripts/baseline-adopt.sh --db DBNAME --apply    # 核验通过后写水位
```

核验三条，任一不过都不写库：

1. `sys_baseline` 不存在或为空（已接入过的库不改水位）；
2. 库结构与「重放 `init.sql` 再跑 `-migrate-only` 的临时库」一致 —— 两侧都是 `pg_dump` 输出，比较口径对称，deparse 差异继续按 3.6 归一；
3. `system_data.sql` 里每张表的行数不多于现有库（少了说明 seed 没跑全）。

第 2 条先做**混库检测**：现有库若有重放库里不存在的表，直接判「不是本仓迁移产物库」并点名表名，不再往下比 —— 这类库混了别的应用对象，接入基线等于把外来结构当成基线的一部分固化。

实测（2026-11-15）：`artex` 被拒在混库检测 —— 多出 60+ 张其他项目的表（`conversations` / `findings` / `tools` / `tasks` / `assets` / `llm_profiles` / `mcp_servers` 等），另有 `pgcrypto` / `vector` 扩展与 `sync_task_asset_links` 等外来函数。该库不接入基线；要接入请用只跑过本仓迁移的库，或新建库载入基线。

### 3.6 验收

```bash
bash scripts/baseline-verify.sh          # 建两个临时库，约 2 分钟
bash scripts/baseline-verify.sh --keep   # 保留库与 /tmp/wp-baseline-verify-* 日志
```

三条判据：

1. **等价**：空库全量迁移 与 基线库（`init.sql` + 迁移）结构一致（月分区除外）、逐表行数一致（`sys_baseline` 除外）；
2. **幂等**：基线库再跑一次迁移，结构逐字节不变、`sys_baseline` 仍只有一行；
3. **回落**：篡改 `sys_baseline.manifest_sha256` 并删掉 `admin_side_effect_tasks`（由 manifest 内迁移 705 建）后再跑迁移，该表必须被重建。

实测结论（2026-11-15）：三条全绿。结构比较归一了 pg_dump 的两种 deparse —— 同一 CHECK 表达式在一阶库是 `ANY ((ARRAY[...])::text[])`，重放一次后是 `ANY (ARRAY[(...)::text])`，语义相同；实测全库 9 处（8 个 CHECK 约束 + 1 个部分索引），归一是比较口径，不是放宽判据（其余差异仍会报红）。

### 3.7 验收发现的既有漂移（已修）

`760-inventory-movement-fk-restore`：173 用「重命名旧表 → 建分区父表 → 搬数据 → 删旧表」把 `inventory_stock_movements` 改成月分区表，134 加在旧表上的 `fk_inventory_movements_product` 随旧表一起消失，173 之后没有任何迁移补回。结果是**存量库有这条外键、新库没有**（实测 artex 与 wp_baseline_dev 有 7 个同名约束行，含各分区；新库 0 个）。补一条幂等迁移把两侧收敛到同一形状。

### 3.8 已知限制

- artex 不是本仓迁移产物库（3.5 实测），不接入基线；需要基线环境请新建库载入 `schema/baseline/init.sql`。
- **seed 里仍有 DDL**：6 张 `ai_*` 表由 seed 542 建（`registerSeed` + `CREATE TABLE`），所以结构基线（`-skip-seeds` 生成）不含它们，靠初始化时跑 seed 补齐。这是既有形状，本次不改历史 seed；新数据/结构应分别落在 Migration 与 Seed。
- 结构比较依赖 deparse 归一规则。将来 pg_dump 若改变其它表达式的输出形式，验收会报红，需要在 `scripts/baseline-verify.sh` 的 `normalize` 里补规则，**不要放宽成忽略 CHECK**。
- manifest 必须与注册表同序同集：改迁移就要重生成基线，[public/migrations/baseline_manifest_test.go](../public/migrations/baseline_manifest_test.go) 会拦。
- `schema/CURRENT.sql`（阶段 1）与基线是两份产物：前者用于回归比对含月分区全量结构，后者用于初始化。改迁移时两者都要更新。
