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
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
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
		options: []productdto.WarehouseOptionResp{{ID: parityWarehouseID, Label: "主仓"}},
	})
	s.SetInventoryService(parityStockPortStub{rows: []inventorydto.ProductWarehouseStock{
		{ProductID: fx.variantProduct, WarehouseID: parityWarehouseID, WarehouseName: "主仓",
			TrackQuantity: true, Quantity: 7},
	}})

	ctx := context.Background()
	listView, err := s.ProductListPage(ctx, &productdto.ProductListPageReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("列表页读模型失败：%v", err)
	}
	if len(listView.Products) != 2 || len(listView.RowDetails) != 2 {
		t.Fatalf("期望 2 行（变体 + 容器），实际成品行 %d、原料行 %d",
			len(listView.Products), len(listView.RowDetails))
	}

	for i, item := range listView.RowDetails {
		detailView, derr := s.ProductDetailPage(ctx, &productdto.ProductDetailPageReq{
			ProjectID: projectID, ProductID: item.ID,
		})
		if derr != nil {
			t.Fatalf("详情页读模型取 %s 失败：%v", item.ID, derr)
		}
		if detailView.Product == nil {
			t.Fatalf("详情页没有装配出成品行（商品 %s）", item.ID)
		}
		want := detailView.Product
		got := listView.Products[i]
		if got.ID != want.ID {
			t.Fatalf("第 %d 行的顺序不一致：列表行 %s、详情行 %s", i, got.ID, want.ID)
		}

		// 夹具自检：被比较的字段必须在详情行上非零 —— 否则「两边都空」也会相等。
		nonEmpty := []struct{ name, value string }{
			{"PriceMin", amountText(want.PriceMin)}, {"PriceMax", amountText(want.PriceMax)},
			{"BrandName", want.BrandName}, {"TagNames", strings.Join(want.TagNames, ",")},
		}
		if detailView.Detail != nil {
			nonEmpty = append(nonEmpty,
				struct{ name, value string }{"CategoryCell", want.CategoryCell},
				struct{ name, value string }{"RatingAvg", want.RatingAvg})
		}
		for _, c := range nonEmpty {
			if c.value == "" {
				t.Fatalf("夹具自检失败：商品 %s 的 %s 在详情行上是空值，对账会变成「两边都空」的假绿",
					got.Name, c.name)
			}
		}

		pairs := []struct{ name, got, want string }{
			{"ID", got.ID, want.ID},
			{"Name", got.Name, want.Name},
			{"Slug", got.Slug, want.Slug},
			{"SKUCode", got.SKUCode, want.SKUCode},
			{"Status", got.Status, want.Status},
			{"PriceMin", amountText(got.PriceMin), amountText(want.PriceMin)},
			{"PriceMax", amountText(got.PriceMax), amountText(want.PriceMax)},
			{"CategoryCell", got.CategoryCell, want.CategoryCell},
			{"BrandName", got.BrandName, want.BrandName},
			{"TagNames", strings.Join(got.TagNames, ","), strings.Join(want.TagNames, ",")},
			{"RatingAvg", got.RatingAvg, want.RatingAvg},
			{"RatingCount", strconv.Itoa(got.RatingCount), strconv.Itoa(want.RatingCount)},
		}
		for _, p := range pairs {
			if p.got != p.want {
				t.Errorf("商品 %s 的字段 %s：列表行 %q、详情行 %q", got.Name, p.name, p.got, p.want)
			}
		}
		if got.VariantCount != want.VariantCount {
			t.Errorf("商品 %s 的 VariantCount：列表行 %d、详情行 %d", got.Name, got.VariantCount, want.VariantCount)
		}
		if got.CategoryOthers != want.CategoryOthers {
			t.Errorf("商品 %s 的 CategoryOthers：列表行 %d、详情行 %d", got.Name, got.CategoryOthers, want.CategoryOthers)
		}
		if got.HasRating != want.HasRating {
			t.Errorf("商品 %s 的 HasRating：列表行 %v、详情行 %v", got.Name, got.HasRating, want.HasRating)
		}
		if !reflect.DeepEqual(got.Stock, want.Stock) {
			t.Errorf("商品 %s 的库存单元格：列表行 %#v、详情行 %#v", got.Name, got.Stock, want.Stock)
		}

		// 批量事实（applyRowFacts）唯一的产物：挂载 id。字符串列巧合相等时它们会先露馅。
		if detailView.Detail == nil {
			t.Fatalf("详情页没有详情对象（商品 %s）", item.ID)
		}
		detail := detailView.Detail
		if !reflect.DeepEqual(item.CategoryIDs, detail.CategoryIDs) {
			t.Errorf("商品 %s 的 CategoryIDs：批量事实 %v、详情 %v", got.Name, item.CategoryIDs, detail.CategoryIDs)
		}
		if item.PrimaryCategoryID != detail.PrimaryCategoryID {
			t.Errorf("商品 %s 的 PrimaryCategoryID：批量事实 %q、详情 %q",
				got.Name, item.PrimaryCategoryID, detail.PrimaryCategoryID)
		}
		if item.BrandID != detail.BrandID {
			t.Errorf("商品 %s 的 BrandID：批量事实 %q、详情 %q", got.Name, item.BrandID, detail.BrandID)
		}
		if !reflect.DeepEqual(item.TagIDs, detail.TagIDs) {
			t.Errorf("商品 %s 的 TagIDs：批量事实 %v、详情 %v", got.Name, item.TagIDs, detail.TagIDs)
		}
	}

	// 绝对值（对账只看「两边相等」，相等也可能两边都错）。
	for _, row := range listView.Products {
		switch row.Name {
		case "变体商品":
			// 价格是原值（元）：12.5 / 30，展示文本由模板 amount() 生成。
			if row.PriceMin != 12.5 || row.PriceMax != 30 || row.VariantCount != 2 {
				t.Errorf("变体商品的价格区间/变体数：%v~%v，%d 个", row.PriceMin, row.PriceMax, row.VariantCount)
			}
			if !row.HasRating || row.RatingCount != 2 || row.RatingAvg != "4.50" {
				t.Errorf("变体商品的评分段：HasRating=%v count=%d avg=%q", row.HasRating, row.RatingCount, row.RatingAvg)
			}
			if len(row.Ratings) != 0 {
				t.Errorf("列表行不加载评分**明细**（只读投影值），实际 %d 条", len(row.Ratings))
			}
			if row.Stock == nil {
				t.Fatalf("变体商品应带库存单元格")
			}
			if row.Stock.State != productenums.StockStateTracked {
				t.Errorf("变体商品的库存三态应为 %s，实际 %v", productenums.StockStateTracked, row.Stock.State)
			}
		case "单品商品":
			if row.PriceMin != 88.5 || row.PriceMax != 88.5 {
				t.Errorf("单品商品按唯一变体取价：%v~%v", row.PriceMin, row.PriceMax)
			}
		}
	}
}
