package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

const bulkBudgetN = 20

func pricingBudgetRule() productdto.PricingRuleReq {
	return productdto.PricingRuleReq{
		RuleType:   productenums.PricingRuleCostMultiple,
		RuleParams: json.RawMessage("{\"multiplier\": 1.5}"),
	}
}

type bulkQueryCounter struct {
	mu    sync.Mutex
	stmts []string
}

func (l *bulkQueryCounter) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *bulkQueryCounter) Info(context.Context, string, ...interface{})     {}
func (l *bulkQueryCounter) Warn(context.Context, string, ...interface{})     {}
func (l *bulkQueryCounter) Error(context.Context, string, ...interface{})    {}

func (l *bulkQueryCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	l.mu.Lock()
	l.stmts = append(l.stmts, sql)
	l.mu.Unlock()
}

func (l *bulkQueryCounter) reset() {
	l.mu.Lock()
	l.stmts = nil
	l.mu.Unlock()
}

func (l *bulkQueryCounter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.stmts)
}

func (l *bulkQueryCounter) dump() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.stmts, "\n  ")
}

type bulkFixture struct {
	db       *gorm.DB
	svc      *productservice.Service
	projects *projectservice.Service
	counter  *bulkQueryCounter
}

func newBulkFixture(t *testing.T) *bulkFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	counter := &bulkQueryCounter{}
	counted := db.Session(&gorm.Session{Logger: counter})
	projects := projectservice.NewService(projectmodel.NewProjectModel(counted))
	svc := productservice.NewService(productmodel.NewModel(counted), projects, masterdataservice.NewService(masterdatamodel.NewModel(counted), projects))
	return &bulkFixture{db: db, svc: svc, projects: projects, counter: counter}
}

func (f *bulkFixture) newProject(t *testing.T, name string) string {
	t.Helper()
	p, err := f.projects.Create(context.Background(), &projectdto.CreateReq{Name: name})
	if err != nil {
		t.Fatalf("建测试工程失败：%v", err)
	}
	return p.ID
}

func seedProductRows(t *testing.T, db *gorm.DB, projectID string, tagIDs []string) []string {
	t.Helper()
	ids := make([]string, 0, len(tagIDs))
	values := make([]string, 0, len(tagIDs))
	args := make([]any, 0, len(tagIDs)*5)

	batch := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	for i, tagID := range tagIDs {
		id := uuid.NewString()
		ids = append(ids, id)
		tags := "[]"
		if tagID != "" {
			tags = fmt.Sprintf("[%q]", tagID)
		}
		values = append(values, "(?, ?, ?, ?, ?::jsonb)")
		args = append(args, id, projectID, fmt.Sprintf("商品 %s-%04d", batch, i), fmt.Sprintf("sku-%s-%04d", batch, i), tags)
	}
	if err := db.Exec("INSERT INTO products (id, project_id, name, slug, tag_ids) VALUES "+strings.Join(values, ", "), args...).Error; err != nil {
		t.Fatalf("批量造商品失败：%v", err)
	}
	return ids
}

func seedVariantRows(t *testing.T, db *gorm.DB, productIDs []string, price, cost float64) {
	t.Helper()
	values := make([]string, 0, len(productIDs))
	args := make([]any, 0, len(productIDs)*4)
	for i, pid := range productIDs {
		values = append(values, "(?, ?, ?, ?, ?)")
		args = append(args, uuid.NewString(), pid, fmt.Sprintf("v-%04d", i), price, cost)
	}
	if err := db.Exec("INSERT INTO product_variants (id, product_id, sku_code, price, cost_price) VALUES "+strings.Join(values, ", "), args...).Error; err != nil {
		t.Fatalf("批量造变体失败：%v", err)
	}
}

func seedTaxonomyRows(t *testing.T, db *gorm.DB, table, projectID, prefix string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	values := make([]string, 0, n)
	args := make([]any, 0, n*4)
	for i := 0; i < n; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		values = append(values, "(?, ?, ?, ?)")
		args = append(args, id, projectID, fmt.Sprintf("%s %04d", prefix, i), fmt.Sprintf("%s-%04d", prefix, i))
	}
	if err := db.Exec("INSERT INTO "+table+" (id, project_id, name, slug) VALUES "+strings.Join(values, ", "), args...).Error; err != nil {
		t.Fatalf("批量造 %s 失败：%v", table, err)
	}
	return ids
}

func seedAttributeRows(t *testing.T, db *gorm.DB, projectID string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	values := make([]string, 0, n)
	args := make([]any, 0, n*4)

	batch := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	for i := 0; i < n; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		values = append(values, "(?, ?, ?, ?)")
		args = append(args, id, projectID, fmt.Sprintf("attr-%s-%04d", batch, i), fmt.Sprintf("属性 %s-%04d", batch, i))
	}
	if err := db.Exec("INSERT INTO product_attributes (id, project_id, key, name) VALUES "+strings.Join(values, ", "), args...).Error; err != nil {
		t.Fatalf("批量造属性失败：%v", err)
	}
	return ids
}

func TestBulkOperationSQLBudget(t *testing.T) {
	f := newBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const n = bulkBudgetN

	type bulkCase struct {
		name string

		seed func(t *testing.T, projectID string) []string

		bulk func(t *testing.T, ids []string, projectID string) int

		loop func(t *testing.T, id, projectID string)
	}

	simpleSeed := func(table, prefix string, withAttr bool) func(*testing.T, string) []string {
		return func(t *testing.T, projectID string) []string {
			if withAttr {
				return seedAttributeRows(t, f.db, projectID, n)
			}
			return seedTaxonomyRows(t, f.db, table, projectID, prefix, n)
		}
	}

	cases := []bulkCase{
		{
			name: "商品批量删除",
			seed: func(t *testing.T, projectID string) []string {
				tags := make([]string, n)
				ids := seedProductRows(t, f.db, projectID, tags)
				seedVariantRows(t, f.db, ids, 100, 50)
				return ids
			},
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectID})
				if err != nil {
					t.Fatalf("批量删除商品失败：%v", err)
				}
				return res.Done
			},
			loop: func(t *testing.T, id, projectID string) {
				if err := f.svc.Delete(ctx, &productdto.DeleteReq{ID: id, ProjectID: projectID}); err != nil {
					t.Fatalf("单条删除商品失败：%v", err)
				}
			},
		},
		{
			name: "分类批量删除",
			seed: simpleSeed("product_categories", "分类", false),
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkDeleteProductCategories(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectID})
				if err != nil {
					t.Fatalf("批量删除分类失败：%v", err)
				}
				return res.Done
			},
			loop: func(t *testing.T, id, projectID string) {
				if err := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ID: id, ProjectID: projectID}); err != nil {
					t.Fatalf("单条删除分类失败：%v", err)
				}
			},
		},
		{
			name: "品牌批量删除",
			seed: simpleSeed("product_brands", "品牌", false),
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkDeleteProductBrands(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectID})
				if err != nil {
					t.Fatalf("批量删除品牌失败：%v", err)
				}
				return res.Done
			},
			loop: func(t *testing.T, id, projectID string) {
				if err := f.svc.DeleteBrand(ctx, &productdto.DeleteBrandReq{ID: id, ProjectID: projectID}); err != nil {
					t.Fatalf("单条删除品牌失败：%v", err)
				}
			},
		},
		{
			name: "属性批量删除",
			seed: simpleSeed("", "", true),
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkDeleteProductAttributes(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectID})
				if err != nil {
					t.Fatalf("批量删除属性失败：%v", err)
				}
				return res.Done
			},
			loop: func(t *testing.T, id, projectID string) {
				if err := f.svc.DeleteAttribute(ctx, &productdto.DeleteAttributeReq{ID: id, ProjectID: projectID}); err != nil {
					t.Fatalf("单条删除属性失败：%v", err)
				}
			},
		},
		{
			name: "标签批量删除",
			seed: func(t *testing.T, projectID string) []string {
				ids := seedTaxonomyRows(t, f.db, "product_tags", projectID, "标签", n)

				seedProductRows(t, f.db, projectID, ids)
				return ids
			},
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkDeleteProductTags(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectID})
				if err != nil {
					t.Fatalf("批量删除标签失败：%v", err)
				}
				return res.Done
			},
			loop: func(t *testing.T, id, projectID string) {
				if err := f.svc.DeleteTag(ctx, &productdto.DeleteTagReq{ID: id, ProjectID: projectID}); err != nil {
					t.Fatalf("单条删除标签失败：%v", err)
				}
			},
		},
		{
			name: "批量定价",
			seed: func(t *testing.T, projectID string) []string {
				tags := make([]string, n)
				ids := seedProductRows(t, f.db, projectID, tags)
				seedVariantRows(t, f.db, ids, 100, 50)
				return ids
			},
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkApplyPricing(ctx, &productdto.BulkPricingReq{
					IDs: ids, ProjectID: projectID, Note: "批量回归",
					Rule: pricingBudgetRule(),
				})
				if err != nil {
					t.Fatalf("批量改价失败：%v", err)
				}
				if res.ChangedVariants != n {
					t.Fatalf("批量改价应改动 %d 个变体，实际 %d（%+v）", n, res.ChangedVariants, res)
				}
				return res.Done
			},
			loop: func(t *testing.T, id, projectID string) {
				rule := pricingBudgetRule()
				rule.ProjectID = projectID
				rule.Scope = productenums.PricingScopeProduct
				rule.TargetID = id
				if _, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
					PricingRuleReq: rule,
					Note:           "逐条回归",
				}); err != nil {
					t.Fatalf("单条改价失败：%v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bulkProject := f.newProject(t, tc.name+"（批量）")
			loopProject := f.newProject(t, tc.name+"（逐条）")
			bulkIDs := tc.seed(t, bulkProject)
			loopIDs := tc.seed(t, loopProject)
			if len(bulkIDs) != n || len(loopIDs) != n {
				t.Fatalf("夹具规模不对：bulk=%d loop=%d want %d", len(bulkIDs), len(loopIDs), n)
			}

			f.counter.reset()
			done := tc.bulk(t, bulkIDs, bulkProject)
			bulkSQL := f.counter.count()
			bulkStmts := f.counter.dump()
			if done != n {
				t.Fatalf("批量入口应完成 %d 项，实际 %d（夹具或实现有一边不对）", n, done)
			}

			f.counter.reset()
			for _, id := range loopIDs {
				tc.loop(t, id, loopProject)
			}
			loopSQL := f.counter.count()

			t.Logf("%s：批量 %d 条 SQL / 逐条 %d 条 SQL（各 %d 项，省 %d 条）",
				tc.name, bulkSQL, loopSQL, n, loopSQL-bulkSQL)
			t.Logf("%s 批量语句：\n  %s", tc.name, bulkStmts)
			if bulkSQL >= loopSQL {
				t.Fatalf("批量入口的 SQL 条数（%d）不少于逐条循环（%d）—— 批量没有生效", bulkSQL, loopSQL)
			}
		})
	}
}
