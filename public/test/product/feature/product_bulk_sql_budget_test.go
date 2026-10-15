// product_bulk_sql_budget_test.go — 六个批量用例的 SQL 次数对比（task-156，dd.txt §5.4）。
//
// 判据不是「循环搬进了 service」，而是「同规模数据下，一条批量用例的 SQL 条数明显少于逐条
// 调用单条用例」。所以每个用例都造两份等价数据：一份跑批量入口，一份跑单条循环（单条循环就是
// 改造前控制器的行为），各自计 SQL 条数并断言 批量 < 逐条。
//
// 计数方式：给 gorm 会话挂只观察、不改语义的 logger（先例
// public/test/product/feature/product_tag_page_query_budget_test.go）。夹具数据用批量 SQL 造，
// 避免「造 N 份数据」本身落在计数窗口里。
package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

// bulkBudgetN 每个用例的操作项数：20 足以摊平「连接预热、第一条语句」的一次性开销，
// 又远低于 bulkids.MaxIDs（200），不触到上限分支。
const bulkBudgetN = 20

// pricingBudgetRule 改价用例共用的规则：成本 × 1.5（成本 50 的商品 → 售价 75）。
// 两个测试文件都用它，避免同一份规则参数抄两遍。
func pricingBudgetRule() productdto.PricingRuleReq {
	return productdto.PricingRuleReq{
		RuleType:   productenums.PricingRuleCostMultiple,
		RuleParams: json.RawMessage("{\"multiplier\": 1.5}"),
	}
}

// bulkQueryCounter 记录经过 gorm 的每一条 SQL（含事务的 BEGIN / COMMIT）。
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

// bulkFixture 计数夹具：真实 PG + 生产迁移 + 真实 service，只把 model 换成挂了计数 logger 的会话。
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
	svc := productservice.NewService(productmodel.NewModel(counted), projects)
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

// seedProductRows 批量造 n 个商品（tagIDs 为 nil 时 tag_ids 为空数组）。
func seedProductRows(t *testing.T, db *gorm.DB, projectID string, tagIDs []string) []string {
	t.Helper()
	ids := make([]string, 0, len(tagIDs))
	values := make([]string, 0, len(tagIDs))
	args := make([]any, 0, len(tagIDs)*5)
	// slug 在工程内唯一（uq_products_project_slug）：同一工程可能被 seed 多次，加一段随机
	// 前缀，否则第二次插入撞唯一键（实测 23505）。
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

// seedVariantRows 给每个商品各造一个变体（定价用例的成本与售价固定，便于断言「确实改了价」）。
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

// seedTaxonomyRows 批量造 n 个分类 / 品牌 / 标签（三张表的可插列一致：id / project_id / name / slug）。
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

// seedAttributeRows 批量造 n 个属性组（属性表用 key / name 两列，与分类品牌标签不同）。
func seedAttributeRows(t *testing.T, db *gorm.DB, projectID string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	values := make([]string, 0, n)
	args := make([]any, 0, n*4)
	// key 在工程内唯一（uq_product_attributes_project_key）：理由同 seedProductRows 的 slug。
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

// TestBulkOperationSQLBudget 六个批量用例逐个对比「批量入口」与「单条循环」的 SQL 条数。
//
// 逐条循环就是改造前控制器的行为：控制器拿 ID 清单自己 for 循环调单条 service。
// 断言方向只有一个 —— 批量严格少于逐条；数字本身用 t.Logf 打出来进报告。
func TestBulkOperationSQLBudget(t *testing.T) {
	f := newBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const n = bulkBudgetN

	type bulkCase struct {
		name string
		// seed 造 n 份可操作数据，返回要操作的 id 清单（批量工程与逐条工程各调一次）。
		seed func(t *testing.T, projectID string) []string
		// bulk 调批量入口，返回完成项数（用于断言「真的做了事」，防止空跑得出漂亮数字）。
		bulk func(t *testing.T, ids []string, projectID string) int
		// loop 调单条入口一次。
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
				return res.Succeeded
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
				return res.Succeeded
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
				return res.Succeeded
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
				return res.Succeeded
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
				// 每个标签挂一个本工程商品：本工程内引用不阻止删除，但会触发解绑 UPDATE —— 逐条
				// 与批量两边都要付这个代价，才谈得上同一起跑线。
				seedProductRows(t, f.db, projectID, ids)
				return ids
			},
			bulk: func(t *testing.T, ids []string, projectID string) int {
				res, err := f.svc.BulkDeleteProductTags(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectID})
				if err != nil {
					t.Fatalf("批量删除标签失败：%v", err)
				}
				return res.Succeeded
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
				return res.Succeeded
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
