# BE-queue 交付报告：menu_stale 同事务可靠入队（定点修复轮）

范围：**只做队列侧** —— `internal/module/build/{model,service,contract,dto,enums}` 的新增代码与其 tests、
迁移 **295 回填的两处比较**与 **698 及其 register**、本报告；测试源码已补齐但**本轮未运行**。
未碰 699 / page / presentation / pkg / i18n / navigation 的源码（它们在工作树里的改动属于别的 owner）。

本回复核轮（queue-type-fix）修的四件事，明细见 §4.2 / §4.4 / §5：
① 295 回填改为两端 `::text`（text = uuid 没有隐式转换，只改一侧会在 uuid 阶段失败）；
② 698 补幂等 forward 回填；
③ register 判据由五件补到七件（形状 CHECK、号源表 + 四值约束 + RLS + 策略、索引键列）；
④ `Enqueue` 对 page / presentation 的 source_id 做 UUID 归一、非法值受控 400。

主审裁定已落地：`build_jobs.source_id` 由 uuid **放宽为 text**，menu_stale 的来源标识保留
`canonicalUUID:kind`，不退回去只存裸工程 UUID（理由见 §4）。上一版报告 §6 的
「source_id 是 uuid → 插入必然失败」这条已知未解约束**已解除**。

---

## 1. 层归属表

| 改动 | 层 | 该层允许 | 实际做了什么 |
|---|---|---|---|
| `internal/module/build/model/build_model.go` | model | 本模块表 CRUD、事务内原子组合 | `SourceTypeMenuStale`、`Entity.InputVersion *int64`、`Entity.ContextKey *string`、`ErrTxRequired`、`CreateTx`、`requireTx` |
| `internal/module/build/model/build_job_seq.go`（新） | model | 同上 | 号源访问单元 `NextMenuStaleVersionTx`，**GORM clause 实现**（无裸 SQL），`seqRow` 是语句目标而非实体 |
| `internal/module/build/dto/build_resp.go` | dto | 形状与校验（`*_req.go` / `*_resp.go`） | `Job.InputVersion` 投影 + `MenuStaleSourceID` / `SplitMenuStaleSourceID`（原 `build_menu_stale.go` 已删除并入本文件） |
| `internal/module/build/enums/build_enums.go` | enums | key + 中文兜底 | `ErrInvalidMenuKind` |
| `internal/module/build/service/build_menu_stale.go`（新） | service | 事务边界（句柄下传）、规则 | `EnqueueMenuStaleTx`：kind/UUID 规则 + 取号 + 组装（含 `ContextKey`） |
| `internal/module/build/service/build_service.go` | service | 入参规则 + 投影 | `toDto` 映射 `InputVersion`；`canonicalSourceID`（UUID 归一）+ 非法值 `ErrInvalidParam`（§4.2） |
| `internal/module/build/service/build_service_test.go`（新） | 测试 | 纯逻辑 | 归一口径、非法 UUID 拒绝、menu_stale 不得从通用入口入队 |
| `internal/module/build/contract/build_service.go` | contract | 接口 + 不可变 DTO | 接口方法 + `MenuStaleSourceID` / `SplitMenuStaleSourceID` **func 包装**（不再是 `var` 重导出） |
| `public/migrations/698_navigation_menu_stale_sequence.sql` | 迁移 | 幂等 SQL | 号源表 + `source_id` 放宽为 text + 两列 + 形状 CHECK + 部分唯一索引 + 来源白名单 |
| `public/migrations/register_navigation_menu_stale_sequence.go` | 迁移 | 注册 + CheckSQL | `Version: "698-navigation-menu-stale-sequence"`，**七件**形状判据（见 §4.4） |
| `public/test/build/feature/build_menu_stale_test.go` | 测试 | 真实 PG（`support.NewMigratedPGTestDB`） | 16 条链路用例（见 §6） |
| build `model/service/dto` 的 `*_test.go` | 测试 | 纯逻辑 | 事务守卫、实体列形状、编解码往返 |

## 2. 冻结契约（实现 = 冻结签名）

```go
// build/service（*Service），同时进 contract.BuildService
func (s *Service) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB,
    projectID, kind string) (inputVersion int64, err error)

// build/dto
func MenuStaleSourceID(projectID, kind string) (sourceID string, ok bool)
func SplitMenuStaleSourceID(sourceID string) (projectID, kind string, ok bool)

// build/contract：同名 **func 包装**（不是 var 重导出）
func MenuStaleSourceID(projectID, kind string) (sourceID string, ok bool)
func SplitMenuStaleSourceID(sourceID string) (projectID, kind string, ok bool)
```

契约要点（逐条在实现里成立）：

- **只用调用方事务**：不开事务、不提交、不回滚、不设 scope。判据 `requireTx`
  （`*sql.Tx` / `*gorm.PreparedStmtTX`，形状照抄 `pkg/i18n/content_write.go` 的 requireTx），
  失败返回 `buildmodel.ErrTxRequired`。
- **contract 侧不可变**：上一版用 `var SplitMenuStaleSourceID = builddto.SplitMenuStaleSourceID`，
  任何一处 `contract.SplitMenuStaleSourceID = ...` 都能在运行期换掉全部消费方的解析规则；
  本轮改为 func 包装，并顺带把 dto 细节挡在契约后面。
- **kind 严格四值**：`header / header_mobile / footer / footer_mobile`，与 285 的
  `navigations_kind_check`、698 的 `ck_build_job_seq_kind` 逐字一致；非法 kind 在**取号之前**被拒
  （`ErrInvalidMenuKind`，400 事实）。
- **工程 uuid**：`MenuStaleSourceID` 用 `uuid.Parse(...).String()` 归一 —— 这一条在 `source_id`
  改成 text **之后更重要**：uuid 列会自动归一大小写，text 列不会，归一只能靠应用层。
- **source_type / source_id**：`menu_stale`，source_id = `<规范UUID>:<kind>`。
- **Job.ProjectID** = 规范工程 UUID；**lang 为空串**（未冻结语言），不夹带 kind。
- **版本号**：专用表 `build_job_seq(project_id, kind)`，语句是
  `INSERT … ON CONFLICT (project_id, kind) DO UPDATE SET next_version = next_version + 1
  RETURNING next_version`；不用 MAX+1 / update_time / hash（理由写在 `build_job_seq.go` 头部）。
- **hash 含版本号**：`build_input_hash = "menu_stale:{projectID}:{kind}#{version}"`，
  A→B→A 三个号三段 hash，不会与遗留 pending / running 撞键。
- **严格返错**：`CreateTx` **不用** `ON CONFLICT DO NOTHING`；唯一冲突返回
  `构建任务唯一键冲突（同一来源与输入版本已在队列中）: %w`，由调用方回滚。
- **失败一起回滚**：取号与插行在同一 tx，回滚时号随之撤销。
- **不动旧来源**：`input_version / context_key` 均可空、**不回填**；旧来源的
  `uq_build_jobs_pending` / `uq_build_jobs_running_source` 原样保留（键列与 predicate 见 §5）。
- **context_key 真的落库**：`Entity.ContextKey` 字段 + `EnqueueMenuStaleTx` 赋 `&sourceID`，
  698 的 `build_jobs_menu_stale_shape_check` 把 `context_key = source_id` 这条等式钉在库里。
  缺它会让 `uq_build_jobs_context_version` 的键里永远是 NULL —— PG 的 UNIQUE 允许多个 NULL，
  去重会静默失效且没有任何报错。本轮的 `TestEntityHasMenuStaleColumns` 与
  `TestMenuStaleAllFourKinds` 各钉一侧（结构 + 落库值）。

## 3. 架构预算：取号没有新增裸 SQL

`internal/architecture/raw_sql_boundary_test.go` 的 `rawSQLAllowedList` 对
`internal/module/build/model/build_model.go` 仍是 `Max: 1`（那条豁免属于既有的 ReclaimStale CTE），
本轮**没有**为取号另开一份。

上一版 `build_job_seq.go` 里有一条 `const nextVersionSQL` + `tx.Raw(...)`，本轮删除，改为
`clause.OnConflict` + `clause.Assignments` + `clause.Returning` + `Create(&got)` 的 GORM 形态：

- `clause.OnConflict{Columns: {project_id, kind}, DoUpdates: {next_version: gorm.Expr("next_version + 1"), update_time: gorm.Expr("now()")}}`
  → PG 侧就是 `ON CONFLICT (project_id, kind) DO UPDATE`，并发写同键由行锁串行化；
- `clause.Returning{Columns: {next_version}}` → 分配与读回同一条语句，中间没有窗口；
- 插入映射只管 `project_id / kind`，`next_version` 交给列的 `DEFAULT 1`（在 Go 里再写一遍 1 就是第二份真相）。

**目标对象的选择**（这里特意改过一次）：插入与结果用同一个对象 `got := seqRow{}`，写成
`Create(&got)`。本仓既有形态都是 `Model(&目标).Clauses(Returning).Updates(...)`
（`page_model.go:326-329`、`product_outbox_model.go:94-100`）；`Create(map…).Scan(&局部)` 时
目标不是语句对象，扫回与否取决于 gorm 内部实现，不是本模块能依赖的契约。
`seqRow` 只带 `project_id / kind / next_version` 三列，不是 `build_job_seq` 的实体
（本表在 model 侧只有取号一个用途；kind 四值、工程外键、RLS 全部由 698 表达）。

## 4. 迁移 698

**编号占用**：`public/migrations` 现存最大是 699（`699_navigation_input_version.sql`，
实体输入代次，另一侧的 owner）。698 归本批，**与主草案 §4.4 的
`698_navigation_publish_fencing.sql` 号冲突**——`publish_epoch` 起草时换号或合并，不要重复占用。

698 的五件事：

1. `CREATE TABLE IF NOT EXISTS build_job_seq`：
   `(project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE, kind varchar(32),
   next_version bigint NOT NULL DEFAULT 1, create_time/update_time timestamptz)`，
   PK `(project_id, kind)`，`ck_build_job_seq_kind` 四值 CHECK；ENABLE + FORCE RLS +
   `project_isolation` 策略（与 215/673 同形，fail closed）。
2. **`build_jobs.source_id` uuid → text**：
   ```sql
   DO $$ BEGIN
       IF (SELECT data_type FROM information_schema.columns
            WHERE table_schema = current_schema() AND table_name = 'build_jobs'
              AND column_name = 'source_id') = 'uuid' THEN
           ALTER TABLE build_jobs ALTER COLUMN source_id TYPE text USING source_id::text;
       END IF;
   END $$;
   ```
   用 DO 块守卫是因为 **PG 对同型 ALTER 直接报错**（不是空操作），不加守卫这份"幂等"迁移在
   第二次执行时就会炸。
3. `input_version bigint NULL` / `context_key text NULL`（均不回填）+ 列注释。
4. `build_jobs_menu_stale_shape_check`：`source_type <> 'menu_stale' OR (` 四段事实自洽 `)` ——
   `input_version IS NOT NULL AND context_key IS NOT NULL AND project_id IS NOT NULL
   AND left(source_id,37) = project_id::text || ':' AND substr(source_id,38) IN (四值)
   AND context_key = left(source_id,37) || substr(source_id,38)`。
   判据全用内置字符串函数，不往库里装自定义函数。
5. `uq_build_jobs_context_version ON build_jobs (context_key, input_version) WHERE source_type = 'menu_stale'`；
   以及 `build_jobs_source_type_check` 放宽为 `('page','presentation','menu_stale')`。

### 4.1 为什么是 text，而不是退回裸工程 UUID

`uq_build_jobs_running_source (source_type, source_id) WHERE status='running'` 是本队列的**来源互斥**
判据，claim 的 `NOT EXISTS (… r.source_type = build_jobs.source_type AND r.source_id = build_jobs.source_id)`
是它的前置过滤。若 menu_stale 的 source_id 只存裸工程 id，四个菜单位置共用同一个来源 ——
页眉一旦在跑，页脚就永远领不到，四位置独立重建当场失效。把位置编进 source_id 才能让互斥粒度
停在「位置」。`TestMenuStaleFourKindsRunConcurrently` 钉住这一点（四个位置各被 claim 一次）。

### 4.2 对旧来源是纯类型放宽，但「非规范来源 id」必须在入队处归一

`USING source_id::text` 逐值等价：存量值是 uuid，转 text 不改变值；列 NOT NULL，没有 NULL 风险。
改型前后的**文本逐字相同**只对 `uuid.NewString()` 这类规范形态成立（`presentation_instance.go:141`、
page 侧同模式）—— 改型**前**这一条不是必然的：`Enqueue` 当时只做 `TrimSpace`，大写 / 无连字符 /
带花括号的写法靠 uuid 列归一。

**本轮修复（692 轮复核缺口 5）**：改 text 之后数据库不再归一，同一实体的两种写法会各占一条
`uq_build_jobs_running_source`（同来源两条 running，互斥失效）与一条 `uq_build_jobs_pending`
（同一份工作两条待办，去重失效），且都不报错。处置：

- `build/service/build_service.go` 新增 `canonicalSourceID`（`uuid.Parse(...).String()`），
  `Enqueue` 在**白名单校验之后、组装实体之前**调用；非法 UUID 返回 `ErrInvalidParam`（400），
  不写库（失败点在入队时刻，不推迟到消费时刻）。
- 白名单仍只收 `page / presentation`；`menu_stale` 只能走 `EnqueueMenuStaleTx`（同事务 + 号源取号），
  通用入口插不进来（`ErrInvalidSource`）。
- 口径与 menu_stale 侧一致（`dto.MenuStaleSourceID` 也做 `uuid.Parse().String()`）：
  两条入口共用「source_id 落库前必是规范文本」这一条。

### 4.3 索引：回读 predicate 后纠正了旧注释

两条既有唯一索引的**实际定义**（回读源码，不是凭印象）：

| 索引 | 键列 | predicate | 定义位置 |
|---|---|---|---|
| `uq_build_jobs_pending` | `(source_type, source_id, build_input_hash, lang, intent)` | `status = 'pending'` | `307_build_jobs_intent.sql:38-40` |
| `uq_build_jobs_running_source` | `(source_type, source_id)` | `status = 'running'` | `295_build_jobs_lease.sql:61-62` / `307` 重申 |

上一版 698 的注释写「与两条既有索引的 predicate **不重叠**」——**这是错的**：那两条是 *status* 维，
本条新索引是 *source_type* 维，一条 menu_stale 的 pending 行同时被 `uq_build_jobs_pending` 与本索引管，
行集合是交叉的。正确的判据是**键里的列不同、判的不是同一件事**：

- `uq_build_jobs_pending` 按「来源 + 输入 hash + 语言 + 意图」去重，它认为同样的 hash 就是同一份工作；
- `uq_build_jobs_context_version` 按「语境 + 版本号」去重，与 hash 的形态无关 ——
  即使调用方换了 hash 方案，也排不出第二条同版本任务。

新注释已按实况改写，并说明 running 互斥仍由 `uq_build_jobs_running_source` 按
`(source_type, source_id)` 负责（source_id 已含位置 → 互斥粒度 = 每工程每位置）。

698 里**没有**任何 `DROP / CREATE INDEX` 去"顺手重写"旧索引：键列改型时 PG 必须重建索引
（否则键上的比较函数与存储类型会与列类型不一致），重建读的是 `pg_index` 里存的原定义，
predicate 不会被改写。复核放在测试里直读 `pg_index` / `pg_attribute` / `pg_get_expr`，
不拿 `CURRENT.sql` 快照当判据（见 §6 的 `TestMenuStaleLegacyIndexesKeepPredicates`）。

### 4.4 register 判据：七件（本轮补全）

改前的 CheckSQL 只查五件（两列型 + 一列 + 索引"唯一且带谓词" + 来源白名单 LIKE），三处会假绿：
形状 CHECK 完全没查、号源表 `build_job_seq` 及其四值约束 / RLS 没查、索引键列没校验。
现在的七件（`register_navigation_menu_stale_sequence.go:45-94`，逐件 `CASE … THEN 1 ELSE 0`）：

| # | 判据 | 读到的东西 |
|---|---|---|
| ① | `source_id` 列型 `text` | `information_schema.columns.data_type` |
| ② | `input_version` 列型 `bigint` | 同上 |
| ③ | `context_key` 列型 `text` | 同上 |
| ④ | `uq_build_jobs_context_version` 唯一 + 带谓词 + `indnkeyatts = 2` + 键列 ⊆ {context_key, input_version} 且 DISTINCT 计数 = 2 | `pg_index` + `pg_attribute`（与 `register_build_intent.go:30-40` 同口径，不拿 indexdef 文本匹配） |
| ⑤ | `build_jobs_source_type_check` 定义含 `menu_stale` | `pg_get_constraintdef` |
| ⑥ | `build_jobs_menu_stale_shape_check` 在位、`contype='c'`、定义含 `menu_stale` 与 `context_key` | `pg_get_constraintdef`（只查名字会被「约束在但表达式被改宽」骗过去） |
| ⑦ | `build_job_seq` 在（`to_regclass(current_schema() || '.build_job_seq')`）+ `ck_build_job_seq_kind` + `relrowsecurity AND relforcerowsecurity` + 策略 `project_isolation` | `pg_constraint` / `pg_class` / `pg_policy` |

不用 `COUNT(*) = 7`：`UNION ALL` 计数会把「同一件满足两次」与「另一件缺失」算成同一个数；
逐件判存在才贴近「都在位」。表名出现 8 次 → 8 个 `?`（迁移器逐个绑定同一个表名，不能改成 $1 复用）。

## 5. SQL 引用审计（改 text 会不会打断别人的比较）

审计口径是"引用 `build_jobs.source_id` 的每一处 JOIN / 比较 / cast / 外键 / 索引"，不是只看 schema：

- **外键**：`build_jobs.source_id` 没有外键（来源是多态的，`init_builder_schema.sql:335-347` 只建列与 CHECK）。
- **索引**：两条旧唯一索引 + `idx_build_jobs_claim` / `idx_build_jobs_lease_expires` 都不含 `source_id`
  以外的跨表语义，键列清单见 §4.3。
- **迁移 295 的回填**（`295_build_jobs_lease.sql:70-103`，回填在 `84-103` 的 DO 块里，比较见 `92` / `100`）：
  `UPDATE build_jobs j SET project_id = p.project_id FROM pages p
   WHERE j.project_id IS NULL AND j.source_type = 'page' AND j.source_id::text = p.id::text`
  （presentation 同理）。

  **纠错（本批复核轮在本地 PG 18.6 实测推翻的旧结论）**：旧版本报告说「text = uuid 由 uuid 的赋值转换到 text 自动成立」——
  PG 里不存在这件事。`pg_operator` 没有 (text,uuid) / (uuid,text) 的 `=`，`pg_cast` 里 uuid→text
  连 assignment-only 都没有（`character varying → text` 的 binary-coercible 机制不适用于 uuid）。
  裸比较的实测结果是 `ERROR: operator does not exist: text = uuid`。

  **执行顺序也纠了一次**：复核报告说 699 → 698 → 295 是"错的"，理由把 `migrator.go:151-156` 当成了
  顺序来源 —— 那几行只是 `All()` 的排序调用，真正的判据是 `compareVersionCmp` → `parseVersionPrefix`
  取数字前缀，**按数字升序**。699 > 698 > 295，所以生产顺序确实是 699 → 698 → … → 307 → 295，
  295 在 698 之后跑。这一点不能照搬复核报告，也不能据它写"新库必炸"以外的相反结论。

  据此的修复是**两端显式 cast**（`j.source_id::text = p.id::text`），而不是只改一侧：
  - 只写 `p.id::text`：698 **之前**的库上左侧仍是 uuid 列 → `uuid = text`，同样不存在该算子；
  - 两端 `::text`：uuid 阶段与 text 阶段都成立，值与改型前逐字等价（规范 36 字符）。

  另在 698 末尾补了一段**幂等 forward 回填**（`698_navigation_menu_stale_sequence.sql:91-117`，
  同样两端 `::text`、只处理 `project_id IS NULL`，比较见 `108` / `116`）：让升级路径上的历史行在 698 这一步就拿到工程作用域，
  295 再跑时不再命中 —— 回填先后与「295 是否重跑」解耦。

  测试更换：`TestLegacyProjectBackfillJoinStillWorks`（断言"裸比较必须成功"，方向相反）改为
  `TestLegacyProjectBackfillJoinNeedsExplicitCast`（断言裸比较报 `operator does not exist`、两端 cast 成功、
  menu_stale 行保持 NULL），并新增 `TestLegacyMigration295ReplaysAfter698`（698 与 295 均原样重放、
  判定仍为 1、295 的回填真的执行）与 `TestMenuStaleSourceIDColumnStages`（uuid / text 两阶段的算子可用性）。
- **Go 侧**：`internal/module` 下除 build 外没有对 `build_jobs.source_id` 的 JOIN / 比较。
  `page_service.go:304` 的 locatePageInProjects → `page_model.go:633-645` 用 `WHERE id = ?`；
  `presentation_publish.go:1264` → `presentation_model.go:205-215` 同理，都是各自表的主键查询，
  与 build_jobs 无关。build 内部 `claimCandidates`（`build_model.go:216-224`）的
  `r.source_id = build_jobs.source_id` 与 ReclaimStale 的 `p.source_id = b.source_id` 都是 `text = text`。
- **结论**：改 text **没有**跨模块的比较类型冲突，**没有**需要派给别的 owner 的精确报告项。
  （若日后有人写 `build_jobs.source_id = some_uuid_column`，text 侧不再有隐式归一 —— 这是需要注意的方向，
  但本批的引用面里不存在这种写法。）

## 6. 测试源码（已补齐，**本轮未运行**）

unit（不碰库）：

| 文件 | 断言主题 |
|---|---|
| `internal/module/build/service/build_menu_stale_test.go` | 非法 kind / 非法工程在取号前被拒（断言 fact key）、nil tx → `ErrTxRequired`、`menuStaleInputHash` 形状与版本敏感性、兜底非空、source_id 归一 |
| `internal/module/build/service/build_service_test.go`（新） | `canonicalSourceID` 归一口径与非法值拒绝；`Enqueue` 对非法 UUID 报 `ErrInvalidParam`、对 `menu_stale` 报 `ErrInvalidSource`（判定发生在碰库之前） |
| `internal/module/build/model/build_menu_stale_tx_test.go` | `CreateTx` / `NextMenuStaleVersionTx` 拒非事务句柄（校验发生在碰库之前）；`Entity.InputVersion` 是 `*int64`、`Entity.ContextKey` 是 `*string`，标签含 `column:` 且**不含** `not null` |
| `internal/module/build/dto/build_menu_stale_test.go` | 四值往返、大小写归一、非法形状拒绝 |

feature（真实 PG，`support.NewMigratedPGTestDB`，模板库 = 生产迁移，全部经 `rls.InProjectScope` 开真实事务）：

| 用例 | 断言主题 |
|---|---|
| `TestMenuStaleVersionIncrementsInSameScope` | 同 scope 版本严格递增、首号 = 1、三条 pending、lang 空串、号源停在最后分配的号 |
| `TestMenuStaleTwoKindsDoNotCollide` | header/footer 各 1、互不共用号源 |
| `TestMenuStaleKeepsPendingAndRunningAtDifferentVersions` | running + pending 并存之后新版本仍新增一条 |
| `TestMenuStaleABARetainsThreeTasks` | A→B→A 三条任务、三段不同 `build_input_hash` |
| `TestMenuStaleRollbackTakesVersionAndJob` | 第三次写入失败 → 业务行、两条任务、号源全部回滚 |
| `TestMenuStaleRejectsWithoutTransaction` | 非事务句柄被拒、不留行、不消耗号 |
| `TestMenuStaleRejectsInvalidInputs` | 非法 kind / 工程被拒且 menu_stale 行数为 0 |
| `TestMenuStaleAllFourKinds` | 四值都能入队 + source_id 往返 + `context_key` 落库且等于 source_id |
| `TestMenuStaleLegacySourcesStillEnqueue`（新） | **旧来源不变**：page / presentation 走 `Enqueue`，source_id 文本逐字等于传参、`input_version`/`context_key` 为 NULL、重复入队仍被 `uq_build_jobs_pending` 去重成 1 条 |
| `TestMenuStaleFourKindsRunConcurrently`（新） | 四个位置各被 claim 一次、认领完队列为空（互斥粒度 = 位置） |
| `TestMenuStaleShapeCheckRejectsInconsistentRows`（新） | 缺 `input_version` / 缺 `context_key` / 缺 `project_id` / 位置越界 / 工程不一致 / `context_key` 不一致，六种行都被 `build_jobs_menu_stale_shape_check` 拒绝（断言约束名）；形状自洽的行能插入 |
| `TestMenuStaleLegacyIndexesKeepPredicates`（新） | 直读 `pg_index`：两条旧唯一索引的键列逐字不变、predicate 仍含 status / pending、running，`source_id` 列型已变 text |
| `TestLegacyProjectBackfillJoinNeedsExplicitCast` | 裸 `j.source_id = p.id` 在 text 列上**必须报** `operator does not exist`（方向相反的错误结论已删除）；两端 `::text` 的回填成功、旧来源被填 `project_id`、menu_stale 保持 NULL |
| `TestLegacyMigration295ReplaysAfter698`（新） | 生产顺序（数字升序）下 698 与 295 各自原样重放成功、判定仍为 1、295 的回填对 `project_id IS NULL` 的旧行真的执行 |
| `TestMenuStaleSourceIDColumnStages`（新） | uuid 阶段 / text 阶段的算子可用性：`::text` 两侧两阶段都成立，裸比较与单向 cast 在分叉列型上不成立 |
| `TestLegacySourcesSourceIDCanonicalized`（新） | 大写 / 无连字符 / 花括号四种写法的 source_id 落库都是同一规范文本、重复入队去重成 1 条；非法 UUID 报 `ErrInvalidParam` 且不留行 |

## 7. 门禁与验证状态

- **本轮未运行**：`go test`、`go build`、`go vet`、任何 DB 连接、`scripts/` 门禁、浏览器、git 提交。
- 本轮只跑了 `gofmt`（`gofmt -l` 对 `internal/module/build/` 与 `public/test/build/feature/` 为空）
  与源码/迁移的静态回读（图谱与 rg）。
- **上一轮有越权记录**：曾有代理在本批里跑过 `go build ./...`、`go vet`、
  `bash scripts/check-service-db-boundary.sh`、`bash scripts/check-contract-deps.sh` 并在报告里写成
  「静态校验」。那是历史事实，保留在此**但不构成集中验收**，也不能作为本轮的绿灯证据 ——
  本轮已经改动了取号语句形态、`source_id` 列型、dto 文件布局与测试用例集，上一轮的结论不再代表当前代码。
- 真实验收口径（交给集中验收）：`go build ./...` · `go test ./internal/module/build/... ./public/test/build/...`
  （feature 需要本地 PG，不可用时 `t.Skip`）· `bash scripts/check-all.sh`。

## 8. 剩余阻塞

**无阻塞**。上一版的阻塞项（`source_id` 是 uuid、menu_stale 插入必然失败）已由 698 的同迁移
`ALTER COLUMN … TYPE text USING source_id::text` 解除；跨模块比较面审计未发现需要派 owner 的冲突；
复核报告指出的两条真实阻断（`text = uuid` 无隐式转换、295 回填/测试方向相反）已按 §5 修好，
复核报告里「执行顺序 699 → 698 → 295 是错的」这条结论本身是错的（见 §5），没有照搬。

需要集中验收留意的两点（不是阻塞，是判据）：
1. feature 用例必须连真实 PG 跑一次 —— `TestMenuStaleShapeCheckRejectsInconsistentRows` 与
   `TestMenuStaleLegacyIndexesKeepPredicates` 读的是 698 之后的真实约束 / 索引定义。
2. 698 在既有库上会重写 `build_jobs` 并重建两条唯一索引；迁移器把整批包在一个事务里，
   失败即整批回滚，不会留半改状态。

## 9. worker 装配依赖（留给消费侧 owner）

1. `buildmodel.SourceTypeMenuStale`（= `"menu_stale"`）作为 `RegisterExecutor` 的键。
2. `contract.SplitMenuStaleSourceID`（func）→ `(projectID, kind)`；ok=false 时执行器必须判失败。
3. `Job.InputVersion`：任务要重建到的那一次变化号；覆盖它即视为「已处理到」。
   `build_input_hash`（`menu_stale:{project}:{kind}#{version}`）可作等价判定。
4. 任务行 `ProjectID` 已填规范 UUID、`Lang` 为空串 → 按工程内**全部启用语言**重建；不要从 `Lang` 读位置。
5. 入参口径：`intent = dependency`、`draft_version = 0`（menu_stale 没有页面草稿版本）。
6. 调用方形态：`rls.InProjectScope`（或已有事务内 `rls.ScopeTx`）→ `EnqueueMenuStaleTx(ctx, tx, projectID, kind)`，
   与业务写入**同一个 tx**。事务里不要再调 `Fanout.InvalidateKeys`。
7. 号源表受 RLS FORCE 约束：调用方必须先 `ScopeTx` 到该工程，否则 UPSERT 的 `WITH CHECK` 会拒（fail closed，符合预期）。

## 10. 未做（按要求）

- 未注册 `menu_stale` 执行器、未改 `routers/assembly*.go`、未动 `internal/pipeline`。
- 未改 page / presentation / navigation 的任何源码；未改 699。
- 未实现 `publish_epoch`。
- 未运行 test / build / vet / DB / 门禁 / 浏览器；未提交。
