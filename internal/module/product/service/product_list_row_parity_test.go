package productservice_test

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

type parityWarehousePortStub struct {
	productcontract.WarehouseOptionsPort
	options []productdto.WarehouseOptionResp
}

func (s parityWarehousePortStub) WarehouseOptions(context.Context, string) ([]productdto.WarehouseOptionResp, error) {
	return s.options, nil
}

func (s parityWarehousePortStub) WarehouseSKUOptions(context.Context, string, []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error) {
	return []productdto.WarehouseSKUGroupResp{}, nil
}

type parityStockPortStub struct {
	inventorycontract.ProductStockPort
	rows []inventorydto.ProductWarehouseStock
}

func (s parityStockPortStub) WarehouseStocksByProducts(context.Context, string, []string) ([]inventorydto.ProductWarehouseStock, error) {
	return s.rows, nil
}

const parityWarehouseID = "11111111-1111-1111-1111-111111111111"

type parityFixture struct {
	variantProduct string
	singleProduct  string
}

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

	exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort,
			default_price, category_ids, primary_category_id, brand_id, tag_ids, create_time, update_time)
		VALUES (?, ?, '单品商品', 'p-single', 'SKU-S1', 'published', 2,
			88.50, ?::jsonb, ?, ?, ?::jsonb, now(), now())`,
		f.singleProduct, projectID,
		"["+strconv.Quote(categoryA)+"]", categoryA, brand, "["+strconv.Quote(manualTag)+"]")

	attributeID := uuid.NewString()
	exec(`INSERT INTO product_attributes (id, project_id, key, name, create_time, update_time)
		VALUES (?, ?, 'material', '材质', now(), now())`, attributeID, projectID)
	for _, productID := range []string{f.variantProduct, f.singleProduct} {
		exec(`UPDATE products SET attribute_ids = ?::jsonb, images_alt = ?::jsonb WHERE id = ?`,
			"["+strconv.Quote(attributeID)+"]", `["商品主图"]`, productID)
	}

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

func amountText(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func TestProductListRowSummaryMatchesDetailPath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "行对账")
	fx := seedParityFixture(t, db, projectID)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "行对账"}},
	}, stubMasterDataChanges{})
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
		if len(detail.AttributeIDs) == 0 || len(detail.Attributes) == 0 || len(detail.ImageAlts) == 0 {
			t.Fatal("夹具缺少属性或图片替代文本")
		}
		if !reflect.DeepEqual(item.AttributeIDs, detail.AttributeIDs) || !reflect.DeepEqual(item.Attributes, detail.Attributes) || !reflect.DeepEqual(item.ImageAlts, detail.ImageAlts) {
			t.Fatalf("列表与详情的属性及图片替代文本不一致：%+v / %+v", item, detail)
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
