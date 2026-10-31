package productservice_test

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

type listPageProjectStub struct {
	projectcontract.ProjectService
	rows []projectdto.ProjectResp
}

func (s listPageProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, nil
}

type sqlCountProbe struct {
	mu    sync.Mutex
	stmts []string
}

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

const listPageProductsTableReads = 2

func productsReadStmt(sql string) bool {
	return strings.Contains(sql, "FROM "+`"products"`)
}

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

func (p *sqlCountProbe) dump() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.stmts, "\n")
}

func seedListProduct(t *testing.T, db *gorm.DB, projectID string, seq int) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, 'published', ?, now(), now())`,
		id, projectID, fmt.Sprintf("商品 %03d", seq), fmt.Sprintf("p-%03d", seq),
		fmt.Sprintf("SKU-%03d", seq), seq).Error; err != nil {
		t.Fatalf("插入商品失败：%v", err)
	}
	return id
}

func seedListProductsBulk(t *testing.T, db *gorm.DB, projectID string, from, to int) {
	t.Helper()
	const batchRows = 500
	for start := from; start <= to; start += batchRows {
		end := start + batchRows - 1
		if end > to {
			end = to
		}
		var sb strings.Builder
		sb.WriteString(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time) VALUES `)
		args := make([]any, 0, (end-start+1)*6)
		for seq := start; seq <= end; seq++ {
			if seq > start {
				sb.WriteString(", ")
			}
			sb.WriteString("(?, ?, ?, ?, ?, 'published', ?, now(), now())")
			args = append(args, uuid.NewString(), projectID,
				fmt.Sprintf("商品 %03d", seq), fmt.Sprintf("p-%03d", seq),
				fmt.Sprintf("SKU-%03d", seq), seq)
		}
		if err := db.Exec(sb.String(), args...).Error; err != nil {
			t.Fatalf("批量插入商品 %d..%d 失败：%v", start, end, err)
		}
	}
}

func TestProductListPageSQLCountIsConstantInProductCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "列表 SQL 判据")

	probe := attachSQLCountProbe(t, db)
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "列表 SQL 判据"}},
	})
	ctx := context.Background()

	checkPage := func(label string, page int, wantRows int, wantTotal int64) int {
		t.Helper()
		probe.reset()
		view, err := s.QueryProducts(ctx, &productdto.ListReq{ProjectID: projectID, Page: page})
		if err != nil {
			t.Fatalf("%s：列表页读模型失败：%v", label, err)
		}
		if len(view.Items) != wantRows {
			t.Fatalf("%s：成品行 %d 条，期望 %d", label, len(view.Items), wantRows)
		}
		if view.Total != wantTotal {
			t.Fatalf("%s：总数 %d，期望 %d", label, view.Total, wantTotal)
		}
		if lookups := probe.perProductDetailLookups(); lookups != 0 {
			t.Fatalf("%s：行组装期间发了 %d 次逐商品详情查询（判据是 0 次）。命中语句：\n%s",
				label, lookups, probe.dump())
		}
		if reads := probe.productTableReads(); reads != listPageProductsTableReads {
			t.Fatalf("%s：本页读了 products 表 %d 次，期望 %d 次（列表 / count(*) 各一次）——"+
				"额外详情或同表补偿读取都会超出预算。命中语句：\n%s",
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
		wantRows := min(n, 20)
		label := fmt.Sprintf("n=%d（本页 %d 行）", n, wantRows)
		totals[n] = checkPage(label, 1, wantRows, int64(n))
		t.Logf("%s：SQL 总次数=%d，逐商品详情查询=0，读 products 表=%d",
			label, totals[n], listPageProductsTableReads)
	}
	if totals[1] != totals[20] || totals[20] != totals[100] {
		t.Fatalf("SQL 次数随商品数变化：1→%d、20→%d、100→%d（页内取数必须是常数条）",
			totals[1], totals[20], totals[100])
	}

	for page := 2; page <= 5; page++ {
		label := fmt.Sprintf("100 商品第 %d 页", page)
		got := checkPage(label, page, 20, 100)
		if got != totals[1] {
			t.Fatalf("第 %d 页 SQL 次数 %d，与单商品页 %d 不等（页内取数必须与商品数、页码无关）",
				page, got, totals[1])
		}
		t.Logf("100 商品第 %d 页：SQL 总次数=%d（与 1 商品页相同）", page, got)
	}

	seedListProductsBulk(t, db, projectID, seeded+1, 10000)
	seeded = 10000
	label := fmt.Sprintf("n=10000（工程内 %d 行、本页 %d 行）", seeded, 20)
	got := checkPage(label, 1, 20, int64(seeded))
	if got != totals[1] {
		t.Fatalf("n=10000 档 SQL 次数 %d，与单商品页 %d 不等（页内取数必须与工程内商品数无关）", got, totals[1])
	}
	t.Logf("n=10000：SQL 总次数=%d（与 1 商品页逐条相同）；残余的线性项只在单条语句内部，"+
		"不在语句条数上（见 AGENTS.md §5 末段）", got)
}
