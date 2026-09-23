package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestProductRatingCountHeaderSeed(t *testing.T) {
	const version = "438-i18n-product-rating-count-header"
	var seed migrations.Seed
	for _, item := range migrations.AllSeeds() {
		if item.Version == version {
			seed = item
			break
		}
	}
	if seed.Version == "" {
		t.Fatalf("种子 %s 未注册", version)
	}
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	var applied int64
	if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("首次执行前应待补，实际 %d", applied)
	}
	for round := 0; round < 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 次执行种子失败: %v", round+1, err)
		}
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil {
			t.Fatal(err)
		}
		if applied != 1 {
			t.Fatalf("第 %d 次执行后应判定为已应用，实际 %d", round+1, applied)
		}
	}
	for lang, want := range map[string]string{"zh-CN": "评分数", "en-US": "Ratings"} {
		var values []string
		if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", "admin.products.rating.countHeader", lang).Scan(&values).Error; err != nil {
			t.Fatal(err)
		}
		if len(values) != 1 || values[0] != want {
			t.Errorf("%s 应有一条 %q，实际 %v", lang, want, values)
		}
	}
}
