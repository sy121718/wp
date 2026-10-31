package migrations_test

import (
	"testing"

	"gorm.io/gorm"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestProductErrorDetailSeedPreservesTranslations(t *testing.T) {
	var seed migrations.Seed
	for _, item := range migrations.AllSeeds() {
		if item.Version == "697-product-error-detail-keys" {
			seed = item
			break
		}
	}
	if seed.Version == "" {
		t.Fatal("商品错误明细迁移未注册")
	}
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	// 模板库只保证迁移结构；先建立真实旧版词条，再模拟运营修改。
	for _, historical := range migrations.AllSeeds() {
		if historical.Version != "449-i18n-product-inventory-go-texts" {
			continue
		}
		for _, stmt := range migrations.SplitStatements(historical.SQL) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// 上线前校验新 SQL 的解析及表列绑定，不执行 PREPARE 中的语句。
	if err := db.Connection(func(conn *gorm.DB) error {
		for _, stmt := range migrations.SplitStatements(seed.SQL) {
			if err := conn.Exec("PREPARE product_detail_seed AS " + stmt).Error; err != nil {
				return err
			}
			if err := conn.Exec("DEALLOCATE product_detail_seed").Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	const old = "admin.product_pricing.detail.paramRequired"
	const target = "product.err.pricingParamRequired"
	if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ?", target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", "Custom {field}", old, "en-US").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES (?, 'fr-FR', 'Requis {field}', 200, 'admin', 'operator', 1, now(), now()) ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value", old).Error; err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		var applied int64
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil || applied != 0 {
			t.Fatalf("缺词时误判完成: %d %v", applied, err)
		}
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
			t.Fatalf("迁移未完成: %d %v", applied, err)
		}
		for _, tc := range []struct{ key, lang, want string }{
			{target, "en-US", "Custom {field}"}, {target, "fr-FR", "Requis {field}"}, {old, "en-US", "Custom {field}"},
			{productenums.DetailTagParamRequired, "zh-CN", "{field} 必填"},
		} {
			var got string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", tc.key, tc.lang).Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%s/%s=%q，期望%q", tc.key, tc.lang, got, tc.want)
			}
		}
		for key, fallback := range productenums.ErrDetailFallbacks {
			var rows []struct {
				Lang      string
				ItemValue string
			}
			if err := db.Raw("SELECT lang, item_value FROM sys_i18n WHERE item_key = ? AND lang IN ('zh-CN', 'en-US')", key).Scan(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 {
				t.Errorf("明细缺基础语言: %s: %v", key, rows)
			}
			for _, row := range rows {
				if row.Lang == "zh-CN" && row.ItemValue != fallback {
					t.Errorf("明细兜底与迁移不一致: %s: %q/%q", key, row.ItemValue, fallback)
				}
			}
		}
		if round == 0 {
			if err := db.Exec("UPDATE sys_i18n SET item_value = '旧词条后续变化' WHERE item_key = ? AND lang = 'zh-CN'", old).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'", productenums.DetailTagParamRequired).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var got string
	if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'", target).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != "{field} 必填" {
		t.Fatalf("已有目标译文被覆盖: %q", got)
	}
}
