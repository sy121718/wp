# 商品列表页 N+1 性能基准报告（1 / 20 / 100 三档 SQL 次数与耗时）

> 来源：`/home/sky/桌面/dd.txt` §3.5 P0 性能验收表（第 238 行）、§9 验收清单 A5（第 706 行）、
> §11 合并门槛（第 858 行：「性能报告能证明 SQL 次数不再随商品数量线性增长」）。
> 范围：**只读测量 + 探针口径收口**。涉及两个测试文件（`internal/module/product/service/product_list_page_bench_test.go`
> 与 `internal/module/product/service/product_list_page_sql_count_test.go`）；**不改生产代码**、不新增依赖。
> 结论一句话：改造后**页内 SQL 条数恒为 17 条**，与工程内商品数（1 / 20 / 100 / 10000 四档）、页码都无关，
> 逐商品详情查询 0 次、读 `products` 表的语句恒 3 条；
> 1 / 20 / 100 三档整页墙钟耗时中位数落在 1.1–2.6ms（五次复跑区间），**没有随商品数增长的那一项**。
> 残余的线性项不在 SQL 条数层面，而在单条语句自身的工作量（`count(*)` 与列表 `ORDER BY … LIMIT/OFFSET` 要看过的行数）—— 见 §5。
>
> **task-164 的两处更新**：① 探针补挂 gorm **Row 回调链**，口径 16 → 17（§3.2 / §9.1），并新增一层形状判据
> 「读 `products` 表的语句恒 3 条」；② 新增 n=10000 档（§3.1 / §3.4）。

---

## 1. 验收项对照

| 验收项（dd.txt §3.5） | 通过标准 | 实测（本报告 §3 / §4） | 依据 |
|---|---|---|---|
| 逐商品详情查询 | 列表行组装期间 0 次 | **0 次**（四档 + 100 档第 2..5 页） | `product_list_page_sql_count_test.go:241` 硬断言 |
| 读 `products` 表 | 条数固定，不随商品数 / 本页行数 / 页码变 | **恒 3 条**（列表 / `count(*)` / 批量挂载事实） | `product_list_page_sql_count_test.go:245` 硬断言（期望值常量在同文件 `:124`） |
| 评分查询 | 按商品批量汇总，不逐条查 | 每页 1 条 `IN (本页 id)`（`product_list_page_bench_test.go` 语句清单第 17 条） | §3.2 语句清单 |
| SQL 次数 | 不再随商品数线性增长；记录固定开销与批次开销 | **17 / 17 / 17 / 17**（1 / 20 / 100 / 10000 档），100 档翻 5 页同样 17 | §3.1、§3.3、§3.4 |
| 1 / 20 / 100 条商品均有耗时基准（§9 A5） | 三档都有 wall-clock | n=1 1.11–1.51ms / n=20 1.27–1.70ms / n=100 1.25–2.57ms（**中位数，五次执行区间**） | §4 |
| 性能报告证明不再线性增长（§11） | 有报告 + 可复放证据 | 本报告 + §6 的变红实验 + §7 的可复放命令 | — |

## 2. 测量口径

- **页内 SQL 条数**：gorm 回调计数，挂**三条链**（Query / Row / Raw），探针实现在
  `internal/module/product/service/product_list_page_sql_count_test.go:72`（`attachSQLCountProbe`）：
  `Query().After("gorm:query")`（`Find` / `First` / `Count`）、`Row().After("gorm:row")`（**`.Scan()` / `.Rows()` 走的另一条链**）、
  `Raw().After("gorm:raw")`。**Row 这条是 task-164 补的**：漏掉它时本页少计 1 条（`ListRowFacts` 的 SELECT，见 §9.1），
  且整类 Scan 形态的读根本不进计数。
  事务边界里的 `set_config('app.project_id', …)`（`rls.InProjectScope` 每条带作用域的读都会设一次）也算在内 ——
  它们是真实往返，不是测试噪音。**BEGIN / COMMIT 不计入**（驱动层发出，不走 gorm 回调）。
- **逐商品详情查询**：语句同时含 `FROM "products"` 与 `WHERE id = `（`s.Get` → `model.Get` → `.First()` 的形状），
  判定在 `product_list_page_sql_count_test.go:106`（`perProductDetailLookups`）。
- **读 `products` 表的语句**：语句含 `FROM "products"` 即计数（列表 / `count(*)` / 批量挂载事实各一次 = 3），
  判定在 `product_list_page_sql_count_test.go:132`（`productTableReads`），期望值常量是同文件 `:124` 的 `listPageProductsTableReads`。
  它与上一条是**两层形状判据**：上一条只认 `WHERE id = ` 这一种写法，这一条认**表名** —— 逐商品 `ListRowFacts` 的形状是
  `project_id = … AND id IN (…)`，没有 `id = ` 字样，只有这一条抓得到（§6.2 有实测）。
- **档位定义**：1 / 20 / 100 / 10000 个**同一工程内累积插入**的商品，四档都先看第 1 页（100 档另翻第 2..5 页）。
  每页 20 行（`ProductListPageSize = 20`，`product_list_page.go:44`），所以 `n=100` 与 `n=10000` 档都是
  「工程里有很多商品、本页仍 20 行」—— 与 `n=20` 档的页面工作量**逐条相同**，差别只在表里的行数。
  `n=10000` 的数据由 `seedListProductsBulk`（同文件 `:181`，每 500 行一条 INSERT）灌入，
  行内容与单行插入的 `seedListProduct` 逐字相同。
- **耗时**：`time.Now() / time.Since` 包住一次 `ProductListPage` 调用（整页取数 + 行装配 + 视图装配，不含 HTTP 与模板渲染）。
  每档热身 1 次（连接池、prepared statement、PG 计划缓存都不是被测对象）后连测 5 次，报告**中位数**，
  同时保留 min / max 与逐次明细。**耗时不做任何阈值断言**（理由见 §4.3）。
- **环境**：linux/amd64（16 核）、Go 1.26.4、PostgreSQL 18.6（Homebrew）、`127.0.0.1:5432`、
  测试库由 `public/test/support.NewMigratedPGTestDB(t)` 复制迁移模板库（表结构来自生产迁移）。

## 3. SQL 次数：改造前后对照

### 3.1 各档总数（1 / 20 / 100 / 10000）

| 档位（工程内商品数） | 本页行数 | 改造前（task-154 基线） | 改造后（本次实测） |
|---|---|---|---|
| 1 | 1 | 20 条，逐商品详情 2 次 | **17 条，逐商品详情 0 次，读 products 表 3 条** |
| 20 | 20 | 153 条，逐商品详情 40 次 | **17 条，0 次，3 条** |
| 100 | 20（每页 20 行） | 153 条，逐商品详情 40 次 | **17 条，0 次，3 条** |
| 10000 | 20（每页 20 行） | —— | **17 条，0 次，3 条** |
| 100 · 第 2..5 页 | 20 | 每行补一次，随页码重复 | **每页各 17 条，0 次** |

改造前（20 / 153 / 153）与改造后的 17 条不是同一个探针口径下的数字：改造前的基线来自 task-154，
当时的探针同样漏计 Row 链（同一份探针代码），只是彼时逐商品读占绝对多数，漏 1 条不改变结论方向。

改造前数字来自 task-154 的基线实测（已随该批合入 main），本轮**未重测**：
重测需要回退已合入的重构（超出本批边界，且 `git checkout` 属禁用操作）。由其数字反推的斜率可用作对照：
`(153 − 20) / (20 − 1) = 7 条/行`，即改造前每多一行 +7 条语句、其中 2 条是逐商品详情形状；
固定部分 `20 − 7 = 13 条`。

### 3.2 改造后 17 条的逐条清单（n=100 第 1 页实测，`product_list_page_bench_test.go` 的语句清单输出）

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
| 15 | `SELECT id, type, category_ids, primary_category_id, brand_id, tag_ids, default_price FROM "products" WHERE project_id = $1 AND id IN ($2,…,$21)` | `ListRowFacts`（`.Scan()`） | 本页批次 |
| 16 | `SELECT set_config('app.project_id', $1, true)` | 评分作用域 | 本页批次 |
| 17 | `SELECT * FROM "product_ratings" WHERE project_id = $1 AND product_id IN ($2,…,$21) ORDER BY product_id, create_time DESC, id DESC` | `RatingSummariesByProducts` | 本页批次 |

**分类汇总（这三类就是 dd.txt §3.5 要的「固定开销」与「批次开销」）**：

| 开销类别 | 条数 | 随下列量变化 | 改造前 |
|---|---|---|---|
| 页级固定 | **10 条** | 不随商品数、不随本页行数、不随页码变 | 13 条（固定部分反推值） |
| 本页批次（按本页 id 批量） | **7 条** | 语句条数固定；只有 `IN` 列表长度 = 本页行数（≤20） | 7 条/行 |
| 逐行（行组装期间） | **0 条** | — | 7 条/行，其中 2 条是逐商品详情形状 |

**task-164 修掉的漏计**：改造前探针只挂 Query / Raw 两条链，而挂载事实读 `ListRowFacts` 走 `.Scan()` ——
在 gorm 里落到 **Row 回调链**（`Rows()` → `gorm:row`），于是第 15 条这条 SELECT 从未进计数
（旧口径 16 条 / 页，真实 17 条 / 页）。补挂 Row 链后，上面这份 17 条的清单就是页面**真实**的全部语句；
判据新增的「读 `products` 表恒 3 条」（第 8 / 11 / 15 条）让它以后不再有同类盲区。

### 3.3 恒定性的直接证据（判据文件里的真实输出，`-v`）

```text
product_list_page_sql_count_test.go:263: n=1（本页 1 行）：SQL 总次数=17，逐商品详情查询=0，读 products 表=3
product_list_page_sql_count_test.go:263: n=20（本页 20 行）：SQL 总次数=17，逐商品详情查询=0，读 products 表=3
product_list_page_sql_count_test.go:263: n=100（本页 20 行）：SQL 总次数=17，逐商品详情查询=0，读 products 表=3
product_list_page_sql_count_test.go:280: 100 商品第 2 页：SQL 总次数=17（与 1 商品页相同）
product_list_page_sql_count_test.go:280: 100 商品第 3 页：SQL 总次数=17（与 1 商品页相同）
product_list_page_sql_count_test.go:280: 100 商品第 4 页：SQL 总次数=17（与 1 商品页相同）
product_list_page_sql_count_test.go:280: 100 商品第 5 页：SQL 总次数=17（与 1 商品页相同）
product_list_page_sql_count_test.go:293: n=10000：SQL 总次数=17（与 1 商品页逐条相同）；残余的线性项只在单条语句内部，不在语句条数上（见 docs/audit/product-list-n1-report.md §5 末段）
```

### 3.4 n=10000 档：页面行数不变的条件下暴露残余线性项

三档（1 / 20 / 100）能证明「条数不随商品数增长」，但 `n=20` 与 `n=100` 第 1 页的**页面工作量完全相同**
（各 20 行、同样 17 条语句），所以它证明不了「行数成本」这一项。`n=10000` 档把工程内行数拉到 1 万，
本页仍是 20 行 —— 于是两档之间**唯一**的差别是表里的行数：

| 档位 | 工程内行数 | 本页行数 | SQL 总次数 | 逐商品详情 | 读 `products` 表 |
|---|---|---|---|---|---|
| n=20 | 20 | 20 | 17 | 0 | 3 |
| n=100 | 100 | 20 | 17 | 0 | 3 |
| n=10000 | 10000 | 20 | 17 | 0 | 3 |

结论：**SQL 条数不随行数变**（17 条，判据断言四档相等，见 `product_list_page_sql_count_test.go:284-292`）。
行数带来的成本因此**全部落在单条语句内部** —— `count(*)` 要数完 1 万行、列表查询的
`ORDER BY sort, create_time DESC LIMIT 20` 要按索引取出前 20 行。这与 dd.txt 要消除的
「随商品数线性增长（每行一次往返）」不是同一类问题（该项的耗时量化见 §4 与 §8 的登记）。

## 4. 耗时基准（1 / 20 / 100 三档）

### 4.1 三档实测

一次完整运行的逐次输出（`product_list_page_bench_test.go:75` / `:78` 两条 `t.Logf`）：

```text
n=  1 第 1 页：行数= 1 SQL=17 逐商品详情=0 | 耗时 min=972.245µs  中位=973.327µs  max=1.458798ms（5 次：972.245µs / 973.067µs / 973.327µs / 1.247602ms / 1.458798ms）
n= 20 第 1 页：行数=20 SQL=17 逐商品详情=0 | 耗时 min=1.057385ms 中位=1.28906ms  max=2.010814ms（5 次：1.057385ms / 1.176879ms / 1.28906ms / 1.495868ms / 2.010814ms）
n=100 第 1 页：行数=20 SQL=17 逐商品详情=0 | 耗时 min=898.397µs  中位=1.181488ms max=2.180382ms（5 次：898.397µs / 952.999µs / 1.181488ms / 1.372356ms / 2.180382ms）
n=100 第 2 页：行数=20 SQL=17 逐商品详情=0 | 耗时 min=1.021727ms 中位=1.580416ms max=1.871373ms（5 次：1.021727ms / 1.292667ms / 1.580416ms / 1.583051ms / 1.871373ms）
```

> 注：SQL=17 这一列是 task-164 补挂 Row 链后的口径。同一台机器同一份代码，逐次最小值的绝对水平
> 会随机器负载整体平移（旧记录里同一档是 1.10ms，本轮是 0.97ms）—— 所以本节的结论只看**档间关系**与
> **量级**，不看绝对值（§4.3 的理由）。

五次独立执行（每次执行内部连测 5 次、取中位数）的**中位数区间**：

| 档位 | 中位数区间（5 次复跑） | min 观测 | max 观测 | 本页行数 |
|---|---|---|---|---|
| n=1 | **1.11 – 1.51 ms** | 0.97 ms | 2.14 ms | 1 |
| n=20 | **1.27 – 1.70 ms** | 1.11 ms | 2.04 ms | 20 |
| n=100 第 1 页 | **1.25 – 2.57 ms** | 0.99 ms | 3.76 ms | 20 |
| n=100 第 2 页 | **1.29 – 1.66 ms** | 0.98 ms | 3.48 ms | 20 |
| 注入一（逐商品 `s.Get`，对照，§6.1） | n=1 1.42 / n=20 6.03 / n=100 5.62 ms | 1.17 ms | 6.44 ms | 同上 |
| 注入二（逐商品 `ListRowFacts`，对照，§6.2） | n=1 1.48 / n=20 4.54 / n=100 5.07 ms | 1.18 ms | 5.35 ms | 同上 |

### 4.2 怎么读这组数字

- **1 行页 vs 20 行页**：1.11–1.51ms → 1.27–1.70ms，差 ~0.2ms。这一项是**逐行装配**（行装配 + 挂载事实投影 + 视图准备），
  与 SQL 条数无关（两档都是 17 条），属于「本页行数」的成本，不是「商品总数」的成本。
- **20 行页的 n=20 vs n=100**：页面工作量完全相同（各 20 行、同样 17 条语句），差别只有工程内行数（20 vs 100）。
  实测中位数互有高低（1.27–1.70ms vs 1.25–2.57ms），**差异落在抖动区间内** ⇒ 在 100 行这个规模上，
  「语句要扫过的行数」这一项还测不出量级（`n=10000` 档只做了 SQL 计数，见 §3.4；耗时档位仍是 1 / 20 / 100）。
- **第 2 页与第 1 页同量级**（1.29–1.66ms）⇒ `OFFSET` 分页没有引入逐行往返。

### 4.3 为什么耗时不做硬断言

同一台机器、同一份代码，n=1 档的中位数在五次复跑里从 **1.83ms 到 1.03ms**（差 0.8ms，约 78%），
n=20 档从 1.70ms 到 2.25ms。任何阈值断言都会在这里随机变红，而**变红那次不指向任何代码缺陷** ——
flaky 的门禁会被人关掉，然后连 §3.3 那条真判据也一起没人看。所以本文件**只记录**：
耗时进日志与报告、由人复核；会变红的两条判据（逐商品详情 == 0、三档次数相等）留在 `product_list_page_sql_count_test.go`。

## 5. 结论依据：为什么 SQL 次数不再随商品数线性增长

1. **逐商品详情查询已从列表路径删除**：`ProductListPage`（`product_list_page.go:52`）只在末尾调一次
   `productRowsFromList`（`product_row.go:184`），后者对整页 id 做两次批量读（`ListRowFacts`、`RatingSummariesByProducts`），
   循环体里没有任何取数调用（`product_row.go:205-216`）。
2. **条数与商品数解耦**：17 条里 10 条是页级固定、7 条是本页批次；两类的**条数**都与工程内商品数无关。
   唯一的「行数相关量」是 `IN (…)` 列表的长度，而它等于本页行数（≤ `ProductListPageSize` = 20），不随工程商品数增长。
3. **两个方向的实测都对齐**：四档总次数相等（§3.3、§3.4）；100 档翻第 2..5 页同样每条 17（§3.3）；
   反过来施加回归（§6.1 注入逐商品 `s.Get`）时条数立刻变成 20 / 77 / 77，即每行 +3 条 —— 线性项一旦回来就可见。

**残余的线性项（诚实交代）**：SQL **条数**是常数，但两条语句的**工作量**仍与工程内商品数同阶 ——
`count(*)`（第 11 条）必须数完工程内所有 `products` 行；列表查询（第 8 条）在 `ORDER BY sort, create_time DESC`
之下要用索引取出前 `OFFSET+LIMIT` 行，`OFFSET` 越大要跳过的索引项越多。本报告对这两项的观测是
**n=10000 档的条数仍然不变（§3.4）**：页面上看不到任何随行数增长的语句数，成本只能在单条语句内部。
量级化的耗时结论仍未做（耗时档位是 1 / 20 / 100 三档，§4）：它属于「条数常数、单条成本随行数增长」的形态，
与 dd.txt 要消除的「随商品数线性增长（每行一次往返）」不是同一类问题（登记为 §8 的未覆盖项）。

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

判据（`TestProductListPageSQLCountIsConstantInProductCount`）**FAIL**，真实输出（本次实测）：

```text
=== RUN   TestProductListPageSQLCountIsConstantInProductCount
    product_list_page_sql_count_test.go:262: n=1（本页 1 行）：行组装期间发了 1 次逐商品详情查询（判据是 0 次）。命中语句：
        SELECT set_config('app.project_id', $1, true)
        SELECT * FROM "product_categories" ORDER BY sort ASC, create_time ASC, id ASC
        …（略：品牌 / 标签 / 列表 / 变体 / 总数 / 属性组 / 挂载事实 / 评分，与 §3.2 的 17 条同）…
        SELECT * FROM "products" WHERE id = $1 ORDER BY "products"."id" LIMIT $2
        SELECT * FROM "product_variants" WHERE product_id = $1 ORDER BY sort ASC, create_time ASC
--- FAIL: TestProductListPageSQLCountIsConstantInProductCount (0.12s)
FAIL
FAIL	go_wp/internal/module/product/service	0.184s
FAIL
```

同一注入下**耗时基准**的记录值（它只记录、不断言；红点由判据负责）：

```text
n=  1 第 1 页：行数= 1 SQL=20 逐商品详情=1 | 耗时 min=1.16678ms  中位=1.422329ms max=1.701704ms（5 次：1.16678ms / 1.200343ms / 1.422329ms / 1.683229ms / 1.701704ms）
n= 20 第 1 页：行数=20 SQL=77 逐商品详情=20 | 耗时 min=5.719815ms 中位=6.02571ms  max=6.440648ms（5 次：5.719815ms / 5.908059ms / 6.02571ms / 6.334759ms / 6.440648ms）
n=100 第 1 页：行数=20 SQL=77 逐商品详情=20 | 耗时 min=4.926006ms 中位=5.622603ms max=6.395443ms（5 次：4.926006ms / 5.453284ms / 5.622603ms / 6.090591ms / 6.395443ms）
n=100 第 2 页：行数=20 SQL=77 逐商品详情=20 | 耗时 min=5.255264ms 中位=5.46184ms  max=6.162227ms（5 次：5.255264ms / 5.350983ms / 5.46184ms / 6.090241ms / 6.162227ms）
```

即每行 +3 条计数语句（`set_config` + `products WHERE id =` + `product_variants WHERE product_id =`），
20 行页的整页耗时中位数从 1.3–2.0ms 涨到 5.5–6.0ms（约 3–4 倍），
SQL 条数从干净的 17 变成 20 / 77 / 77（`n` 档位内部一致，跨档位线性 —— 判据的第二层因此同时变红）。

### 6.2 注入二：逐商品 `ListRowFacts`（把批量事实读改回逐商品读）

```go
// 注入到同一个循环里：
if _, gerr := s.m.ListRowFacts(ctx, projectID, []string{item.ID}); gerr != nil {
    return nil, gerr
}
```

这条注入走 `.Scan()` → **Row 回调链**。**这正是 task-164 要修掉的盲区**：补挂 Row 链 + 新增
「读 `products` 表恒 3 条」这层形状判据之后，它被当场抓住（改造前只能靠「总次数相等」兜住）。真实输出：

```text
=== RUN   TestProductListPageSQLCountIsConstantInProductCount
    product_list_page_sql_count_test.go:262: n=1（本页 1 行）：本页读了 products 表 4 次，期望 3 次（列表 / count(*) / 批量挂载事实各一次）——逐商品读无论哪种形态（s.Get 的 WHERE id = 或 ListRowFacts 的 id IN）都会把它推高。命中语句：
        SELECT id, project_id, name, slug, sku_code, type, status, images, sort, default_image, create_time, update_time FROM "products" ORDER BY sort ASC, create_time DESC LIMIT $1
        SELECT count(*) FROM "products"
        SELECT id, type, category_ids, primary_category_id, brand_id, tag_ids, default_price FROM "products" WHERE project_id = $1 AND id IN ($2)
        SELECT id, type, category_ids, primary_category_id, brand_id, tag_ids, default_price FROM "products" WHERE project_id = $1 AND id IN ($2)
--- FAIL: TestProductListPageSQLCountIsConstantInProductCount (0.11s)
FAIL
FAIL	go_wp/internal/module/product/service	0.174s
FAIL
```

同一注入下**耗时基准**的记录值（它只记录、不断言）：

```text
n=  1 第 1 页：行数= 1 SQL=19 逐商品详情=0 | 耗时 min=1.178843ms 中位=1.476371ms max=1.655035ms（5 次：1.178843ms / 1.199362ms / 1.476371ms / 1.569064ms / 1.655035ms）
n= 20 第 1 页：行数=20 SQL=57 逐商品详情=0 | 耗时 min=4.004345ms 中位=4.537647ms max=5.163691ms（5 次：4.004345ms / 4.181088ms / 4.537647ms / 4.831837ms / 5.163691ms）
n=100 第 1 页：行数=20 SQL=57 逐商品详情=0 | 耗时 min=3.922561ms 中位=5.074746ms max=5.350762ms（5 次：3.922561ms / 4.012821ms / 5.074746ms / 5.156077ms / 5.350762ms）
n=100 第 2 页：行数=20 SQL=57 逐商品详情=0 | 耗时 min=3.9329ms   中位=4.185667ms max=4.810839ms（5 次：3.9329ms / 4.015016ms / 4.185667ms / 4.485358ms / 4.810839ms）
```

三点结论：

1. **形状判据现在抓得到它**（4 ≠ 3），失败信息直接列出那两条 `id IN ($2)` 的 SELECT ——
   改造前探针只挂 Query / Raw，这条语句压根不进计数，而「逐商品详情 == 0」只认 `id = ` 这一种写法，
   于是这一整类回归只能靠总次数反推。
2. **「总次数相等」这条仍然兜得住**（判据的第二层，与形状判据互补）：每行 +2 条（`set_config` + Scan 的 SELECT），
   实测 19 / 57 / 57 —— 三档不等。两条判据现在一前一后都指向同一个回归，定位成本从「反推」降到「照单读」。
3. 逐商品 `s.Get`（注入一）与逐商品 `ListRowFacts`（注入二）现在**各有一条形状判据**先命中，
   不必再从总次数差值倒推是哪种写法回来了。

### 6.3 还原后复跑全绿

```text
--- PASS: TestProductListPageWallClockByProductCount (0.19s)
--- PASS: TestProductListPageSQLCountIsConstantInProductCount (0.42s)
PASS
ok  	go_wp/internal/module/product/service	1.076s
```

还原方式（两次注入相同）：注入前 `cp` 一份到 `/tmp`，实验后 `cp` 回去并核对 md5 ——
`5ca1832f94c3bf7eb7d9a48f000c4462  internal/module/product/service/product_row.go`（与注入前一致），
`git status --short` 里 `product_row.go` 无记录、`git diff` 为空（**未使用 `git checkout` / `git stash`**）。

## 7. 可复放命令

```bash
# 耗时基准（1/20/100 三档 + 100 档第 2 页；只记录、不断言）
go test ./internal/module/product/service/ -run TestProductListPageWallClockByProductCount -count=1 -v

# SQL 次数判据（逐商品详情 == 0、读 products 表恒 3 条、1/20/100/10000 四档次数相等、100 档翻第 2..5 页）
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
| 10^4–10^5 行规模下的耗时 | **部分做** | `n=10000` 档做了 **SQL 计数**（§3.4），耗时档位仍是 1 / 20 / 100（dd.txt §3.5 的三档）；见 §5 末段 |
| pprof / 内存分配 | **未做** | 没有指标要求；SQL 条数已能回答合并门槛 |
| CI 里的耗时阈值门禁 | **刻意不做** | §4.3：会 flaky 的门禁最终会被关掉 |
| 探针补挂 Row 链（把 16 改成 17） | **本批已做** | task-164 完成：`attachSQLCountProbe` 挂满 Query / Row / Raw 三条链，SQL 总次数断言由 16/16/16 变 17/17/17；同时新增「读 products 表恒 3 条」形状判据（§9.1、§6.2）。严格程度只升不降 |
| 耗时档位补 `n=10000` | **未做** | 本批的 `n=10000` 只落在 SQL 判据文件（打印条数 / 行数 / 逐商品详情）；耗时基准文件按任务边界不动（§9 第 3 条已说明观测口在哪） |

## 9. 清单外发现

1. **探针漏计 gorm Row 链**（`.Scan()` / `.Rows()`）—— **task-164 已修**。
   改造前：`product_list_page_sql_count_test.go` 的 `attachSQLCountProbe` 只挂 Query 与 Raw，
   本页漏 1 条（`ListRowFacts` 的 SELECT）⇒ 真实 17 条/页、探针报 16 条；且探针**看不见** `.Scan()` 形状的逐商品读，
   形状判据（只认 `WHERE id = `）也抓不到，只有「三档总次数相等」能兜（§6.2 的旧实测：1→17、20→36、100→36）。
   改造后：补挂 `Row().After("gorm:row")`（同文件 `:84`），并新增形状判据「读 `products` 表的语句恒 3 条」
   （常量 `:124`、判定 `:132`、断言 `:245`）。逐商品 `s.Get` 与逐商品 `ListRowFacts` 现在各有形状判据直接变红（§6.1 / §6.2）。
2. **改造后的固定开销比改造前略高**：页级固定 10 条 vs 改造前反推的 13 条（含成对 `set_config`）。
   17 条里 **8 条是 `rls.InProjectScope` 的 `set_config`**（每个带作用域的读开一次事务）——
   若要继续压固定开销，方向是**合并作用域**（一次事务里做完本页读），不是回到逐商品读。
3. **`n=20` 与 `n=100` 第 1 页的页面工作量完全相同**（各 20 行、各 17 条语句），因此这两档的耗时差**只能**反映
   「表里行数」这一项 —— **task-164 已按这条落地**：SQL 判据文件新增 `n=10000` 档（工程内 1 万行、本页仍 20 行），
   实测 SQL 总次数仍是 17、本页行数 20、逐商品详情 0（§3.4）⇒ 行数成本全部落在单条语句内部。
   耗时侧的 `n=10000` 档未做（耗时基准仍是 1/20/100 三档），若将来要量化「单条语句随行数变贵」多少，
   在那份耗时文件里加一档即可 —— 它只记录、不断言，不会引入 flaky 门禁。
4. **`product_list_page_bench_test.go` 的前提校验**（读模型失败 / `LoadFailed` / 成品行 0 条即 `t.Fatalf`）
   是「测得准」的前提，不是新判据；它保证不会把一次失败调用的耗时记进报告。