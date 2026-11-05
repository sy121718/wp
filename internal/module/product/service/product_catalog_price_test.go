package productservice_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	"go_wp/public/test/support"
)

func seedCatalogVariants(t *testing.T, db *gorm.DB, productID string, rows []struct {
	id, sku             string
	price, compare      float64
	hasCompare, enabled bool
}) {
	t.Helper()
	for i, row := range rows {
		var compare any
		if row.hasCompare {
			compare = row.compare
		}
		sql := "INSERT INTO product_variants (id, product_id, sku_code, barcode, price, compare_price, cost_price, image, option_values, enabled, sort, metadata, warehouse_skus, create_time, update_time) " +
			"VALUES (?, ?, ?, '', ?, ?, NULL, '', '{}'::jsonb, ?, ?, '{}'::jsonb, '[]'::jsonb, now(), now())"
		if err := db.Exec(sql, row.id, productID, row.sku, row.price, compare, row.enabled, i).Error; err != nil {
			t.Fatalf("插入变体 %s 失败：%v", row.id, err)
		}
	}
}

func TestPublicCatalogPriceUsesEnabledCheapestVariant(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "目录价格口径")
	productID := uuid.NewString()
	if err := db.Exec(
		"INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time) "+
			"VALUES (?, ?, '价格口径商品', 'price-scope', 'SKU-PRICE', 'published', 1, now(), now())",
		productID, projectID).Error; err != nil {
		t.Fatalf("插入商品失败：%v", err)
	}

	seedCatalogVariants(t, db, productID, []struct {
		id, sku             string
		price, compare      float64
		hasCompare, enabled bool
	}{
		{id: uuid.NewString(), sku: "SKU-A", price: 10, enabled: true},
		{id: uuid.NewString(), sku: "SKU-B", price: 20, compare: 999, hasCompare: true, enabled: true},
		{id: uuid.NewString(), sku: "SKU-C", price: 1, compare: 5000, hasCompare: true, enabled: false},
	})

	seedCatalogPublication(t, db, projectID, "product", productID, "detail", "zh-CN", "/p/price-scope", "")
	s := productservice.NewService(productmodel.NewModel(db), newCatalogProjectStub(projectID, "zh-CN"), stubMasterDataChanges{})
	out, err := s.PublicCatalog(context.Background(), &productdto.PublicCatalogReq{
		ProjectID: projectID, Page: 1, PageSize: 12,
	})
	if err != nil {
		t.Fatalf("目录查询失败：%v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("应返回 1 个商品，实际 %d", len(out.Items))
	}
	item := out.Items[0]
	if item.Price == nil || *item.Price != 1000 {
		t.Fatalf("售价应是启用变体最低价 1000 分，实际 %v", item.Price)
	}
	if item.ComparePrice != nil {
		t.Fatalf("最低价变体没有划线价时不应借用别的变体，实际 %d", *item.ComparePrice)
	}
	if item.MaxPrice == nil || *item.MaxPrice != 2000 {
		t.Fatalf("最高价应只算启用变体（2000 分），实际 %v", item.MaxPrice)
	}
}
