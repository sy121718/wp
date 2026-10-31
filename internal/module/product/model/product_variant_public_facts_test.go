package productmodel_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

func TestVariantPublicFactsSelectsNoCostColumn(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "公开变体投影")

	productID, variantID := uuid.NewString(), uuid.NewString()
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
		VALUES (?, ?, '投影商品', 'proj-variant', 'SKU-PROJ', 'published', 1, now(), now())`,
		productID, projectID).Error; err != nil {
		t.Fatalf("夹具备料失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO product_variants (id, product_id, sku_code, price, compare_price, cost_price, option_values, enabled, sort, metadata, create_time, update_time)
		VALUES (?, ?, 'SKU-PROJ-1', 12.50, 20.00, 7.00, '{"颜色":"红"}'::jsonb, true, 1, '{}'::jsonb, now(), now())`,
		variantID, productID).Error; err != nil {
		t.Fatalf("夹具备料失败：%v", err)
	}

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("variant:public-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "product_variants") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	rows, err := productmodel.NewModel(db).ListVariantPublicFacts(context.Background(), []string{variantID}, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if captured == "" {
		t.Fatal("没抓到读 product_variants 的 SQL（回调没生效）")
	}
	for _, banned := range []string{"cost_price", "warehouse_skus", "option_values", "metadata", "barcode", "create_time"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("访客侧投影不该读 %q：%s", banned, captured)
		}
	}
	if len(rows) != 1 || rows[0].ID != variantID || rows[0].ProductID != productID {
		t.Fatalf("命中的事实不对：%+v", rows)
	}
	if rows[0].Price != 12.5 || !rows[0].Enabled {
		t.Fatalf("价格与启用状态必须照常读出：%+v", rows[0])
	}
	if rows[0].CostPrice != nil {
		t.Fatalf("成本价不该被读出来：%+v", rows[0])
	}

	rows, err = productmodel.NewModel(db).ListVariantPublicFacts(context.Background(), nil, projectID)
	if err != nil || rows != nil {
		t.Fatalf("空清单应直接返回 nil：%v / %+v", err, rows)
	}
}

func TestVariantDisplayFactsSelectsBuildColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "构建期投影")

	productID, variantID := uuid.NewString(), uuid.NewString()
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
		VALUES (?, ?, '构建商品', 'build-variant', 'SKU-BUILD', 'published', 1, now(), now())`,
		productID, projectID).Error; err != nil {
		t.Fatalf("夹具备料失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO product_variants (id, product_id, sku_code, price, compare_price, cost_price, image, option_values, enabled, sort, metadata, create_time, update_time)
		VALUES (?, ?, 'SKU-BUILD-1', 30.00, 45.00, 11.00, '/storage/v.png', '{"颜色":"红"}'::jsonb, true, 1, '{}'::jsonb, now(), now())`,
		variantID, productID).Error; err != nil {
		t.Fatalf("夹具备料失败：%v", err)
	}

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("variant:display-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "product_variants") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	rows, err := productmodel.NewModel(db).ListVariantDisplayFacts(context.Background(), []string{productID})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	for _, banned := range []string{"cost_price", "warehouse_skus", "metadata", "barcode"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("构建期投影不该读 %q：%s", banned, captured)
		}
	}
	for _, needed := range []string{"option_values", "compare_price", "sku_code"} {
		if !strings.Contains(captured, needed) {
			t.Fatalf("构建期投影漏了 %q（optionsJSON / 划线价 / 首个 SKU 都要它）：%s", needed, captured)
		}
	}
	if len(rows) != 1 || rows[0].SKUCode != "SKU-BUILD-1" || rows[0].ComparePrice == nil || *rows[0].ComparePrice != 45 {
		t.Fatalf("投影出来的事实不对：%+v", rows)
	}
	if len(rows[0].OptionValues) == 0 {
		t.Fatalf("规格组合必须读出（集合项的 variants JSON 用它）：%+v", rows[0])
	}
	if rows[0].CostPrice != nil {
		t.Fatalf("成本价不该被读进构建进程：%+v", rows[0])
	}
}
