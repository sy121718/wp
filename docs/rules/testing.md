# 测试与验证细则

`AGENTS.md` 的「测试」节给判据；本文件给测试基建的实测结论与**下结论前怎么验**的方法论。

## 测试分层

- **模块内就近单测**（`internal/module/**/*_test.go`）—— 纯逻辑，不碰数据库、不走装配；
- **feature / 链路测试**（`public/test/**`，真实 PostgreSQL）—— 需要数据库、事务、HTTP 或完整装配路径的行为；
- **组件测试**在组件包内（`internal/builder/components/*`），含确定性构建与 fuzz 测试。

**判定标准是「这个判断错了会不会静默出错」**：会（多记一条流转、多标一个 stale、金额舍入、错误被压成
同一句话）就补模块内单测；不会、只是路径走不通，交给 feature 测试。

不追求覆盖率数字，不新增额外测试框架，默认跑现有测试。

## 并发与迁移基建

### 全量测试可以并发

`make test` 走 `-p $(nproc)`（`TEST_PARALLEL` 可覆盖）。2026-09 之前只能 `-p 1`，两道拦路虎都已拆掉：

**① `pg_trgm` 是库级唯一的扩展。** 装在哪个 schema 只有 search_path 含它的连接才解析得到
`gin_trgm_ops` —— 过去串行时靠「测试结束 DROP SCHEMA 把扩展一并删掉、下个 schema 重新装」侥幸通过，
一并行就互相踩（后来者 `CREATE EXTENSION IF NOT EXISTS` 静默跳过，随后整条迁移报
operator class does not exist）。现在它固定装在有专用 schema **`ext_shared`**（迁移 210 负责既有库搬迁），
迁移器 `Run` 统一把该 schema 补进 search_path —— 任何调用方都不会再踩。

**② 测试不再为每个用例重跑全部迁移**（见下面「模板库」），并发时的锁表压力随之消失。
这正是当初 `-p 8` 会随机几个包 `out of shared memory` 的原因（失败包每次都不同，别误读成
「某个包坏了」）。要再往上提并发，先确认 `max_locks_per_transaction`（默认 64）够用。

### 按对象名查 catalog 的 SQL 必须限定 `current_schema()`

`pg_class` / `pg_indexes` 是**全库**的，并发（或库里残留了旧 schema）时同名表 / 索引会被一并查到。
迁移判定与测试断言各踩过一次：

- 167 的判定漏了 `schemaname`，整条迁移被静默跳过（该 schema 的 trgm 索引全缺）；
- `p7_index_audit_test` 把 35 个 schema 的同名索引键列拼成了一份。

用 `'表名'::regclass` / `to_regclass` 锚定对象是安全的（按 search_path 解析），
按 `relname` / `indexname` 过滤才需要显式限定。

### 迁移必须在单连接上跑

`Run` 用 `db.Connection`：`pg_advisory_lock` 是会话级的，而 `db.Raw` / `db.Exec` 每次都从连接池取连接 ——
换连接会让 `unlock` 落到别的连接上（锁永不释放，几十个测试进程一起泄漏直接 `out of shared memory`），
那把锁也根本保护不到迁移语句本身。

锁键按 `current_database() || current_schema()` 派生：生产多实例同库同 schema 仍然互斥（原意），
测试各用隔离 schema 时不再互相排队。

## 测试的表结构一律来自生产迁移

两个 helper 分工明确，别混用：

- **`support.NewMigratedPGTestDB(t)`** —— 需要真实 schema 的用例（feature / 链路 / 大部分 unit）。
  它复制一份**模板库**（`CREATE DATABASE ... TEMPLATE wp_test_tpl_<指纹>`，实测约 65ms），模板库由
  `migrations.Run` 建成：结构与生产逐字节一致，只是不再为每个用例重付那 1.1s（admin 一个包 116 个用例
  过去就是 128s，现在 46s）。模板名带 `migrations.Fingerprint()`：迁移一改就换新名字重建，绝不会拿过期
  结构跑测试；旧模板在建模板时顺手清理。
- **`support.NewPGTestDB(t)`** —— 建**空库**，给自己建表（`AutoMigrate` / 手抄 DDL）或故意构造旧 schema
  的用例用。

**塞给它们完整生产结构反而会坏**：实测 `AutoMigrate` 会去对齐一个名字不同的约束而报 42704，
手抄的最小 schema 没有外键、换成生产结构后 INSERT 立刻撞 FK。

### AutoMigrate 不是「简化版建表」

它照 model 的 gorm 标签建列，与生产 DDL 静默分叉 —— `artifact` / `media` / `plugin` / `publication`
四个包曾因此跑在「没有外键、没有唯一键、列型不对」的表上，断言在测试里全绿、到生产才暴露：

- `plugin_registry.manifest` 被建成 bytea 而生产是 jsonb；
- `page_routes` 缺 `page_id` / `presentation_id` 恰有一个的 check；
- `receipt_data` 不是 jsonb，于是非法 JSON 也能落库。

四包已全部切到 `support.NewMigratedPGTestDB`（`artifact` 用 `NewMigratedPGTestDBTranslateError` 对齐生产的
gorm TranslateError），连带的代价是要补真实父行（`support.SeedProjectRow` + pages / content_templates /
presentation_instances 等）。

AutoMigrate 只留给「故意构造旧 schema」的用例；`page` / `block` / `project` / `content` 的 unit 包仍用它
自建表（当前断言不依赖列型细节，属待收敛的存量），改到那片代码时顺手切过来。

**禁止手抄 `CREATE TABLE`** 去伪造「看起来像生产」的表：会与生产静默分叉（`publication` 用例手抄的
`publication_receipts` 停在 `uuid` + `created_at`，与生产迁移后的 `bigint` + `create_time` 脱节；
同类手抄分布在 7 个 feature 目录）。测试只额外补**真实父行**（`support.SeedProjectRow` 等）。

PG / Redis 不可用时相关用例 `t.Skip`。

## 分页判据要分清「单页」与「多页」

`shell.BuildPagination(total, page, limit, baseURL, t)` 在 **`total <= limit`（单页）或 `total == 0` 时返回
nil**，`TemplateKeys()` 给空 map —— 于是单页场景页面上**根本没有「共 N 条，第 X-Y 条」那一行**。

写分页测试时若在「筛选后只剩单页」的分支里断言该文案，会得到一次**假失败**（实测踩过：品牌页关键词筛出
10 条时断言「共 10 条」失败，实际是按设计不渲染）。单页只能断言「无分页条 + 行数」；要断言信息行，
先确认 `total > limit`。

## 下结论前怎么验

### 计数与命名不是判据，结论必须回读语义

`rg -c` / `grep -c` 只能当**筛查起点** —— 任何写进结论、文档、任务书或提交信息的判断，都要回到上下文确认
「它到底是不是你要找的那个东西」。实测踩过四种误判，全都是「计数 / 命名对上了，语义没对上」：

- **常量名 ≠ 值**：`orderenums.ErrInternal` 名字像 i18n key，值是中文文案「操作失败，请稍后重试」；
  而同域的 `ErrOrderNotFound` 的值**就是** key（`order.err.orderNotFound`）。判「这是不是裸 key」
  只能读值 —— 同一个 enums 包里两种形态并存是常态（未接 i18n 的模块直接等于中文常量）。
- **类名 / 关键词计数 ≠ 目标数量**：`class` 含 `alert|notice|empty-state` 的行数被当成「提示槽数量」
  （26 处），真正的判据是 `role="alert"`（**1 处**）。
- **计数判据本身选错**：`grep -c 'class="pagination"'` 被当成「有没有分页」的判据 —— 已接分页的页面
  （products / orders / customers / returns / coupons）在同一判据下**同样是 0**，因为分页条在
  `partials/pagination.html` 内部。正确判据是 `{{include "partials/pagination.html"}}` 或 handler 是否调
  `shell.BuildPagination`。
- **按域 / 文件一刀切 ≠ 逐条定性**：workbench 51 处 `c.String` 被整体归为「接口出口（P2）」，
  逐条看调用方后其中 **23 处服务的是页面导航**（`<a href="/workbench?id=…">`），是 P0。

反过来说：**读数异常时先怀疑自己的判据，而不是直接下结论** —— 门禁脚本曾因注释里的字面 `{{range}}`
产生幻影栈帧（12 处假阳性）；「门禁绿」同样不等于「没问题」
（`check-no-internal-error-leak.sh` 的候选集只含带 `.Error()` 的行，硬编码文案与裸 key 从来不在它的视野里，
修前修后都是绿的）。

### 任务清单的行号会整体失效

`docs/02-{L,M,O}` 三份清单里记的文件行号，在后续几批改动后**全部漂移**（核对时逐条重新定位过）。
引用它们时按**语义**定位（函数名、类名、结构特征、关键文案、i18n key），不要按行号跳转；
清单条目本身（问题描述与判据）仍然可信，读数与行号要重新采信。

### 委派 / 并行任务：独占文件清单要算上「同包私有函数的签名」

给并行代理（或自己分批）划「独占文件」边界时，**只列文件是不够的** —— 同包私有函数的**签名**也是边界。
改一个签名会波及本包其它文件里的调用点，而那些文件可能正握在另一个并行任务手里，且编译错误会一次炸出一大片。

实证：一个代理改了 `flatCategories` / `listBrands` / `listTags` 三个同包私有函数签名，
**立刻炸出 9 处清单外调用点**，只能整批回退、改成新增函数（原签名保留给既有调用方）。

稳妥顺序是：**先 `rg` 出全部调用点**，再决定「改签名 + 把这些文件一起纳入清单」还是
「新增函数 / 在调用点内联」。同理，一旦要动 `contract/` 或 `service/`，那也已经越出「只改这个 handler」
的边界了 —— 停下来报告，不要就地扩权。

批次执行者的完整工作规则见 `docs/agents/parallel-batch-rules.md`。

### 跨批次写同名 i18n key：允许，但要知道谁会赢

并行批次各自写迁移时，`INSERT ... ON CONFLICT (item_key, lang) DO NOTHING` 保证了**不报错、不重复**，
但**按版本号小的先执行、先写者胜出**。

实测：407 的 7 行只插进去 2 行，其余 5 行与 405/406 重叠被跳过（`MsgInternalError` 的 en-US 取自 405、
`loadFailed*` 取自 406）。合批时要**实测最终落库值**（`SELECT item_value`）而不是读迁移文件，
并记住「后写的那些行是死代码」。

### 做覆盖全部能力的实例页

这是性价比最高的一次集成验证：单组件测试与四五个区块的小案例页都看不出模板截断、盒模型偏移这类缺陷，
只有把全部模式铺在一个长页面里才暴露。

## 交互与动画改动的验证

见 [`frontend.md`](frontend.md) —— 多端适配、四种输入、无头环境与动画/观察者类改动的判据都在那里。
