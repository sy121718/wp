# 商品列表页 N+1 性能基准报告（1 / 20 / 100 三档 SQL 次数与耗时）

> 来源：`/home/sky/桌面/dd.txt` §3.5 P0 性能验收表（第 238 行）、§9 验收清单 A5（第 706 行）、
> §11 合并门槛（第 858 行：「性能报告能证明 SQL 次数不再随商品数量线性增长」）。
> 范围：**只读测量**。本文档新增一个测试文件（`internal/module/product/service/product_list_page_bench_test.go`）
> 与本文档本身；**不改生产代码、不改既有判据语义**、不新增依赖。
> 结论一句话：改造后**页内 SQL 条数恒为 16 条（探针口径）**，与工程内商品数、页码都无关，逐商品详情查询 0 次；
> 1 / 20 / 100 三档整页墙钟耗时中位数落在 1.0–2.3ms（五次复跑区间），**没有随商品数增长的那一项**。
> 残余的线性项不在 SQL 条数层面，而在单条语句自身的工作量（`count(*)` 与列表 `ORDER BY … LIMIT/OFFSET` 要看过的行数）—— 见 §5。

---

## 1. 验收项对照

| 验收项（dd.txt §3.5） | 通过标准 | 实测（本报告 §3 / §4） | 依据 |
|---|---|---|---|
| 逐商品详情查询 | 列表行组装期间 0 次 | **0 次**（三档 + 100 档第 2..5 页） | `product_list_page_sql_count_test.go:162` 硬断言 |
| 评分查询 | 按商品批量汇总，不逐条查 | 每页 1 条 `IN (本页 id)`（`product_list_page_bench_test.go` 语句清单第 16 条） | §3.2 语句清单 |
| SQL 次数 | 不再随商品数线性增长；记录固定开销与批次开销 | **16 / 16 / 16**（1 / 20 / 100 档），100 档翻 5 页同样 16 | §3.1、§3.3 |
| 1 / 20 / 100 条商品均有耗时基准（§9 A5） | 三档都有 wall-clock | n=1 1.03–1.83ms / n=20 1.70–2.25ms / n=100 1.20–1.60ms（**中位数，五次执行区间**） | §4 |
| 性能报告证明不再线性增长（§11） | 有报告 + 可复放证据 | 本报告 + §6 的变红实验 + §7 的可复放命令 | — |

## 2. 测量口径

- **页内 SQL 条数**：gorm 回调计数（`db.Callback().Query().After("gorm:query")` + `Raw().After("gorm:raw")`），
  探针实现在 `internal/module/product/service/product_list_page_sql_count_test.go:61`（`attachSQLCountProbe`）。
  事务边界里的 `set_config('app.project_id', …)`（`rls.InProjectScope` 每条带作用域的读都会设一次）也算在内 ——
  它们是真实往返，不是测试噪音。**BEGIN / COMMIT 不计入**（驱动层发出，不走 gorm 回调）。
- **逐商品详情查询**：语句同时含 `FROM "products"` 与 `WHERE id = `（`s.Get` → `model.Get` → `.First()` 的形状），
  判定在 `product_list_page_sql_count_test.go:92`（`perProductDetailLookups`）。
- **三档定义**：1 / 20 / 100 个**同一工程内累积插入**的商品。每页 20 行（`ProductListPageSize = 20`，
  `product_list_page.go:44`），所以 `n=100` 档是「工程里有 100 个商品、本页 20 行」；100 档另测第 2 页。
- **耗时**：`time.Now() / time.Since` 包住一次 `ProductListPage` 调用（整页取数 + 行装配 + 视图装配，不含 HTTP 与模板渲染）。
  每档热身 1 次（连接池、prepared statement、PG 计划缓存都不是被测对象）后连测 5 次，报告**中位数**，
  同时保留 min / max 与逐次明细。**耗时不做任何阈值断言**（理由见 §4.3）。
- **环境**：linux/amd64（16 核）、Go 1.26.4、PostgreSQL 18.6（Homebrew）、`127.0.0.1:5432`、
  测试库由 `public/test/support.NewMigratedPGTestDB(t)` 复制迁移模板库（表结构来自生产迁移）。

## 3. SQL 次数：改造前后对照

### 3.1 三档总数

| 档位（工程内商品数） | 本页行数 | 改造前（task-154 基线） | 改造后（本次实测，探针口径） |
|---|---|---|---|
| 1 | 1 | 20 条，逐商品详情 2 次 | **16 条，逐商品详情 0 次** |
| 20 | 20 | 153 条，逐商品详情 40 次 | **16 条，0 次** |
| 100 | 20（每页 20 行） | 153 条，逐商品详情 40 次 | **16 条，0 次** |
| 100 · 第 2..5 页 | 20 | 每行补一次，随页码重复 | **每页各 16 条，0 次** |

改造前数字来自 task-154 的基线实测（已随该批合入 main），本轮**未重测**：
重测需要回退已合入的重构（超出本批边界，且 `git checkout` 属禁用操作）。由其数字反推的斜率可用作对照：
`(153 − 20) / (20 − 1) = 7 条/行`，即改造前每多一行 +7 条语句、其中 2 条是逐商品详情形状；
固定部分 `20 − 7 = 13 条`。

### 3.2 改造后 16 条的逐条清单（n=100 第 1 页实测，`product_list_page_bench_test.go` 的语句清单输出）

| # | 语句（形如 gorm 生成的占位符 SQL，`$n` 为参数位） | 归属 | 开销类别 |
|---|---|---|---|
| 1 | `SELECT set_config('app.project_id', $1, true)` | 分类树作用域 | 页级固定 |
| 2 | `SELECT * FROM "product_categories" ORDER BY sort ASC, create_time ASC, id ASC` | `ListCategories` | 页级固定 |
| 3 | `SELECT set_config('app.project_id', $1, true)` | 品牌作用域 | 页级固定 |
| 4 | `SELECT * FROM "product_brands" ORDER BY sort ASC, create_time ASC, id ASC` | `ListBrands` | 页级固定 |
| 5 | `SELECT set_config('app.project_id', $1, true)` | 标签作用域 | 页级固定 |
| 6 | `SELECT * FROM "product_tags" ORDER BY sort ASC, create_time ASC, id ASC` | `ListTags` | 页级固定 |
| 7 | `SELECT set_config('app.project_id', $1, true)` | 列表作用域 | 本页批次 |
| 8 | `SELECT id, project_id, name, slug, sku_code, type, status, images, sort, default_image, create_time, update_time FROM "products" ORDER BY sort ASC, create_time DESC LIMIT $1`（+`OFFSET`） | `List` | 本页批次 |
| 9 | `SELECT * FROM "product_variants" WHERE product_id IN ($1,…,$20) ORDER BY sort ASC` | `ListVariantsByProducts` | 本页批次 |
| 10 | `SELECT set_config('app.project_id', $1, true)` | 总数作用域 | 页级固定 |
| 11 | `SELECT count(*) FROM "products"` | `CountProducts` | 页级固定 |
| 12 | `SELECT set_config('app.project_id', $1, true)` | 属性组作用域 | 页级固定 |
| 13 | `SELECT * FROM "product_attributes" ORDER BY sort ASC, create_time ASC LIMIT $1` | `ListAttributes` | 页级固定 |
| 14 | `SELECT set_config('app.project_id', $1, true)` | 挂载事实作用域 | 本页批次 |
| 15 | `SELECT set_config('app.project_id', $1, true)` | 评分作用域 | 本页批次 |
| 16 | `SELECT * FROM "product_ratings" WHERE project_id = $1 AND product_id IN ($2,…,$21) ORDER BY product_id, create_time DESC, id DESC` | `RatingSummariesByProducts` | 本页批次 |

**分类汇总（这三类就是 dd.txt §3.5 要的「固定开销」与「批次开销」）**：

| 开销类别 | 条数 | 随下列量变化 | 改造前 |
|---|---|---|---|
| 页级固定 | **10 条** | 不随商品数、不随本页行数、不随页码变 | 13 条（固定部分反推值） |
| 本页批次（按本页 id 批量） | **6 条**（探针口径；真实 7 条，见 §6.2） | 语句条数固定；只有 `IN` 列表长度 = 本页行数（≤20） | 7 条/行 |
| 逐行（行组装期间） | **0 条** | — | 7 条/行，其中 2 条是逐商品详情形状 |

第 14 条与第 15 条之间**没有查询语句**，不是漏印：挂载事实读 `ListRowFacts` 走 `.Scan()`，
在 gorm 里落到 **Row 回调链**（`Rows()` → `gorm:row`），而探针只挂了 Query 与 Raw 两条链 ⇒ 每条被漏计 1 条。
所以**真实语句条数是 17 条/页**，探针口径报 16。这条盲区的实测证据在 §6.2。

### 3.3 恒定性的直接证据（判据文件里的真实输出，`-v`）

```text
product_list_page_sql_count_test.go:167: n=1（本页 1 行）：SQL 总次数=16，逐商品详情查询=0
product_list_page_sql_count_test.go:167: n=20（本页 20 行）：SQL 总次数=16，逐商品详情查询=0
product_list_page_sql_count_test.go:167: n=100（本页 20 行）：SQL 总次数=16，逐商品详情查询=0
product_list_page_sql_count_test.go:192: 100 商品第 2 页：SQL 总次数=16（与 1 商品页相同）
product_list_page_sql_count_test.go:192: 100 商品第 3 页：SQL 总次数=16（与 1 商品页相同）
product_list_page_sql_count_test.go:192: 100 商品第 4 页：SQL 总次数=16（与 1 商品页相同）
product_list_page_sql_count_test.go:192: 100 商品第 5 页：SQL 总次数=16（与 1 商品页相同）
```

## 4. 耗时基准（1 / 20 / 100 三档）

### 4.1 三档实测

一次完整运行的逐次输出（`product_list_page_bench_test.go:73` / `:76`）：

```text
n=  1 第 1 页：行数= 1 SQL=16 逐商品详情=0 | 耗时 min=1.098882ms 中位=1.375592ms max=1.844862ms（5 次：1.098882ms / 1.254824ms / 1.375592ms / 1.659634ms / 1.844862ms）
n= 20 第 1 页：行数=20 SQL=16 逐商品详情=0 | 耗时 min=1.379989ms 中位=2.246426ms max=2.873412ms（5 次：1.379989ms / 1.383937ms / 2.246426ms / 2.324212ms / 2.873412ms）
n=100 第 1 页：行数=20 SQL=16 逐商品详情=0 | 耗时 min=1.250337ms 中位=1.600624ms max=3.420399ms（5 次：1.250337ms / 1.41218ms / 1.600624ms / 1.79077ms / 3.420399ms）
n=100 第 2 页：行数=20 SQL=16 逐商品详情=0 | 耗时 min=1.001049ms 中位=1.293117ms max=2.087417ms（5 次：1.001049ms / 1.219048ms / 1.293117ms / 1.452246ms / 2.087417ms）
```

五次独立执行（每次执行内部连测 5 次、取中位数）的**中位数区间**：

| 档位 | 中位数区间（5 次复跑） | min 观测 | max 观测 | 本页行数 |
|---|---|---|---|---|
| n=1 | **1.03 – 1.83 ms** | 0.90 ms | 2.86 ms | 1 |
| n=20 | **1.70 – 2.25 ms** | 1.09 ms | 3.12 ms | 20 |
| n=100 第 1 页 | **1.20 – 1.60 ms** | 1.07 ms | 3.42 ms | 20 |
| n=100 第 2 页 | **1.29 – 1.59 ms** | 1.00 ms | 2.96 ms | 20 |
| 注入缺陷后（对照，§6.1） | n=1 2.38 / n=20 7.51 / n=100 7.05 ms | — | 9.75 ms | 同上 |

### 4.2 怎么读这组数字

- **1 行页 vs 20 行页**：1.03–1.83ms → 1.70–2.25ms，差 ~0.5ms。这一项是**逐行装配**（行装配 + 挂载事实投影 + 视图准备），
  与 SQL 条数无关（两档都是 16 条），属于「本页行数」的成本，不是「商品总数」的成本。
- **20 行页的 n=20 vs n=100**：页面工作量完全相同（各 20 行、同样 16 条语句），差别只有工程内行数（20 vs 100）。
  实测中位数互有高低（1.70–2.25ms vs 1.20–1.60ms），**差异落在抖动区间内** ⇒ 在 100 行这个规模上，
  「语句要扫过的行数」这一项还测不出量级。
- **第 2 页与第 1 页同量级**（1.29–1.59ms）⇒ `OFFSET` 分页没有引入逐行往返。

### 4.3 为什么耗时不做硬断言

同一台机器、同一份代码，n=1 档的中位数在五次复跑里从 **1.83ms 到 1.03ms**（差 0.8ms，约 78%），
n=20 档从 1.70ms 到 2.25ms。任何阈值断言都会在这里随机变红，而**变红那次不指向任何代码缺陷** ——
flaky 的门禁会被人关掉，然后连 §3.3 那条真判据也一起没人看。所以本文件**只记录**：
耗时进日志与报告、由人复核；会变红的两条判据（逐商品详情 == 0、三档次数相等）留在 `product_list_page_sql_count_test.go`。

## 5. 结论依据：为什么 SQL 次数不再随商品数线性增长

1. **逐商品详情查询已从列表路径删除**：`ProductListPage`（`product_list_page.go:52`）只在末尾调一次
   `productRowsFromList`（`product_row.go:184`），后者对整页 id 做两次批量读（`ListRowFacts`、`RatingSummariesByProducts`），
   循环体里没有任何取数调用（`product_row.go:205-216`）。
2. **条数与商品数解耦**：16 条里 10 条是页级固定、6 条是本页批次；两类的**条数**都与工程内商品数无关。
   唯一的「行数相关量」是 `IN (…)` 列表的长度，而它等于本页行数（≤ `ProductListPageSize` = 20），不随工程商品数增长。
3. **两个方向的实测都对齐**：三档总次数相等（§3.3）；100 档翻第 2..5 页同样每条 16（§3.3）；
   反过来施加回归（§6.1 注入逐商品 `s.Get`）时条数立刻变成 19 / 76 / 76，即每行 +3 条 —— 线性项一旦回来就可见。

**残余的线性项（诚实交代）**：SQL **条数**是常数，但两条语句的**工作量**仍与工程内商品数同阶 ——
`count(*)`（第 11 条）必须数完工程内所有 `products` 行；列表查询（第 8 条）在 `ORDER BY sort, create_time DESC`
之下要用索引取出前 `OFFSET+LIMIT` 行，`OFFSET` 越大要跳过的索引项越多。本报告的规模上限是 100 行，
测不出这两项在 10^4–10^5 行下的量级；它属于「条数常数、单条成本随行数增长」的形态，与 dd.txt 要消除的
「随商品数线性增长（每行一次往返）」不是同一类问题（登记为 §8 的未覆盖项）。

## 6. 变红实验（判据必须会红，且红得可定位）

两次注入都改在 `productRowsFromList` 的装配循环里（`product_row.go:205-216`），注入后跑判据、记录输出、
再用 `edit` 精确还原（**未使用 `git checkout`**），还原后 `git diff` 为空（`product_row.go` 无改动）。

### 6.1 注入一：逐商品 `s.Get`（N+1 的本体）

```go
// 注入到 product_row.go 的 productRowsFromList 装配循环里：
applyRowFacts(item, facts[item.ID])
if _, gerr := s.Get(ctx, &productdto.GetReq{ProjectID: projectID, ID: item.ID}); gerr != nil {
    return nil, gerr
}
```

判据（`TestProductListPageSQLCountIsConstantInProductCount`）**FAIL**，真实输出：

```text
=== RUN   TestProductListPageSQLCountIsConstantInProductCount
    product_list_page_sql_count_test.go:163: n=1：行组装期间发了 1 次逐商品详情查询（判据是 0 次）。命中语句：
        SELECT set_config('app.project_id', $1, true)
        SELECT * FROM "product_categories" ORDER BY sort ASC, create_time ASC, id ASC
        …（略：品牌 / 标签 / 列表 / 变体 / 总数 / 属性组 / 评分，与 §3.2 同）…
        SELECT * FROM "products" WHERE id = $1 ORDER BY "products"."id" LIMIT $2
        SELECT * FROM "product_variants" WHERE product_id = $1 ORDER BY sort ASC, create_time ASC
--- FAIL: TestProductListPageSQLCountIsConstantInProductCount (0.15s)
FAIL	go_wp/internal/module/product/service	0.211s
```

同一注入下**耗时基准**的记录值（它只记录、不断言；红点由判据负责）：

```text
n=  1 第 1 页：行数= 1 SQL=19 逐商品详情=1 | 耗时 min=2.20049ms  中位=2.378954ms max=3.06964ms
n= 20 第 1 页：行数=20 SQL=76 逐商品详情=20 | 耗时 min=5.894833ms 中位=7.514673ms max=9.747182ms
n=100 第 1 页：行数=20 SQL=76 逐商品详情=20 | 耗时 min=5.832456ms 中位=7.049249ms max=8.730584ms
n=100 第 2 页：行数=20 SQL=76 逐商品详情=20 | 耗时 min=5.530669ms 中位=6.796345ms max=9.017443ms
```

即每行 +3 条计数语句（`set_config` + `products WHERE id =` + `product_variants WHERE product_id =`），
20 行页的整页耗时中位数从 1.6–2.2ms 涨到 7.0–7.5ms（约 4 倍）。

### 6.2 注入二：逐商品 `ListRowFacts`（把批量事实读改回逐商品读）

```go
// 注入到同一个循环里：
if _, gerr := s.m.ListRowFacts(ctx, projectID, []string{item.ID}); gerr != nil {
    return nil, gerr
}
```

这条注入走 `.Scan()` → **Row 回调链**，于是判据给出的是**另一种红**，真实输出：

```text
product_list_page_sql_count_test.go:167: n=1（本页 1 行）：SQL 总次数=17，逐商品详情查询=0
product_list_page_sql_count_test.go:167: n=20（本页 20 行）：SQL 总次数=36，逐商品详情查询=0
product_list_page_sql_count_test.go:167: n=100（本页 20 行）：SQL 总次数=36，逐商品详情查询=0
product_list_page_sql_count_test.go:170: SQL 次数随商品数变化：1→17、20→36、100→36（页内取数必须是常数条）
--- FAIL: TestProductListPageSQLCountIsConstantInProductCount (0.16s)
```

两点结论：

1. **「总次数相等」这条判据兜住了它**（每行多出的 `set_config` 被计数）—— 两段判据里它是更强的网。
2. **「逐商品详情查询 == 0」这条判据对 `.Scan()` 形状是盲的**（仍报 0 次）：它只认 `FROM "products"` + `WHERE id = ` 这一种形状。
   这与 §3.2 的漏计是同一根源（探针只挂 Query / Raw 两条链）。**当前这不是漏洞**（列表路径上唯一的 Scan 读是批量 `ListRowFacts`），
   但「往循环里加一条 `.Scan()` 的逐商品读」这类回归只能靠总次数那条抓，形状判据抓不到。

顺带由两次注入定量出探针的漏计额：注入二在 n=1 时是 17 条（比干净版多 1 条 `set_config`），
而 n=1 时「逐商品事实读」与「批量事实读」产生的 SELECT 条数相同 ⇒ 干净版**每页真实语句数 17 条**（16 条计数 + 1 条未被计数的 `ListRowFacts` 的 SELECT）。

### 6.3 还原后复跑全绿

```text
--- PASS: TestProductListPageWallClockByProductCount (0.20s)
--- PASS: TestProductListPageSQLCountIsConstantInProductCount (0.13s)
PASS
ok  	go_wp/internal/module/product/service	0.391s
```

`git status --porcelain` 在还原后只剩本批新增的两个文件（测试文件 + 本报告），`product_row.go` 无残留 diff。

## 7. 可复放命令

```bash
# 耗时基准（1/20/100 三档 + 100 档第 2 页；只记录、不断言）
go test ./internal/module/product/service/ -run TestProductListPageWallClockByProductCount -count=1 -v

# SQL 次数判据（逐商品详情 == 0、三档次数相等、100 档翻第 2..5 页）
go test ./internal/module/product/service/ -run TestProductListPageSQLCountIsConstantInProductCount -count=1 -v

# 两档一起跑（报告 §3.3 与 §4.1 的输出就来自这一条）
go test ./internal/module/product/service/ -run 'TestProductListPageWallClockByProductCount|TestProductListPageSQLCountIsConstantInProductCount' -count=1 -v

# 构建验收（测试库由 support.NewMigratedPGTestDB 复制迁移模板库，本机 PostgreSQL 127.0.0.1:5432）
go build ./...
```

## 8. 未做与已知边界

| 项 | 状态 | 原因 |
|---|---|---|
| 重测「改造前」的 20 / 153 / 153 条 | **未做** | 需要回退已合入 main 的重构（且 `git checkout` 禁用）；引用 task-154 基线并标注来源 |
| 10^4–10^5 行规模下的耗时 | **未做** | 本批边界是 1/20/100 三档（dd.txt §3.5）；残余的线性项见 §5 末段 |
| pprof / 内存分配 | **未做** | 没有指标要求；SQL 条数已能回答合并门槛 |
| CI 里的耗时阈值门禁 | **刻意不做** | §4.3：会 flaky 的门禁最终会被关掉 |
| 探针补挂 Row 链（把 16 改成 17） | **未做** | 会改动判据文件的口径与既有断言数字；登记为发现（§9.1），由主会话决定是否单开一批 |

## 9. 清单外发现

1. **探针漏计 gorm Row 链**（`.Scan()` / `.Rows()`）：`internal/module/product/service/product_list_page_sql_count_test.go:61` 的
   `attachSQLCountProbe` 只挂 Query 与 Raw，本页漏 1 条（`ListRowFacts` 的 SELECT）⇒ 真实 17 条/页、判据报 16 条。
   三档口径一致，**不影响**「三档相等」与「逐商品详情 == 0」两条判据的成立；但形状判据对 `.Scan()` 形状的逐商品读是盲的（§6.2 实测）。
2. **改造后的固定开销比改造前略高**：页级固定 10 条 vs 改造前反推的 13 条（含成对 `set_config`）。
   16 条里 **8 条是 `rls.InProjectScope` 的 `set_config`**（每个带作用域的读开一次事务）——
   若要继续压固定开销，方向是**合并作用域**（一次事务里做完本页读），不是回到逐商品读。
3. **`n=20` 与 `n=100` 第 1 页的页面工作量完全相同**（各 20 行、各 16 条语句），因此这两档的耗时差**只能**反映
   「表里行数」这一项 —— 它是 §5 末段那条残余线性项最省事的观测口：将来若要在更大规模上量化，
   加一档 `n=10000`（页面仍是 20 行）就能把「行数成本」单独读出来，不必动判据。
4. **`product_list_page_bench_test.go` 的前提校验**（读模型失败 / `LoadFailed` / 成品行 0 条即 `t.Fatalf`）
   是「测得准」的前提，不是新判据；它保证不会把一次失败调用的耗时记进报告。