# 初始化基线收敛：设计与现状

**状态：阶段 1 已落地；阶段 2（真基线）仅设计，未实施。**
本文是这份收敛的唯一判据来源。任务书 [TASK-framework-foundation.md](../TASK-framework-foundation.md) 只引用，不复制。

## 1. 现状事实（实测）

- 初始化 = 跑完 `public/migrations` 的 699 个迁移。迁移执行器**没有版本台账**：每条迁移自带 `checkSQL` 幂等检查，命中即跳过（[public/migrations/migrator.go](../public/migrations/migrator.go)）。`sys_migration_marker` 只记「一次性数据动作」，不是版本表。
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

## 3. 阶段 2：真基线（设计，未实施）

目标：新环境初始化 = 载入基线 + 只跑基线之后的增量；不再重演 699 条历史修补。终态必须与全量迁移一致。

三部分：

1. **结构基线** `schema/baseline/*.sql`：非分区对象全量（表/列/索引/约束/扩展/函数/触发器/RLS/default 分区）+ 3 张分区父表结构，**剔除全部 `_YYYY_MM` 月分区**。
2. **系统数据基线**：只导通用初始数据 —— 有效词条（`sys_i18n`）、权限目录（`sys_permission`）、默认菜单（`sys_menus`）、字典、默认配置。**不得从 artex 全库导出**：用户、订单、密钥、自定义译文、角色授权都不是默认值。
3. **增量**：基线之后新增的迁移照常注册与执行。

版本跳过机制（关键设计）：

- 迁移版本号是字符串（`596-sys-migration-marker`），不能按字符串排序判先后；**顺序权威是注册表的注册序列**。
- 因此基线生成时产出 `schema/baseline/manifest.txt`：逐行列出基线覆盖的全部 version。
- 基线库建 `sys_baseline(version, manifest_sha256, applied_at, note)`；执行器读到该行时，跳过 manifest 中的 version，只跑新增项。
- `manifest.txt` 缺失或哈希不匹配 → 视为未载入基线，回落到全量迁移（安全默认）。

分区规则（必须）：基线里不写月分区名，初始化最后调用 `partition.EnsureAhead` 补齐 `-1..+3` 窗口与 `_default`。否则换个月份初始化，基线就与运行期不一致。

现有库接入（不丢数据）：

1. 先核验：表/列/索引/约束/触发器/RLS/分区父表都达到基线（逐项比对，不能只看「表存在」）。
2. 通过后写入 `sys_baseline` 记录，manifest 取仓库当前版本。
3. 核验失败 → 不接入，继续走全量迁移并报差异清单。

验收（全部满足才算完成）：

- 基线库 + 增量 与 全量迁移库 的结构 diff 为空（分区窗口按同一月份对齐后比较）。
- 系统数据逐表 diff 为空（词条、权限、菜单、字典、默认配置）。
- 同一基线重复执行幂等：结构不变、数据不增、`sys_baseline` 不重写。
- 现有库接入前后：业务表行数不变，结构 diff 为空。

## 4. 未实施清单

- `schema/baseline/` 目录、`init.sql`、系统数据快照、`manifest.txt` 均未生成。
- `sys_baseline` 表与迁移器跳过逻辑未实现。
- 现有库（artex）接入核验未执行。
- 基线结构与数据的一致性测试未写。

在以上未完成前，**初始化仍以全量迁移为准**，本文的阶段 1 快照只用于回归对比。
