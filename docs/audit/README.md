# 2026-09 全面审查

本目录为审查结论与整改进度追踪。主索引是 [audit-2026-09.json](./audit-2026-09.json)，分维明细在 [dimensions/](./dimensions/)。

## 状态（2026-09-16 更新）

- **已 resolved**：243 条
- **仍 open**：25 条（high 8 / medium 12 / low 5；按阶段 P5 1 / P6 3 / P7 17 / P8 4）。
  · 其中 **9 条按决策延后**（插件生态，标 `deferredNote`）：`OSS-001/002/003/004/005/007/008/019`、`SEC-005`。
    **2026-09-16 决策确认（第二轮）**：插件生态整体延后，等本体（内容 / 权限 / 可视化编辑 / 发布管线）
    收口之后再考虑 —— 在此之前不投入实现，也不把它算进「还差什么没做」的实际待办。
  · 仍待推进的 16 条里 high 只剩 2 条：`CQ-007`（dashboard 上帝模块）、`UIK-003`（四套视觉体系并行）。

### 2026-09-16 收口批次

本批修掉 5 条（`PERF-013` / `CQ-025` / `EDT-009` / `SEO-023` / `PERF-017`），
并**修正两条已 resolved 的结论** —— 它们此前是「文件在、门禁不生效」：

- `OSS-014`（环境变量覆盖 YAML）：`AutomaticEnv` 只作用于 `Get*` 系列，
  而全仓配置读取走 `UnmarshalKey`，环境变量被**静默忽略**（实测：`GOWP_DATABASE_DBNAME` 指向
  一个不存在的库，`-migrate-only` 照旧迁移 config.yaml 里写的那个库并返回 0）。
  已改为把环境变量并入所属顶层段再整段写回，并加测试按真实读取路径钉住。
- `OSS-010`（CI）：unit job 只覆盖 5 个包路径，**30 个含测试的包一个都不跑**
  （其中恰好是迁移判据、retention 不变量、路由装配断言这些护栏）；integration job 挂着
  `continue-on-error`；其 `DATABASE_URL` 全仓无人读、`config.yaml` 又被 gitignore，
  于是测试要么回退 testcontainers，要么静默 skip。已逐条收口，见该条的 resolutionNote。

本轮顺带修掉的既有缺陷（不在 268 条内）：

| 缺陷 | 症状 | 位置 |
|---|---|---|
| CI 的数据库配置是死配置 | `DATABASE_URL` 全仓零引用（测试基建走 libpq 的 PGHOST/PGUSER/… 与 `TEST_REDIS_ADDR`）；服务容器凭据与 `config.yaml.example` 的默认值不一致，只能靠容器回退兜底 | `.github/workflows/go-test.yml` |
| 没有独立的迁移入口 | 「把库迁好」与「启动服务」绑在一起，CI 与部署只能先起实例再杀掉，失败原因混在启动日志里 | `cmd/main.go` 新增 `-migrate-only`；`Makefile` 的 `migrate` 目标随之改成真跑迁移 |
| presentation 为拿一个枚举常量 import 了对方 model | 跨模块依赖数据访问包，与「只用 contract 与不可变 dto」相悖 | `contenttemplate/contract`（`TemplateRole*` 常量上移，model 转发） |
| `cmd/` 不在 gofmt lint 范围 | `make lint` 只扫 internal/pkg/public，`cmd/main.go` 长期不合规 | `Makefile` 的 `lint` / `test-short` / `check` 三个目标 |
- 批量标记脚本：`scripts/audit-mark-resolved.py`（标记新 resolved）、`scripts/audit-fix-note.py`（覆盖已 resolved 条目的结论修订）

## 怎么读

先看主索引的 `summary` 与 `verdicts`，再按关心的维度打开对应 JSON。每条 finding 的字段固定：

| 字段 | 含义 |
|---|---|
| `id` | 稳定编号（如 `UIK-014`），跨文档引用用它 |
| `severity` | `critical` / `high` / `medium` / `low` |
| `type` | 缺陷分类（design-flaw / implementation-bug / missing-feature / …） |
| `confidence` | 目前全部是 `confirmed`（有代码证据） |
| `problem` / `evidence` / `impact` / `rootCause` | 事实与判断分开写 |
| `remediation` | 建议方案 |
| `status` | `open` 或 `resolved` |
| `resolutionNote` | 已修复项的落地说明（仅 resolved 时有） |
| `progressNote` | 分批推进项的**当前进度**（仅 open 且已部分落地时有）：写清已落地哪一批、实测数据、以及下一步边界。与 `resolutionNote` 互斥——条目 fully 修完时改回 `resolutionNote` 并删掉它 |
| `deferredNote` | **按决策延后**的说明（仅 open 时有）：写清是谁在什么时候决定延后、等什么条件再启动。与 `progressNote` 的区别——后者是「正在分批做、已落地一部分」，前者是「决定先不做」 |
| `phase` | 建议排期（P0–P8） |

## 施工中顺带修复的既有缺陷（非审计条目）

P0 收口过程中撞到一批**不在 268 条内**的缺陷：它们让全仓测试整片变红（16 个包），
使得「按审计条目逐个修复」根本无法验证。已随同一批改动修掉，记录在此便于回溯：

| 缺陷 | 症状 | 位置 |
|---|---|---|
| 迁移版本前缀只认「纯数字 + 分隔符」，撞上存量 `086a` / `086b` / `087b` 时 panic | `RunSeeds` 直接崩，所有依赖种子的测试包整体失败 | `public/migrations/migrator.go` |
| 评分投影列缺 GORM 只读标记 `->` | GORM 把 `rating_avg` / `rating_count` 写进 INSERT，而 products 表没有这两列 —— 每次建商品都失败 | `internal/module/product/model/product_model.go` |
| 页面内联页眉/页脚块、globalref 块解析只传块 ID，缺工程 scope | 内联在降级路径上静默失败：产物少一截、状态码仍是 200 | `page/service/page_theme.go`、`page_assemble.go`、`presentation/service/presentation_blocks.go` |
| 后台 6 处（画布 / 预览 / 草稿预览 / 历史恢复 / 译文）按页面 ID 查详情时缺工程 scope | 一律 404「页面不存在」 | `dashboard/inbound/http/*` |
| 译文工作台与全站索引的块读取缺工程 scope | 块内文本一条都不列，完成度误报 100% | `dashboard/inbound/http/page_translations_blocks.go`、`page_translations_index.go` |
| 导航来源解析（page / block）缺工程 scope | 菜单项永远显示记录里的占位标题与占位链接（回退不报错） | `navigation/outbound/source/resolver.go` |
| 采购入库按**采购单号**查流水判重 | 一张单分多次收货时，第二批被误判为重复 → 库存不加、单据状态却推进 | `product/inventory/service/inventory_receipt.go` |
| button 样式模板残留一条与新规则重复的悬停规则 + golden 未随 reduced-motion 注入更新 | CSS 里同一属性输出两遍；`TestJetViewByteEquivalent` 全用例失败 | `components/button/button.css`、`testdata/golden/*` |
| `searchresults` 样式用字面 `@media (hover: hover)`（解析器只认 max-width 断点） | 构建期 panic；组件颜色全硬编码，不继承主题 | `components/searchresults/searchresults.css` |
| 后台模板对可选键用点号取值（`{{.TemplateEditURL}}`）且数据侧不保证键齐全 | Jet 报错并**截断整页输出** —— 编辑页只剩上半截、状态码 200 | `templates/admin/article_edit.html`、`dashboard/inbound/http/article_*.go` |
| 媒体库的 `projectId` 是「必填但从不参与过滤」的装饰参数 | 任何没带它的调用方（含后台自身）拿到 400 | `media/service/media_crud.go` |
| 模板类型不匹配被 `ErrNoTemplate` 掩盖（错误链丢失） | 拿 article 模板渲染商品时报「没有模板」，与事实无关 | `presentation/service/presentation_service.go` |
| 一批测试夹具 / 断言与实现脱节（缺工程 scope、控件白名单、词条计数、外壳区块名、`isTemplate` 数据） | 用例红但实现正确 | `public/test/{block,page,media,navigation,product,dashboard,pkg/i18n,builder}` |
| 迁移注册的判据写反或退化为默认「表存在即跳过」：`053` 把「外键存在（正需要删）」判成已完成、`165` 把「约束不存在（正需要加）」判成已完成、`201`（DB-019/020 第一批）用 `build_jobs` 的默认表存在判据 → 三者**从未真正执行** | 迁移文件在、DDL 从未生效：`page_routes.page_id` 外键残留使「新建页面」必然 500；`orders.status` 无 CHECK；五张表的 `create_time` / bigint 主键不存在（全仓 10 个包红） | `public/migrations/register_{core,analytics,catalog}.go` |
| 迁移判据只判「约束存在」不管定义（`166` blocks.kind / `114` bundle 形状约束）：旧定义同样「存在」，扩展或收窄后判定不出未达标 | 迁移可被静默跳过，旧定义长期留存 | 同上 |
| feature 测试用手抄 DDL 建表而不是跑生产迁移（`publication` 路由用例） | 手抄的 `publication_receipts` 停在 `uuid` + `created_at`，与生产迁移改名后的 `bigint` + `create_time` 脱节 → 该包 4 个用例全红，且掩盖了真实表结构 | `public/test/publication/feature/publication_route_test.go` |
| 归档路径的 `content_objects.object_key` 用**产物级** key（`artifacts/<hash>`），而表上是 `UNIQUE (provider, object_key)` | 同一产物的第二个内容对象必然撞唯一键，被无 target 的 `ON CONFLICT DO NOTHING` 静默吞掉 → 闭包表外键找不到 `content_hash` → 整单归档回滚；报出来是外键违例，看着像「库坏了」而不是「key 选错了」 | `internal/module/artifact/service/artifact_record.go`（改为 `artifactObjectKey(key, fileName)`，按文件名排序保证确定性） |
| `page_artifact_objects` / `presentation_artifact_objects` 的 `content_hash` 外键没有 `ON DELETE CASCADE` | 内容对象 GC 判定某条只被已回收产物引用 → 选中它 → DELETE 被外键挡下（23503）→ GC 把失败记进统计而不返回 error → 内容对象表在生产上只增不减（静默泄漏） | `public/migrations/204_artifact_closure_fk_cascade.sql`、`init_builder_schema.sql` |
| `ReplaceArtifactContent` 在同一事务里**先写闭包、后写被引用的内容对象** | 顺序违反外键：多语言第二次发布（`EnsureRecord` 的同版本替换分支）必挂 23503 | `internal/module/artifact/model/artifact_model.go`（两段对调） |
| artifact/unit 的 AutoMigrate 表缺生产约束（`UNIQUE (provider, object_key)` 与两条外键），两个 first-writer-wins 用例把「产物级 object_key」当成正确语义钉住 | 测试跑在一套**不存在约束**的表上，真实缺陷被静默放过（同属「手抄表掩盖生产约束」这一族） | `public/test/artifact/unit/helper_test.go`、`content_object_test.go` |
| 访客会话台账落库（`user_sessions`）：会话状态本来就在 Redis，台账只是设备视图的第二份真源 | 要额外维护保留期声明 + 每日清理任务 + 167 的部分索引；改为一律 Redis（索引 ZSET `gwp:userauth:user:<userID>:sess`）后整表删除，顺带消掉「Redis 存明文令牌」的隐患 | `public/migrations/203_drop_unused_user_tables.sql`、`internal/module/user/service/user_session*.go` |
| 同类隐患的存量面：28 个测试文件共 109 条手抄 `CREATE TABLE`（`*/unit` 下的迁移机制/分区/插件用例属于设计使然，风险集中在 `{admin,artifact,dashboard,media,navigation,page,project}/feature`） | 手抄表与生产 schema 静默分叉；DB-019/020 的后续批次继续改列名与主键类型时，这些包会集体变红，且失败信息看起来像「迁移把库改坏了」 | `public/test/*/feature/*_test.go`（建议改为 `migrations.Run` + 只补必要的父行，如 `projects`）。**2026-09-16 复核：存量已降到 10 个文件 20 条**，剩余集中在 `*/unit` 与 `public/migrations` 下有意构造旧 schema 的用例 |
## 高风险项收口情况（2026-09-14）

**DB-004 分区**：三张只增表（`page_views` / `inventory_stock_movements` / `master_data_changes`）
主键均为 `(id)` 且不含时间列，按 PostgreSQL 要求需改为 `(id, <时间列>)`；**实测无任何外键指向它们**（无外键阻塞）；
既有索引多半已带时间列，仅 `idx_inventory_movements_batch` 与 `idx_master_data_changes_field` 需补分区键；
`master_data_changes` 的 append-only 触发器不会自动下沉到分区，需逐分区建。
结论：**技术上无硬阻塞**，但当前库这三张表 0 行、收益无法验证，且需在线迁移窗口与自建的分区创建定时器，
适合等有真实数据量时单批推进。

**DB-018 / TX-011 时间列类型统一**：全库 93 列 `timestamp without time zone`（36 张表）对 91 列 `timestamptz`。
实测 PG 会话时区 `Asia/Shanghai`，种子写入的 `sys_i18n.create_time` 是**本地墙钟**（08:36，而此刻本地 09:50 / UTC 01:50），
而代码里 `time.Now().UTC()` 有 99 处、未加 UTC 的 `time.Now()` 有 40 处 —— **同一列可能混存两种口径**（相差 8 小时）。
结论：**不能批量脚本化**，必须逐列判定写入路径再决定 `AT TIME ZONE` 的解释；判错即整体偏移且 `ALTER` 不可逆。
**已完成（2026-09-14）**：迁移 172 把 93 列统一为 timestamptz（历史值按会话时区解释，dev 库实测时刻未偏移），
并新增护栏测试盯全库终态（只要再出现无时区时间列就红）；券时间窗与后台日期筛选改用固定/站点时区
（`couponWindowLocation` 与 `pkg/sitetz`），不再依赖服务器时区。

**DB-004 分区**：迁移 173 已把三张只增表改为按月 RANGE 分区，主键改 `(id, 时间列)`，
索引与 append-only 触发器在父表上重建，另有 `internal/partition` 负责提前建分区与整块 DETACH 归档。
7 条用例覆盖审计四条验收。

## 数据保留期一览（IDX-019）

声明与执行都在 `internal/retention`：`Task` 描述「哪张表、按哪一列、留多久、每批多少行」，
`Sweep` 由各模块提供（表访问权留在各自的 model 里，不跨模块直查）。新增需清理的表
时照这个形状接一处即可，不再各写一套。

**全库增长型表的声明在 `internal/retention/catalog.go`**（IDX-019 的落地形态）：审计当初的
问题不是「少写了几个清理任务」，而是**没有任何地方承诺过保留期** —— 哪张表留多久、
为什么、谁负责清，全散在各模块注释里。目录把这件事变成代码里的可查清单：纯声明不执行，
每张表**要么给出保留期与清理方式、要么显式写「不清理」并给出理由**；三条不变量由
`catalog_test.go` 钉住（审计列出的表一张不漏、声明之间不自相矛盾、每条都写明理由）。

| 表 | 时间列 | 保留期 | 执行者 | 备注 |
|---|---|---|---|---|
| `page_views` | `viewed_at` | 工程可配（`analytics_retention_days`） | analytics 调度 | IDX-001 起就有 |
| `page_views_daily` | `day` | 不清理（每天一行 × 路径数，体量远小于明细） | analytics 每小时汇总 | 161 建表、170 扩为汇总口径（DB-005/IDX-010）；明细清理后由重算自动收敛 |
| `page_revisions` | `create_time` | 90 天 **且** 每页保留最近 20 个 | 保存草稿时收敛 + page 每日任务 | 两个条件同时满足才删 |
| `page_artifacts` | `create_time` | 30 天（无指针引用才回收） | page 每日任务 | 顺带回收磁盘产物 |
| `mail_campaign_events` | `create_time` | 180 天 | mail 每日任务 | **先固化汇总**（`open_count` / `click_count`）再删明细 |
| `mail_logs` | `create_time` | 180 天 | mail 每日任务 | 与事件明细同一口径 |
| `mail_automation_node_logs` | `create_time` | 180 天 | mail 每日任务 | 自动化逐节点一行，启用后增长最快 |
| `content_objects` | `create_time` | 30 天（无任何现存产物引用才回收） | page 每日任务 | 与产物 GC 同一趟：先删产物再清孤儿对象（IDX-016） |
| `artifacts` 磁盘目录 | 随产物行 | 30 天 | page 每日任务 | 内容寻址目录，与产物行同批删除 |

**声明为不清理的表**（完整理由在 `catalog.go`）：`inventory_stock_movements` 与
`master_data_changes` 是合规留档（改保留期需业务确认，工程侧不应单方面决定删除）；
`order_status_logs` 随订单存续（订单不删，且它是订单详情的一部分）；`product_ratings`
是业务数据（评分参与前台排序与筛选，按时间清理等于悄悄改变呈现）；`sys_translation`
孤儿行需要按「是否还有实体引用该原文 hash」做标记清除（见 I18N-024）；`page_views_daily`
是历史报表的读数来源、体量可控。

### 对账（只发现、不自动修正）

两条对账的共同原则：**只报告**。偏差该往哪边修正取决于原因，自动改可能把真源也改错。

**券计数（DB-021）**：`coupons.used_count` 是**投影** —— 它的存在是为了让核销走
`UPDATE ... WHERE used_count < max_uses` 这样的原子守卫（并发下不能改成每次 COUNT），
真源是 `coupon_redemptions` 明细。两者之间没有数据库层约束，偏差两个方向都会出问题：
计数偏大 → 券提前用尽（用户看到已抢完而实际还有额度）；计数偏小 → 可超出 `max_uses` 继续核销。
对账入口 `GET /api/order/coupon/count-audit`（只读，支持按工程过滤）：用 LEFT JOIN 聚合，
因此「只有计数、没有明细」的券同样会被报出 —— 用 INNER JOIN 会正好漏掉最该发现的那一类。
对账跑完不改数据（有测试钉住）。

**产物磁盘（IDX-015）**：正向巡检访问面链接可达性，反向列出「磁盘上有、无人认领」的产物目录。

另有一条**不删数据**的对账（IDX-015）：`page.AuditPublication` 除巡检访问面链接可达性外，
还会列出「磁盘上有、`page_artifacts` 与 `presentation_artifacts` 都不认领」的产物目录（含体积与文件数）。
孤儿只报告、不自动删 —— 处置方式是看体积后调产物 GC 的保留期，或人工确认后清理。

### 内容对象回收（IDX-016）

产物是内容寻址的，闭包里的共享内容对象（`content_objects`）此前**只有写入路径** —— 产物行
被回收后它引用的对象便永远留在表里。现在的做法是标记清除，判定只有一条：

**该对象是否仍被「现存」（`payload_state <> 'deleted'`）的产物行经 `page_artifact_objects` 引用。**

两个容易看错的地方，都在代码注释里写明了理由：

- **已回收的产物行不算引用**。它的元数据还在（`source_document` 保留、可 rebuild），但它指向的
  物理目录已删，闭包对象的 `object_key` 同样指向不存在的文件；继续算引用等于让对象永不回收。
- **引用来源不止一处**。当前只有 page 路径写闭包，自动发布实例侧不写 `content_objects`
  （见 CQ-015）；未来接入时经 `SetExternalContentRefs` 注入外部引用来源即可，
  未注入 = 确认没有外部引用，注入后查询失败则整轮放弃（宁可不回收也不误删）。

原子性靠「查与删同一条语句」：删除 SQL 自带 `NOT EXISTS` 复查，删除行数少于抛出的候选数
就说明有对象在中间被并发归档重新认领了 —— 报为 `kept_reclaimed`，既不算删也不算失败。
安全默认与产物 GC 一致（`dryRun` 默认 true、保留期同 30 天、同一趟执行）。

尚未接入保留期的表（库存流水、`master_data_changes`、`order_status_logs`、
`product_ratings`、`sys_translation` 孤儿行等）仍按 IDX-019 逐条推进；
`inventory_stock_movements` 与 `master_data_changes` 涉及合规留档，保留期需业务确认后再定。
## 分维文件

| 文件 | 维度 | 条数 |
|---|---|---|
| [01-visual-system.json](./dimensions/01-visual-system.json) | 组件 / 区块 / 页面 / 主题四层 | 15 |
| [02-dynamic-pages-editor.json](./dimensions/02-dynamic-pages-editor.json) | 动态页 × 可视化编辑器 | 18 |
| [03-i18n.json](./dimensions/03-i18n.json) | 多语言 | 25 |
| [04-seo.json](./dimensions/04-seo.json) | SEO | 25 |
| [05-performance.json](./dimensions/05-performance.json) | 性能与内存 | 21 |
| [06-code-quality.json](./dimensions/06-code-quality.json) | 边界、可读性、体系整合 | 26 |
| [07-plugin-opensource.json](./dimensions/07-plugin-opensource.json) | 插件与开源就绪 | 21 |
| [08-ui-multidevice.json](./dimensions/08-ui-multidevice.json) | 多端、无障碍、编辑器体验 | 22 |
| [09-postgres-schema.json](./dimensions/09-postgres-schema.json) | PostgreSQL 结构与约束 | 24 |
| [10-index-lifecycle.json](./dimensions/10-index-lifecycle.json) | 索引与数据生命周期 | 20 |
| [11-security.json](./dimensions/11-security.json) | 安全 | 16 |
| [12-transaction-correctness.json](./dimensions/12-transaction-correctness.json) | 事务、并发、金额 | 16 |
| [14-ui-kit-and-registry.json](./dimensions/14-ui-kit-and-registry.json) | **控件基座、基础动画、组件注册** | 19 |

合计 **268** 条。没有 `13-*`：运维/CI 并入 `07`，库结构与生命周期拆成 `09`/`10`。

## 控件基座 + 动画 + 注册（维 14 摘要）

1. 产物侧白名单与协议已部分收口（`UIK-001`/`UIK-014` resolved）；toast/busy 仍仅后台。
2. `prefers-reduced-motion` 已无条件注入产物（`UIK-006` resolved）。
3. 内置组件继续 Go 注册；插件走 manifest（`REG-003` 仍为 open 设计决策）。
