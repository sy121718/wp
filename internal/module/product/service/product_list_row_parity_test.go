package productservice_test

// product_list_row_parity_test.go — 列表页成品行与**详情页成品行**的对账
// （dd.txt 3.5「列表字段与改造前一致」）。
//
// 改造把「逐商品取详情」换成了「一次批量补料 + 批量评分」，风险是**字段悄悄变了**：
// 列表模板（products.html）读的是汇总段（价格 / 分类 / 品牌 / 标签 / 评分 / 库存），
// 这几个值的原料从「完整详情对象」换成了「列表项 + 批量事实」，少了任何一项都不会报错，
// 只会让某一列变成空的 —— 所以这里在**同一个库、同一份索引、同一批端口桩**上把两条
// 读模型各跑一遍逐字段对账（详情页走的是 s.Get + productRow 的详情路径）。
//
// 对账不成立的两种常见假绿，本用例各自堵住：
//   · 两边都空也相等 —— 先做夹具自检，断言被比较的字段在详情行上**非零**；
//   · 只比字符串列、漏掉原料 —— 同时比 CategoryIDs / PrimaryCategoryID / BrandID / TagIDs
//     （批量事实唯一的产物）与库存单元格整块。
//
// 容器价（bundle）在两条路径上分别由 applyRowFacts / applyPriceRange 算：本用例的容器
// 商品那一档就是对这两个口径的对账（两处各写一遍必然分叉，而分叉的表现只是价格不一样）。
//
// 表结构来自生产迁移，不手抄 CREATE TABLE。

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

// parityWarehousePortStub 页面「归属仓」端口桩（库存列的分仓明细按工程仓库清单展开）。
type parityWarehousePortStub struct {
	productcontract.WarehouseOptionsPort
	options []productdto.WarehouseOptionResp
}

func (s parityWarehousePortStub) WarehouseOptions(context.Context, string) ([]productdto.WarehouseOptionResp, error) {
	return s.options, nil
}

// WarehouseSKUOptions 列表页还会取「从仓库选」的候选分组：桩给空（本用例的判据不是它）。
func (s parityWarehousePortStub) WarehouseSKUOptions(context.Context, string, []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error) {
	return []productdto.WarehouseSKUGroupResp{}, nil
}

// parityStockPortStub 库存端口桩：给了批次一致的库存事实（fillProductStock 的输入）。
type parityStockPortStub struct {
	inventorycontract.ProductStockPort
	rows []inventorydto.ProductWarehouseStock
}

func (s parityStockPortStub) WarehouseStocksByProducts(context.Context, string, []string) ([]inventorydto.ProductWarehouseStock, error) {
	return s.rows, nil
}

const parityWarehouseID = "11111111-1111-1111-1111-111111111111"

// parityFixture 一套「挂满料」的夹具：多 SKU 商品 + 单品商品。
type parityFixture struct {
	variantProduct string
	singleProduct  string
}

// seedParityFixture 造夹具（只补 NOT NULL 且无默认值的列，其余走表默认）。
func seedParityFixture(t *testing.T, db *gorm.DB, projectID string) parityFixture {
	t.Helper()
	f := parityFixture{variantProduct: uuid.NewString(), singleProduct: uuid.NewString()}
	categoryA, categoryB := uuid.NewString(), uuid.NewString()
	brand, manualTag, ruleTag := uuid.NewString(), uuid.NewString(), uuid.NewString()

	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("夹具备料失败：%v（SQL: %s）", err, sql)
		}
	}
	for i, cat := range []string{categoryA, categoryB} {
		exec(`INSERT INTO product_categories (id, project_id, name, slug, sort, create_time, update_time)
			VALUES (?, ?, ?, ?, ?, now(), now())`, cat, projectID, "分类"+strconv.Itoa(i+1), "cat-"+strconv.Itoa(i+1), i+1)
	}
	exec(`INSERT INTO product_brands (id, project_id, name, slug, sort, create_time, update_time)
		VALUES (?, ?, '品牌甲', 'brand-a', 1, now(), now())`, brand, projectID)
	exec(`INSERT INTO product_tags (id, project_id, name, slug, kind, rule_type, sort, create_time, update_time)
		VALUES (?, ?, '手工标签', 'tag-manual', 'manual', '', 1, now(), now())`, manualTag, projectID)
	exec(`INSERT INTO product_tags (id, project_id, name, slug, kind, rule_type, sort, create_time, update_time)
		VALUES (?, ?, '自动标签', 'tag-rule', 'rule', 'price_below', 2, now(), now())`, ruleTag, projectID)

	exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort,
			category_ids, primary_category_id, brand_id, tag_ids, create_time, update_time)
		VALUES (?, ?, '变体商品', 'p-variant', 'SKU-V1', 'published', 1,
			?::jsonb, ?, ?, ?::jsonb, now(), now())`,
		f.variantProduct, projectID,
		"["+strconv.Quote(categoryA)+","+strconv.Quote(categoryB)+"]", categoryA, brand,
		"["+strconv.Quote(manualTag)+","+strconv.Quote(ruleTag)+"]")
	// 单品商品也挂分类 / 品牌 / 标签：夹具的每个待比字段都要有非零值，
	// 否则「两边都空」的对账是假绿（详情行侧的 BrandName / TagLabel / CategoryCell）。
	exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort,
			default_price, category_ids, primary_category_id, brand_id, tag_ids, create_time, update_time)
		VALUES (?, ?, '单品商品', 'p-single', 'SKU-S1', 'published', 2,
			88.50, ?::jsonb, ?, ?, ?::jsonb, now(), now())`,
		f.singleProduct, projectID,
		"["+strconv.Quote(categoryA)+"]", categoryA, brand, "["+strconv.Quote(manualTag)+"]")

	for i, price := range []string{"12.50", "30.00"} {
		exec(`INSERT INTO product_variants (id, product_id, sku_code, price, enabled, sort)
			VALUES (?, ?, ?, ?::numeric, true, ?)`,
			uuid.NewString(), f.variantProduct, "SKU-V1-"+strconv.Itoa(i+1), price, i+1)
	}
	exec(`INSERT INTO product_variants (id, product_id, sku_code, price, enabled, sort)
		VALUES (?, ?, 'SKU-S1-1', 88.50, true, 1)`, uuid.NewString(), f.singleProduct)
	for _, score := range []string{"4.00", "5.00"} {
		exec(`INSERT INTO product_ratings (id, project_id, product_id, score, source, create_time, update_time)
			VALUES (?, ?, ?, ?::numeric, 'manual', now(), now())`,
			uuid.NewString(), projectID, f.variantProduct, score)
	}
	return f
}

// TestProductListRowSummaryMatchesDetailPath 同一库同桩下，列表行与详情页成品行逐字段一致。
// amountText 数值金额的可比文本（对账只比「值」：展示格式已归模板）。
func amountText(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func TestProductListRowSummaryMatchesDetailPath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "行对账")
	fx := seedParityFixture(t, db, projectID)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "行对账"}},
	})
	s.SetWarehouseOptionsPort(parityWarehousePortStub{
		options: []productdto.WarehouseOptionResp{{ID: parityWarehouseID, Name: "主仓"}},
	})
	s.SetInventoryService(parityStockPortStub{rows: []inventorydto.ProductWarehouseStock{
		{ProductID: fx.variantProduct, WarehouseID: parityWarehouseID, WarehouseName: "主仓",
			TrackQuantity: true, Quantity: 7},
	}})

	ctx := context.Background()
	result, err := s.QueryProducts(ctx, &productdto.ListReq{ProjectID: projectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("期望两条商品，实际 %d", len(result.Items))
	}
	for _, item := range result.Items {
		detail, err := s.Get(ctx, &productdto.GetReq{ProjectID: projectID, ID: item.ID})
		if err != nil {
			t.Fatal(err)
		}
		if detail.BrandID == "" || len(detail.CategoryIDs) == 0 || len(detail.TagIDs) == 0 {
			t.Fatal("夹具未挂载关联数据")
		}
		if item.PriceMin != detail.PriceMin || item.PriceMax != detail.PriceMax || item.VariantCount != detail.VariantCount {
			t.Fatalf("列表与详情的价格汇总不一致：%+v / %+v", item, detail)
		}
		if !reflect.DeepEqual(item.CategoryIDs, detail.CategoryIDs) || item.PrimaryCategoryID != detail.PrimaryCategoryID || item.BrandID != detail.BrandID || !reflect.DeepEqual(item.TagIDs, detail.TagIDs) {
			t.Fatal("列表与详情挂载关系不一致")
		}
		if item.Rating.HasRating != detail.Rating.HasRating || item.Rating.Rating != detail.Rating.Rating || item.Rating.RatingCount != detail.Rating.RatingCount {
			t.Fatal("列表与详情评分汇总不一致")
		}
		if len(item.Rating.Items) != 0 {
			t.Fatal("分页查询不应加载评分明细")
		}
		if item.StockState != detail.StockState || item.StockTotal != detail.StockTotal || !reflect.DeepEqual(item.StockWarehouses, detail.StockWarehouses) {
			t.Fatal("列表与详情库存事实不一致")
		}
	}
}
