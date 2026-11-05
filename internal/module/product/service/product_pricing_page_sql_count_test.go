package productservice_test

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

func seedPricingHistoryFixture(t *testing.T, db *gorm.DB, projectID string, batchItemCounts []int) []string {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("台账夹具备料失败：%v（SQL: %s）", err, sql)
		}
	}

	firstProduct := seedListProduct(t, db, projectID, 9001)
	secondProduct := seedListProduct(t, db, projectID, 9002)
	blankNameProduct := uuid.NewString()
	exec("INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time) "+
		"VALUES (?, ?, '', ?, ?, 'published', 3, now(), now())",
		blankNameProduct, projectID, "p-blank", "SKU-BLANK")

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

		projectID := uuid.NewString()
		support.SeedProjectRow(t, db, projectID, fmt.Sprintf("台账 %d 批", batches))
		counts := make([]int, batches)
		for i := range counts {
			counts[i] = 2
		}
		seedPricingHistoryFixture(t, db, projectID, counts)

		s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
			rows: []projectdto.ProjectResp{{ID: projectID, Name: "台账 SQL 判据"}},
		}, stubMasterDataChanges{})
		probe.reset()
		history, err := s.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{ProjectID: projectID, Limit: 10, IncludeItems: true})
		if err != nil {
			t.Fatalf("批次数=%d：台账读模型失败：%v", batches, err)
		}
		if len(history) != batches {
			t.Fatalf("批次数=%d：台账 %d 行，期望 %d", batches, len(history), batches)
		}
		items := 0
		for _, row := range history {
			items += len(row.Items)
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
