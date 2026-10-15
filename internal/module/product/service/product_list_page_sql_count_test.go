package productservice_test

// product_list_page_sql_count_test.go — 商品列表页的 **SQL 次数判据**（P0-1 的硬产出）。
//
// 判据两条，都必须会变红：
//   1. 行组装期间**逐商品详情查询 0 次** —— 把 s.Get 逐商品调用加回列表页或
//      productRowsFromList，本用例立即失败（失败信息会打出命中的语句）；
//   2. SQL 次数**不随商品数（1 / 20 / 100 档）线性增长** —— 页内取数必须是常数条。
//
// 为什么用 gorm 回调计数而不是 pg_stat_statements：判据要区分「语句的形状」——
// 「逐商品详情查询」的形状是 FROM "products" 且 WHERE id = （s.Get → First），
// 而列表查询是 WHERE project_id = …；库级累计量既分不出这两者，也混进了进程里别处的语句。
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

// attachSQLCountProbe 在 db 上挂两条计数回调（Query / Raw）。
//
// Raw 不能省：事务边界（BEGIN / SET LOCAL / COMMIT）走 Raw 链，
// 而 rls.InProjectScope 每条带作用域的读都会开一个事务 —— 这些也是真实的 SQL 次数。
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

// TestProductListPageSQLCountIsConstantInProductCount 每页 1 / 20 / 100 商品三档：
// 逐商品详情查询 0 次，SQL 总次数三档相等；100 档再翻满 5 页，每页同样相等。
func TestProductListPageSQLCountIsConstantInProductCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "列表 SQL 判据")

	probe := attachSQLCountProbe(t, db)
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "列表 SQL 判据"}},
	})
	ctx := context.Background()

	totals := map[int]int{}
	seeded := 0
	for _, n := range []int{1, 20, 100} {
		for seeded < n {
			seeded++
			seedListProduct(t, db, projectID, seeded)
		}
		probe.reset()
		view, err := s.ProductListPage(ctx, &productdto.ProductListPageReq{ProjectID: projectID})
		if err != nil {
			t.Fatalf("n=%d：列表页读模型失败：%v", n, err)
		}
		if view.LoadFailed {
			t.Fatalf("n=%d：列表页被判失败", n)
		}
		wantRows := n
		if wantRows > productservice.ProductListPageSize {
			wantRows = productservice.ProductListPageSize
		}
		if len(view.Products) != wantRows {
			t.Fatalf("n=%d：成品行 %d 条，期望 %d", n, len(view.Products), wantRows)
		}
		if view.Total != int64(n) {
			t.Fatalf("n=%d：总数 %d，期望 %d", n, view.Total, n)
		}
		if lookups := probe.perProductDetailLookups(); lookups != 0 {
			t.Fatalf("n=%d：行组装期间发了 %d 次逐商品详情查询（判据是 0 次）。命中语句：\n%s",
				n, lookups, probe.dump())
		}
		totals[n] = probe.total()
		t.Logf("n=%d（本页 %d 行）：SQL 总次数=%d，逐商品详情查询=0", n, len(view.Products), totals[n])
	}
	if totals[1] != totals[20] || totals[20] != totals[100] {
		t.Fatalf("SQL 次数随商品数变化：1→%d、20→%d、100→%d（页内取数必须是常数条）",
			totals[1], totals[20], totals[100])
	}

	// 100 个商品这一档再翻满 5 页：分页偏移不该带来任何额外次数
	// （N+1 的另一种形态是「每行补一次」，翻页时同样会暴露）。
	for page := 2; page <= 5; page++ {
		probe.reset()
		view, err := s.ProductListPage(ctx, &productdto.ProductListPageReq{ProjectID: projectID, Page: page})
		if err != nil {
			t.Fatalf("第 %d 页读模型失败：%v", page, err)
		}
		if len(view.Products) != productservice.ProductListPageSize {
			t.Fatalf("第 %d 页成品行 %d 条，期望 %d", page, len(view.Products), productservice.ProductListPageSize)
		}
		if lookups := probe.perProductDetailLookups(); lookups != 0 {
			t.Fatalf("第 %d 页行组装期间发了 %d 次逐商品详情查询。命中语句：\n%s", page, lookups, probe.dump())
		}
		if got := probe.total(); got != totals[1] {
			t.Fatalf("第 %d 页 SQL 次数 %d，与单商品页 %d 不等（页内取数必须与商品数、页码无关）",
				page, got, totals[1])
		}
		t.Logf("100 商品第 %d 页：SQL 总次数=%d（与 1 商品页相同）", page, probe.total())
	}
}
