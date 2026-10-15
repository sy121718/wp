package productservice_test

// product_list_page_sql_count_test.go — 商品列表页的 **SQL 次数判据**（P0-1 的硬产出）。
//
// 判据三条，都必须会变红：
//   1. 行组装期间**逐商品详情查询 0 次** —— 把 s.Get 逐商品调用加回列表页或
//      productRowsFromList，本用例立即失败（失败信息会打出命中的语句）；
//   2. 读 `products` 表的语句条数 == 3（列表 / count(*) / 批量挂载事实各一次）——
//      s.Get（WHERE id = ）与逐商品 ListRowFacts（id IN）**两种形态都数得进来**；
//   3. SQL 次数**不随商品数（1 / 20 / 100 / 10000 档）线性增长** —— 页内取数必须是常数条。
//
// 为什么用 gorm 回调计数而不是 pg_stat_statements：判据要区分「语句的形状」——
// 「逐商品详情查询」的形状是 FROM "products" 且 WHERE id = （s.Get → First），
// 而列表查询是 WHERE project_id = …；库级累计量既分不出这两者，也混进了进程里别处的语句。
//
// 探针必须挂满**三条链**（Query / Row / Raw）：漏掉 Row 会少计 `.Scan()` / `.Rows()` 整类语句，
// 本页因此漏 1 条（ListRowFacts 的 SELECT：真实 17 条、漏计报 16 条）—— 见 attachSQLCountProbe。
//
// 为什么这些用例在**外部测试包**（productservice_test）：它们要真实的迁移库
// （support.NewMigratedPGTestDB），而 support 传递依赖 internal/routers →
// product/inbound/http → product/service —— 内部测试包（package productservice）导入它会
// 构成「测试期导入环」（实测 go vet 报 import cycle not allowed in test）。
// 所以：带库的判据走外部测试包 + 导出读模型；未导出纯函数的判据留在内部测试包
// （见 product_row_facts_unit_test.go）。
//
// 表结构来自生产迁移，不手抄 CREATE TABLE。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

// listPageProjectStub 工程契约桩（这两个用例只走工程清单一条读方法）。
type listPageProjectStub struct {
	projectcontract.ProjectService
	rows []projectdto.ProjectResp
}

func (s listPageProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, nil
}

// sqlCountProbe SQL 次数探针：记录被执行的语句文本（gorm 回调）。
type sqlCountProbe struct {
	mu    sync.Mutex
	stmts []string
}

// attachSQLCountProbe 在 db 上挂**三条**计数回调（Query / Row / Raw）。
//
// 三条链各管一类语句，少一条就有一整类隐形：
//   - Query（gorm:query）：Find / First / Count 这类链式读；
//   - Row（gorm:row）：**`.Scan()` / `.Rows()` 走的是另一条链** —— 漏掉它，本页少计 1 条
//     （ListRowFacts 的 SELECT：真实 17 条、漏计报 16 条），且整类 Scan 形态的读根本不进计数；
//   - Raw（gorm:raw）：rls.InProjectScope 每个带作用域的读都会发一条
//     `set_config('app.project_id', …)` —— 它们是真实往返，不是测试噪音。
//
// BEGIN / COMMIT 由驱动层（database/sql）发出、不走 gorm 回调，本来就不计。
func attachSQLCountProbe(t *testing.T, db *gorm.DB) *sqlCountProbe {
	t.Helper()
	p := &sqlCountProbe{}
	record := func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		p.mu.Lock()
		defer p.mu.Unlock()
		p.stmts = append(p.stmts, sql)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:sqlcount_query", record); err != nil {
		t.Fatalf("注册 Query 计数回调失败：%v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:sqlcount_row", record); err != nil {
		t.Fatalf("注册 Row 计数回调失败：%v", err)
	}
	if err := db.Callback().Raw().After("gorm:raw").Register("test:sqlcount_raw", record); err != nil {
		t.Fatalf("注册 Raw 计数回调失败：%v", err)
	}
	return p
}

func (p *sqlCountProbe) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stmts = nil
}

func (p *sqlCountProbe) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.stmts)
}

// perProductDetailLookups 逐商品详情查询的次数（s.Get → model.Get 的形状）。
func (p *sqlCountProbe) perProductDetailLookups() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, sql := range p.stmts {
		if strings.Contains(sql, "FROM "+`"products"`) && strings.Contains(sql, "WHERE id = ") {
			n++
		}
	}
	return n
}

// listPageProductsTableReads 本页**读 products 表**的语句条数：列表查询、count(*) 与批量挂载事实各一条。
//
// 它与 perProductDetailLookups 是两层判据：那一层认「WHERE id = 」这一种形状（s.Get），
// 这一层认**表名** —— 逐商品 ListRowFacts 的形状是 project_id = … AND id IN (…)（见
// model.product_model.go 的 ListRowFacts），没有 `id = ` 字样，只有这一层数得到它。
// 补挂 Row 链之前，它连计数都不进（探针看不见），形状判据对它是盲的。
const listPageProductsTableReads = 3

// productsReadStmt 判断一条语句是不是「读 products 表」。
func productsReadStmt(sql string) bool {
	return strings.Contains(sql, "FROM "+`"products"`)
}

// productTableReads 读 products 表的语句条数。
func (p *sqlCountProbe) productTableReads() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, sql := range p.stmts {
		if productsReadStmt(sql) {
			n++
		}
	}
	return n
}

// dumpProductTableReads 只列读 products 表的语句（失败信息给证据，而不是把整页语句都倒出来）。
func (p *sqlCountProbe) dumpProductTableReads() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	hits := make([]string, 0, 4)
	for _, sql := range p.stmts {
		if productsReadStmt(sql) {
			hits = append(hits, sql)
		}
	}
	return strings.Join(hits, "\n")
}

// dump 打全部语句（失败信息里给出证据，而不是只说「多了几次」）。
func (p *sqlCountProbe) dump() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.stmts, "\n")
}

// seedListProduct 插一行商品（只补 NOT NULL 且无默认值的列，其余走表默认），返回 id。
func seedListProduct(t *testing.T, db *gorm.DB, projectID string, seq int) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, type, sort, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, 'published', 'variant', ?, now(), now())`,
		id, projectID, fmt.Sprintf("商品 %03d", seq), fmt.Sprintf("p-%03d", seq),
		fmt.Sprintf("SKU-%03d", seq), seq).Error; err != nil {
		t.Fatalf("插入商品失败：%v", err)
	}
	return id
}

// seedListProductsBulk 批量插入 [from, to] 区间的商品，行内容与 seedListProduct **逐字相同**
// （同一份 NOT NULL 列清单、同一套 name / slug / sku 格式），只是每 500 行拼成一条 INSERT。
// 为什么要它：n=10000 档若逐行 Exec，用例的时间就全花在 10000 次往返上，而不是被测的取数上。
func seedListProductsBulk(t *testing.T, db *gorm.DB, projectID string, from, to int) {
	t.Helper()
	const batchRows = 500
	for start := from; start <= to; start += batchRows {
		end := start + batchRows - 1
		if end > to {
			end = to
		}
		var sb strings.Builder
		sb.WriteString(`INSERT INTO products (id, project_id, name, slug, sku_code, status, type, sort, create_time, update_time) VALUES `)
		args := make([]any, 0, (end-start+1)*6)
		for seq := start; seq <= end; seq++ {
			if seq > start {
				sb.WriteString(", ")
			}
			sb.WriteString("(?, ?, ?, ?, ?, 'published', 'variant', ?, now(), now())")
			args = append(args, uuid.NewString(), projectID,
				fmt.Sprintf("商品 %03d", seq), fmt.Sprintf("p-%03d", seq),
				fmt.Sprintf("SKU-%03d", seq), seq)
		}
		if err := db.Exec(sb.String(), args...).Error; err != nil {
			t.Fatalf("批量插入商品 %d..%d 失败：%v", start, end, err)
		}
	}
}

// TestProductListPageSQLCountIsConstantInProductCount 每页 1 / 20 / 100 商品三档 + 10000 档：
// 逐商品详情查询 0 次、读 products 表恒 3 条、SQL 总次数各档相等；100 档再翻满 5 页，每页同样成立。
//
// 为什么另加 10000 档：n=20 与 n=100 第 1 页的**页面工作量完全相同**（各 20 行、同样 17 条语句），
// 所以那两档只测得动「条数」；工程内行数远大于本页行数时，残余的线性项才有观测窗口 ——
// 它不在 SQL **条数**层面（10000 档同样相等），而在单条语句自身的工作量（报告 §5 末段）。
func TestProductListPageSQLCountIsConstantInProductCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "列表 SQL 判据")

	probe := attachSQLCountProbe(t, db)
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "列表 SQL 判据"}},
	})
	ctx := context.Background()

	// checkPage 跑一次列表页并核对四条判据，返回本页 SQL 总次数。
	// 四条按「形状 → 总量」排序：形状判据先给出**可定位的语句清单**，总量判据兜住形状认不出的回归。
	checkPage := func(label string, page int, wantRows int, wantTotal int64) int {
		t.Helper()
		probe.reset()
		view, err := s.ProductListPage(ctx, &productdto.ProductListPageReq{ProjectID: projectID, Page: page})
		if err != nil {
			t.Fatalf("%s：列表页读模型失败：%v", label, err)
		}
		if view.LoadFailed {
			t.Fatalf("%s：列表页被判失败", label)
		}
		if len(view.Products) != wantRows {
			t.Fatalf("%s：成品行 %d 条，期望 %d", label, len(view.Products), wantRows)
		}
		if view.Total != wantTotal {
			t.Fatalf("%s：总数 %d，期望 %d", label, view.Total, wantTotal)
		}
		if lookups := probe.perProductDetailLookups(); lookups != 0 {
			t.Fatalf("%s：行组装期间发了 %d 次逐商品详情查询（判据是 0 次）。命中语句：\n%s",
				label, lookups, probe.dump())
		}
		if reads := probe.productTableReads(); reads != listPageProductsTableReads {
			t.Fatalf("%s：本页读了 products 表 %d 次，期望 %d 次（列表 / count(*) / 批量挂载事实各一次）——"+
				"逐商品读无论哪种形态（s.Get 的 WHERE id = 或 ListRowFacts 的 id IN）都会把它推高。命中语句：\n%s",
				label, reads, listPageProductsTableReads, probe.dumpProductTableReads())
		}
		return probe.total()
	}

	totals := map[int]int{}
	seeded := 0
	for _, n := range []int{1, 20, 100} {
		if n > seeded {
			seedListProductsBulk(t, db, projectID, seeded+1, n)
			seeded = n
		}
		wantRows := min(n, productservice.ProductListPageSize)
		label := fmt.Sprintf("n=%d（本页 %d 行）", n, wantRows)
		totals[n] = checkPage(label, 1, wantRows, int64(n))
		t.Logf("%s：SQL 总次数=%d，逐商品详情查询=0，读 products 表=%d",
			label, totals[n], listPageProductsTableReads)
	}
	if totals[1] != totals[20] || totals[20] != totals[100] {
		t.Fatalf("SQL 次数随商品数变化：1→%d、20→%d、100→%d（页内取数必须是常数条）",
			totals[1], totals[20], totals[100])
	}

	// 100 个商品这一档再翻满 5 页：分页偏移不该带来任何额外次数
	// （N+1 的另一种形态是「每行补一次」，翻页时同样会暴露）。
	for page := 2; page <= 5; page++ {
		label := fmt.Sprintf("100 商品第 %d 页", page)
		got := checkPage(label, page, productservice.ProductListPageSize, 100)
		if got != totals[1] {
			t.Fatalf("第 %d 页 SQL 次数 %d，与单商品页 %d 不等（页内取数必须与商品数、页码无关）",
				page, got, totals[1])
		}
		t.Logf("100 商品第 %d 页：SQL 总次数=%d（与 1 商品页相同）", page, got)
	}

	// 第四档：工程内 1 万行、本页仍是 20 行。用途是**单独暴露「表里行数」这一项** ——
	// 页面工作量与 20 行页逐条相同，两档之间的任何差别都只能来自单条语句自身的工作量
	//（count(*) 要数的行、列表 ORDER BY … LIMIT/OFFSET 要跳过的索引项）。
	seedListProductsBulk(t, db, projectID, seeded+1, 10000)
	seeded = 10000
	label := fmt.Sprintf("n=10000（工程内 %d 行、本页 %d 行）", seeded, productservice.ProductListPageSize)
	got := checkPage(label, 1, productservice.ProductListPageSize, int64(seeded))
	if got != totals[1] {
		t.Fatalf("n=10000 档 SQL 次数 %d，与单商品页 %d 不等（页内取数必须与工程内商品数无关）", got, totals[1])
	}
	t.Logf("n=10000：SQL 总次数=%d（与 1 商品页逐条相同）；残余的线性项只在单条语句内部，"+
		"不在语句条数上（见 docs/audit/product-list-n1-report.md §5 末段）", got)
}
