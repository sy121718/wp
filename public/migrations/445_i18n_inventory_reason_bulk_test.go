package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestInventoryReasonBulkSeedIdempotent(t *testing.T) {
	const version = "445-i18n-inventory-reason-bulk"
	found := false
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == version {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("库存原因批量词条未注册")
	}
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	keys := []string{"admin.inventory.bulk.reasonPartial", "admin.inventory.bulk.reasonDone", "admin.inventory.bulk.reasonNoneSelected"}
	for round := 0; round < 3; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ? AND lang IN ('zh-CN','en-US')", keys).Scan(&count).Error; err != nil || count != 6 {
			t.Fatalf("第 %d 轮双语词条数=%d err=%v", round, count, err)
		}
		if round == 0 {
			if err := db.Exec("UPDATE sys_i18n SET item_value = '运营文案' WHERE item_key = ? AND lang = 'zh-CN'", keys[0]).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'", keys[1]).Error; err != nil {
				t.Fatal(err)
			}
		} else {
			var value string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'", keys[0]).Scan(&value).Error; err != nil || value != "运营文案" {
				t.Fatalf("运营文案被覆盖: %q err=%v", value, err)
			}
		}
	}
}
