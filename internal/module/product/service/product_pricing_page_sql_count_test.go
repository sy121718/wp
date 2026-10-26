package productservice_test

// product_pricing_page_sql_count_test.go — 定价留痕台账的 **SQL 次数判据**
// （dd.txt 第八批 8.1「定价历史 N+1」在台账这一屏的等价物）。
//
// 判据三条，都必须会变红：
//  1. 台账装配期间**逐批次明细查询 0 次** —— 把 s.GetPriceAdjustment 的逐批调用加回
//     pricingHistory，本用例立即失败（失败信息打出命中的语句）；
//  2. SQL 总次数**与批次数无关**（1 批与 10 批两档相等）—— 批量化之后取数条数由
//     「这一屏要哪些数据」决定，不由行数决定；
//  3. 两档都真的把明细逐条展开了（每批 2 条）—— 否则「一次都不查」也能满足前两条。
//
// 为什么用 gorm 回调计数（与 P0-1 的 product_list_page_sql_count_test.go 同一手法与理由）：
// 判据要区分语句的**形状** ——「逐批次明细查询」是 product_price_adjustment_items 上带
// 单值 adjustment_id = … 的查询，批量读是 adjustment_id IN (…)，库级累计量分不出两者。
//
// 外部测试包的理由同 product_list_page_sql_count_test.go：要真实迁移库
// （support.NewMigratedPGTestDB），而 support 传递依赖 product/service，
// 内部测试包导入它会构成测试期导入环。
//
// 表结构来自生产迁移，不手抄 CREATE TABLE。

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

// perAdjustmentItemLookups 逐批次明细查询的次数（原实现 GetPriceAdjustment → ListAdjustmentItems 的形状）。
//
// 形状判据用「表名 + 单值 adjustment_id = 」两个片段：批量读的谓词是 adjustment_id IN (…)，
// 不含单值等号；批次头的查询则是另一张表。
func (p *sqlCountProbe) perAdjustmentItemLookups() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, sql := range p.stmts {
		if strings.Contains(sql, "product_price_adjustment_items") && strings.Contains(sql, "adjustment_id = ") {
			n++
		}
	}
	return n
}

// seedPricingHistoryFixture 造台账夹具：batchItemCounts[i] 是第 i 批的明细条数（0 = 空批次，
// 模板那边对应「这一批没有逐变体明细」那一档）。返回按插入顺序的批次 id。
//
// 两个「排序 / 名字」陷阱是故意埋的：明细按 SKU 倒序插入（第 1 条是 SKU-2、第 2 条是 SKU-1），
// 所以 sku_code 升序这条规则错了立刻露；第 3 条明细指向一个**没有商品行**的商品 id ——
// 商品名该留空（明细不带外键、快照仍可读）。
func seedPricingHistoryFixture(t *testing.T, db *gorm.DB, projectID string, batchItemCounts []int) []string {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("台账夹具备料失败：%v（SQL: %s）", err, sql)
		}
	}
	// 明细对 products / product_variants 都有外键（迁移 134，ON DELETE RESTRICT），所以每条
	// 明细都要有真实的商品行与变体行。第三个商品的名字是**空串**：商品名取自当前数据，
	// 取不到（被改名为空）时台账该留空 —— 这一档不能用「删商品」造（外键 RESTRICT），
	// 也不能用「商品属于别的工程」造（测试连接是超级用户，RLS 策略被绕过，实测照样读得到）。
	firstProduct := seedListProduct(t, db, projectID, 9001)
	secondProduct := seedListProduct(t, db, projectID, 9002)
	blankNameProduct := uuid.NewString()
	exec("INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time) "+
		"VALUES (?, ?, '', ?, ?, 'published', 3, now(), now())",
		blankNameProduct, projectID, "p-blank", "SKU-BLANK")

	// 批次时间递减：越后插的批次越新，列表（新的在前）顺序确定。
	base := time.Now().Add(-time.Duration(len(batchItemCounts)+2) * time.Minute)
	ids := make([]string, 0, len(batchItemCounts))
	for i, itemCount := range batchItemCounts {
		adjID := uuid.NewString()
		ids = append(ids, adjID)
		at := base.Add(time.Duration(i) * time.Minute)
		exec("INSERT INTO product_price_adjustments "+
			"(id, project_id, rule_type, rule_params, rounding, scope, filter, variant_count, changed_count, note, operator_id, create_time) "+
			"VALUES (?, ?, 'cost_multiple', jsonb_build_object('multiplier', 1.2), 'end_9', 'filter', '{}'::jsonb, ?, ?, ?, 'op-1', ?)",
			adjID, projectID, itemCount, itemCount, fmt.Sprintf("第 %d 批", i+1), at)
		for j := 0; j < itemCount; j++ {
			productID := firstProduct
			switch j {
			case 1:
				productID = secondProduct
			case 2:
				productID = blankNameProduct
			}
			variantID := uuid.NewString()
			exec("INSERT INTO product_variants (id, product_id, sku_code, price, enabled, sort) "+
				"VALUES (?, ?, ?, 1.00::numeric, true, 1)",
				variantID, productID, fmt.Sprintf("V-%d-%d", i+1, j+1))
			exec("INSERT INTO product_price_adjustment_items "+
				"(id, adjustment_id, product_id, variant_id, sku_code, old_price, new_price, create_time) "+
				"VALUES (?, ?, ?, ?, ?, ?::numeric, ?::numeric, ?)",
				uuid.NewString(), adjID, productID, variantID,
				fmt.Sprintf("SKU-%d", itemCount-j),
				fmt.Sprintf("%d.50", 10+i*10+j), fmt.Sprintf("%d.75", 12+i*10+j), at)
		}
	}
	return ids
}

// historyItemsOf 取台账行的明细（台账行的 Items 恒为 []map[string]any）。
func historyItemsOf(t *testing.T, row map[string]any, where string) []map[string]any {
	t.Helper()
	raw, ok := row["Items"]
	if !ok {
		t.Fatalf("%s：台账行缺 Items 键：%+v", where, row)
	}
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("%s：Items 不是 []map[string]any，实际 %T", where, raw)
	}
	return items
}

// TestProductPricingHistorySQLCountIsConstantInBatchCount 1 批与 10 批两档：
// 逐批次明细查询 0 次、SQL 总次数相等、两档的明细都逐条展开。
func TestProductPricingHistorySQLCountIsConstantInBatchCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()
	probe := attachSQLCountProbe(t, db)

	type runStat struct {
		total   int
		lookups int
		items   int
	}
	run := func(batches int) runStat {
		// 两档各用独立工程：否则 10 批那一档会读到 1 批档留下的批次（limit 10 会截断）。
		projectID := uuid.NewString()
		support.SeedProjectRow(t, db, projectID, fmt.Sprintf("台账 %d 批", batches))
		counts := make([]int, batches)
		for i := range counts {
			counts[i] = 2
		}
		seedPricingHistoryFixture(t, db, projectID, counts)

		s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
			rows: []projectdto.ProjectResp{{ID: projectID, Name: "台账 SQL 判据"}},
		})
		probe.reset()
		view, err := s.ProductPricingPage(ctx, &productdto.ProductPricingPageReq{ProjectID: projectID})
		if err != nil {
			t.Fatalf("批次数=%d：台账读模型失败：%v", batches, err)
		}
		if len(view.History) != batches {
			t.Fatalf("批次数=%d：台账 %d 行，期望 %d", batches, len(view.History), batches)
		}
		items := 0
		for _, row := range view.History {
			items += len(historyItemsOf(t, row, fmt.Sprintf("批次数=%d", batches)))
		}
		return runStat{total: probe.total(), lookups: probe.perAdjustmentItemLookups(), items: items}
	}

	one := run(1)
	ten := run(10)
	t.Logf("1 批台账：SQL 总次数=%d、逐批次明细查询=%d 次、明细 %d 条", one.total, one.lookups, one.items)
	t.Logf("10 批台账：SQL 总次数=%d、逐批次明细查询=%d 次、明细 %d 条", ten.total, ten.lookups, ten.items)

	if ten.lookups != 0 {
		t.Fatalf("10 批台账装配期间发了 %d 次逐批次明细查询（判据是 0 次）。命中语句：%s",
			ten.lookups, probe.dump())
	}
	if one.items != 2 || ten.items != 20 {
		t.Fatalf("明细没有逐条展开：1 批 %d 条（期望 2）、10 批 %d 条（期望 20）—— 「一次都不查」也会让 SQL 次数相等",
			one.items, ten.items)
	}
	if one.total != ten.total {
		t.Fatalf("SQL 次数随批次数变化：1 批 %d 次、10 批 %d 次（台账取数必须是常数条）", one.total, ten.total)
	}
}
