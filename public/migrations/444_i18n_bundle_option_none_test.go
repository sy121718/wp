package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestBundleOptionNoneSeedIsIdempotent(t *testing.T) {
	const key = "admin.product_bundle.option.none"
	var seed migrations.Seed
	for _, candidate := range migrations.AllSeeds() {
		if candidate.Version == "444-i18n-bundle-option-none" {
			seed = candidate
			break
		}
	}
	if seed.Version == "" {
		t.Fatal("捆绑候选空选项词条未注册")
	}
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	const custom = "运营自定义空选项"
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES (?, 'zh-CN', ?, 200, 'admin', 'operator', 1, now(), now())", key, custom).Error; err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 3; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND lang IN ('zh-CN','en-US')", key).Scan(&count).Error; err != nil || count != 2 {
			t.Fatalf("第 %d 轮缺双语词条: count=%d err=%v", round, count, err)
		}
		var value string
		if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'", key).Scan(&value).Error; err != nil || value != custom {
			t.Fatalf("第 %d 轮覆盖运营值: value=%q err=%v", round, value, err)
		}
		if round == 0 {
			if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'", key).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
}
