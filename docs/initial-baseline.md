# 初始化基线收敛：设计与现状

**状态：阶段 1（结构快照）与阶段 2（真基线）均已落地，五步验收可复跑。**
本文是这份收敛的唯一判据来源。任务书 [TASK-framework-foundation.md](../TASK-framework-foundation.md) 只引用，不复制。

## 1. 现状事实（实测）

- 初始化 = 跑完 `public/migrations` 里注册的 186 条迁移与 440 条 seed（目录下 702 个 .sql）。迁移执行器**没有版本台账**：每条迁移自带 `CheckSQL` 幂等检查，命中即跳过（[public/migrations/migrator.go](../public/migrations/migrator.go)）。`sys_migration_marker` 只记「一次性数据动作」，不是版本表。
- 隔离库实测：`go run ./cmd -migrate-only` 从空库到终态约 53 秒，退出码 0。
- 接入基线的库实测：同一条命令只打印 `skipped=186 total=186`（迁移）与 `skipped=440 total=440`（seed），业务日志在 `public/logs/init/app-<date>.log`。
- 结构快照 [schema/CURRENT.sql](../schema/CURRENT.sql) 由 [scripts/schema-snapshot.sh](../scripts/schema-snapshot.sh) 生成，来源是「跑完本仓迁移的库」，当前 132 张表 / 10375 行。
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

- 快照只与「跑完本仓迁移的库」比对，**不与 `config.yaml` 的业务库比对**。业务库里可能混着别的应用的表与本仓无关对象，实测 6315 行噪声。
- 排除属主、授权、注释、`sys_migration_marker`（执行台账，不是结构）与 pg_dump 的随机标记，以及「生成时间」注释，否则每次生成都不同。
- 结构快照是**回归判据**，不是初始化脚本：它不含数据，也不含分区窗口的正确生成规则。

## 3. 阶段 2：真基线（已落地）

目标：新环境初始化 = 载入基线 + 只跑基线之后的增量；终态与全量迁移逐行一致。

### 3.1 产物与生成

`schema/baseline/` 四件，缺一不可：

| 文件 | 内容 |
|---|---|
| `structure.sql` | 非分区对象全量（表/列/索引/约束/扩展/函数/触发器/RLS/default 分区，含纯 DDL 迁移建的 `ai_*` 六张表）+ 3 张分区父表结构，剔除全部 `_YYYY_MM` 月分区 |
| `system_data.sql` | **迁移与 seed 都跑完**的系统数据（词条、权限、菜单、字典、默认配置、初始授权），不含业务数据 |
| `manifest.txt` | 两段：`[migrations]` 186 条 + `[seeds]` 440 条，段内顺序即执行顺序 |
| `init.sql` | 载入结构 → 载入系统数据 → 写 `sys_baseline` 一行（版本 `baseline-<内容摘要前 8 位>`） |

生成与校验：

```bash
bash scripts/baseline-generate.sh                  # 自建临时库跑 -migrate-only（含 seed），生成四件，结束删库
bash scripts/baseline-generate.sh --check          # 前缀 + 摘要自洽；注册表等于冻结清单时另做全量重生成比对
bash scripts/baseline-generate.sh --check --static # 只做无库检查（check-all 里的 check-baseline-artifacts.sh 就是这条）
```

`--check` 有三条判据（第二十四轮修正：旧版把「新增迁移」误报成「基线过期」）：

1. `manifest.txt` 两段各自是当前注册表的**前缀**（[cmd/baseline-manifest](../cmd/baseline-manifest/main.go) 的 `-check-prefix`）。段外新增版本按设计不进基线、照常执行，不算过期；只有顺序错位、条目消失或已冻结条目被改写才报红。
2. `init.sql` 登记的摘要与版本必须与产物内容一致 —— 手改 SQL 而不重新生成会被这条抓住，且不需要建库。
3. 仅当注册表等于冻结清单时，才自建临时库重跑全量并逐份比对（抓「基线 SQL 内容漂移」）。有段外新增版本时不比：「基线＋增量」与历史全量的终态等价由 [scripts/baseline-verify.sh](../scripts/baseline-verify.sh) 判定 —— 不能要求每加一条迁移就重生成基线、刷所有库水位。

`--check` 归一两处噪声：头部「生成时间」注释、系统数据里的时间字面量（时间列由生成时刻的 `now()` 写入）。不归一的话，换一次生成或换一天都会误报过期。

当前产物内容摘要：`8a6d0f78…`（每次重生成都会变，不作为判据）。

### 3.2 为什么导 seed 后终态，以及 manifest 为什么是前缀

旧版（第二十一轮及以前）导「迁移后、seed 前」状态，靠 `cmd -skip-seeds`。理由是：基线已含某条 seed 的新键 → 该 seed 的 `ConditionSQL` 命中跳过 → 同一条 seed 里的 DELETE 不执行（实测 618 让旧词条多 4 行）。

那个理由只在「重放基线后还要把 440 条 seed 全跑一遍」时成立。现在 manifest 同时冻结迁移与 seed 两段，基线库载入后两段都跳过 —— seed 的输入与全量路径完全相同，不再有机会分叉：

- 全量路径：186 条迁移 → 440 条 seed，各自跑到终态；
- 基线路径：`init.sql`（两段的终态）+ 段外增量，seed 段整体跳过。

`-skip-seeds` 因此删除。

两段都是**注册表的前缀**：段内顺序即执行顺序，新增版本追加在段尾、不在基线里，照常执行。这条是「基线 + 增量」与「一改迁移就得重生成、重生成就换哈希、换哈希就拒绝运行」的分界：只要不改已有迁移与系统数据，新增迁移不改变内容摘要，已接入的库继续命中跳过。

### 3.3 跳过判定（三档）

`baselineState`（[public/migrations/baseline_skip.go](../public/migrations/baseline_skip.go)）读 manifest 与 `sys_baseline` 最新一行，分三档：

| 情形 | 行为 |
|---|---|
| 无 manifest / 无 `sys_baseline` 表 / 表在但水位为空 | 全量执行（新库与未接入的旧库） |
| 水位 `manifest_sha256` == `ContentSHA256()` | 跳过 manifest 两段，执行段外增量 |
| 水位存在但摘要不等 | **报错退出**，提示「基线产物已变更」与处置办法 |
| 水位查询本身失败 | **报错退出**，不当作「没有基线」—— 那会让已有结构的库重新跑历史链 |

摘要覆盖内容，不只是清单：`ContentSHA256()` 依次喂 manifest 原文、`--structure--`、structure.sql、`--system-data--`、系统数据（时间字面量归一、剥生成头）。只比 version 清单的话，基线 SQL 内容变了仍会被判「已覆盖」，跳过的迁移永远不会补上。

第三、四档都**不回落全量**。历史迁移假定从空库开始，实证：`164_content_template_is_default.sql` 的 UPDATE 引用 `updated_at`，而 223 已 `DROP` 它建的 `idx_content_templates_default_per_type`、201/205 已把列改名 `update_time`；在终态结构的库上重放，164 的 `CheckSQL` 判不到索引 → 执行 → 撞 `column "updated_at" does not exist (SQLSTATE 42703)`。基线内容变了意味着迁移链形状变了，正确处置是核对差异、追平结构后 `baseline-adopt.sh --refresh --apply` —— 不是删掉水位行试跑（那会半路失败）。

存在性用 `to_regclass('public.sys_baseline')` 查询而不是 `Migrator().HasTable`：后者只有 bool，连接或权限出错时返回 false，已有结构的库会被当成新库重新进入历史链。

`sys_baseline.manifest_sha256` 这个列名沿用历史，语义是**产物内容摘要**（不是清单摘要）。

### 3.4 分区

基线里不写月分区名；初始化最后调用 `partition.EnsureAhead` 补齐 `-1..+3` 与 `_default`。`-migrate-only` 也会跑这一步，所以同一天建的基线库与全量库月分区窗口相同。

### 3.5 现有库接入

```bash
bash scripts/baseline-adopt.sh --db DBNAME                     # 只核验
bash scripts/baseline-adopt.sh --db DBNAME --apply             # 核验通过后写水位
bash scripts/baseline-adopt.sh --db DBNAME --refresh --apply   # 基线产物变更后更新既有水位
```

核验四条，任一不过都不写库；核验过程中的 **SQL 错误一律拒绝接入**（见下方「错误不吞」）：

1. `sys_baseline` 不存在或只有一行（多行判失败；已接入的库要更新水位用 `--refresh`）；
2. 结构与「重放 `init.sql` 再跑 `-migrate-only` 的临时库」一致 —— 两侧都是 `pg_dump` 输出，比较口径对称；先做**混库检测**，现有库多出的表直接点名并拒绝（混了别的应用对象，接入等于把外来结构当基线固化）；
3. `system_data.sql` 里每张表的行数不多于现有库（少了说明 seed 没跑全）；
4. 必需业务键：词条 `(item_key, lang)`、权限 `permission_code`、菜单非空 `permission_code` 在基线里的每一个键都必须存在于现有库（anti-join 为 0）。行数够不代表键齐 —— 缺一个必需键、同时多一条无关记录，行数照样相等。

水位摘要用 `go run ./cmd/baseline-digest`（与迁移器同源），脚本不自己算，避免口径漂移。

**错误不吞**：必需键核验的两次查询原用 `|| true` 调用，失败时输出为空 → anti-join 看起来「没有缺键」→ 脚本报「键全部存在」并以 0 退出（评审 2026-11-15 用不连库的替身探针复现）。现在函数自己记 `key_check_failed`，行数查询失败也直接 fail；[scripts/tests/test_baseline_adopt_guard.py](../scripts/tests/test_baseline_adopt_guard.py) 用假 `psql`/`pg_dump`/`go` 锁住「键查询失败 / COPY 失败 / 确有缺键 / 行数查询失败 / 正常通过」五种结果，已进 check-all 无依赖组。

实测：`artex` 被混库检测拒绝（多出 60+ 张其他项目表与 `pgcrypto`/`vector` 扩展），不接入基线；`wp` 2026-11-15 接入，同日基线改为「冻结前缀 + 内容摘要」后用 `--refresh` 更新水位（`8a6d0f78…`），接入后 `-migrate-only` 实测 `skipped=186 total=186` / `skipped=440 total=440`。

### 3.6 验收

```bash
bash scripts/baseline-verify.sh          # 建两个临时库，约 4 分钟
bash scripts/baseline-verify.sh --keep   # 保留库与 /tmp/wp-baseline-verify-* 日志
```

五项（前四条是判据，第五条是口径钉）：

1. **等价**：空库全量迁移 与「基线库（`init.sql` + 迁移）」结构一致（月分区除外）、**系统数据内容**一致 —— 逐表行数 + 行级 md5 聚合（时间字面量归一后排序聚合），实测 259 张表（含月分区子表）一致。只比行数证明不了终态一致。
2. **幂等**：基线库再跑一次迁移，结构逐字节不变、`sys_baseline` 仍一行。
3. **跳过生效**：匹配哈希时 `DROP` 掉 `admin_side_effect_tasks`（manifest 内 705 建）再跑迁移，它**不能**被重建 —— 这是「被覆盖的迁移确实没执行」的直接证据，而不是「执行了再靠幂等检查混过去」。
4. **拒绝重放**：篡改 `manifest_sha256` 后迁移必须失败并给出「基线产物已变更」，库结构保持不变。
5. **摘要口径**：`init.sql` 记录的内容摘要 == `cmd/baseline-digest` 输出 == `baseline.ContentSHA256()`（由 `TestBaselineInitMatchesContentDigest` 与脚本双侧钉住）。

结构比较归一 pg_dump 的两种 deparse：同一 CHECK 表达式在一阶库是 `ANY ((ARRAY[...])::text[])`，重放一次后是 `ANY (ARRAY[(...)::text])`，语义相同；实测 9 处（8 个 CHECK 约束 + 1 个部分索引）。归一是比较口径，不是放宽判据 —— 其余差异仍会报红。

### 3.7 历史迁移保持原文，修复由新迁移承接

第二十一轮曾直接改写 068/111/171 的 SQL 让它们兼容新旧列名 —— 违反 [public/CLAUDE.md](../public/CLAUDE.md)「历史迁移 SQL 不改」。**已全部回退到原文**（`1c84539e` 的逐字节版本），三条 `CheckSQL` 也回退到原文口径，修复改由三条新迁移承接：

| 新迁移 | 承接的历史对象 | 判据 |
|---|---|---|
| `762-pages-structure-indexes` | 068 的 `idx_pages_theme_alive` / `idx_pages_structure_header` / `idx_pages_structure_footer` | `coverObjects(...)`；索引列名用 `information_schema` 取实际列名的 DO 块 |
| `763-master-data-changes-time-indexes` | 111 的 `idx_master_data_changes_entity` / `idx_master_data_changes_time` | 同上 |
| `764-build-jobs-claim-index` | 171 的 `idx_build_jobs_claim`（明确不建 running：295 已 DROP） | 同上 |

登记在 [register_baseline_backports.go](../public/migrations/register_baseline_backports.go)，`register.go` 末尾调用。回退 + 新迁移承接后终态结构不变（`schema-snapshot.sh --check` 绿，仅生成时间戳变化）。

门禁 [check_sql_coverage_test.go](../public/migrations/check_sql_coverage_test.go) 分两段，判据方向相反：

- **门槛迁移（`761-ai-event-call-log-columns`）及之后新增的迁移**：本迁移 `CREATE` 的对象要么出现在自己的 `CheckSQL` 里，要么被后续迁移真的 `CREATE` / `DROP` 接管；含 `CREATE TABLE` 的建表迁移可整条判表名。
- **更早的历史迁移**：判定保持原文，只检查「不得判终态已删对象」—— `CheckSQL` 判了一个后续被 `DROP` 且未重建的对象会直接报错，因为那种迁移在终态库永远判不通过、每次启动都重放。

历史迁移不能收紧，实证：068 / 111 / 164 / 167 / 171 的 SQL 引用改名前的列（`created_at` / `updated_at`，201/205 已改成 `create_time` / `update_time`）；判定一旦变严，终态库与存量库每次启动都会重放它们并必然撞 `42703`。第一版曾把判定收紧到 047 / 049 / 054 / 055 / 067 / 074 / 075 / 086 / 088 / 091 / 127 / 164 / 167 / 169 / 170 / 175 / 246 / 307 / 626 / 628 共 20 条，`TestSharedFeedbackArtexRowsUpgrade`（终态库上重跑整条链）立刻逐个报 `column "updated_at"/"created_at" does not exist` —— 已全部回退，只有 761 与三条新迁移（762 / 763 / 764）判自己的对象。

历史迁移漏掉的对象由新迁移承接，存量库是否齐由 `baseline-adopt`（结构对称比较 + 系统数据行数与必需键）与 `schema-snapshot --check` 保证，不靠改历史判定兜。已核验：wp 接入基线的结构比较通过，说明 068 之外的历史索引在存量库都存在。

### 3.8 已知限制与维护动作

- **改已有迁移的 SQL 内容必须重生成基线**（内容摘要变），已接入的库会拒绝启动：先让库结构追平，再 `--refresh`。只新增迁移不用重生成 —— `--check` 的前缀判据保证了这一点，静态部分（前缀 + 摘要自洽）在 `check-all.sh` 无依赖组里跑。
- manifest 两段必须与注册表前缀一致，[baseline_manifest_test.go](../public/migrations/baseline_manifest_test.go) 会拦；同一条既有测试钉住 `init.sql` 的摘要与 `ContentSHA256()` 同源（shell 与 Go 各算一遍，口径漂移就红）。
- **seed 里不得放 DDL**：6 张 `ai_*` 表曾由 seed 建（纯 DDL、无数据），已改为迁移登记；判据固定为「纯 DDL 走 Migration，seed 只写数据」。
- adopt 的必需键只覆盖词条/权限/菜单三张表，其余系统表只比行数。
- 结构比较依赖 deparse 归一规则；pg_dump 改变其它表达式输出形式时会报红，补 `normalize` 规则，不要放宽成忽略 CHECK。
- `artex` 不是本仓迁移产物库（3.5 实测），不接入基线；需要基线环境请新建库载入 `schema/baseline/init.sql`。
- `schema/CURRENT.sql`（阶段 1）与基线是两份产物：前者用于回归比对含月分区全量结构，后者用于初始化。改迁移时两者都要更新。

## 4. 维护动作清单

| 你改了什么 | 要做什么 |
|---|---|
| 新增迁移 / seed | 不用动基线：两段前缀不变、摘要不变，已接入库继续跳过 |
| 改已有迁移 SQL、改系统数据 seed | `bash scripts/baseline-generate.sh` → 库结构追平后 `baseline-adopt.sh --db X --refresh --apply` |
| 改结构（新表/列/索引） | 同上，另加 `bash scripts/schema-snapshot.sh` |
| 新环境 | `psql -f schema/baseline/init.sql` → `./app -migrate-only` |
