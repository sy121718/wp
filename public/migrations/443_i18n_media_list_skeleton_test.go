package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// TestMediaListSkeletonSeedIsIdempotent 三轮判定：首放补齐、再放跳过、
// 缺 en-US 行时只补缺失语言、绝不覆盖运营手改的 zh-CN 值。
func TestMediaListSkeletonSeedIsIdempotent(t *testing.T) {
	const version = "443-i18n-media-list-skeleton"
	keys := []string{
		"admin.media.filter.label", "admin.media.list.title", "admin.media.list.aria",
		"admin.media.bulk.selected", "admin.media.bulk.download_scope", "admin.media.bulk.delete",
		"admin.media.bulk.select_all", "admin.media.bulk.select_all_aria", "admin.media.col.actions",
	}
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
	const custom = "Operator-defined file list title"
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES (?, ?, ?, 200, 'admin', 'operator', 1, now(), now())",
		"admin.media.list.title", "zh-CN", custom).Error; err != nil {
		t.Fatal(err)
	}

	for round := 1; round <= 3; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮运行 seed: %v", round, err)
		}
		for _, key := range keys {
			var got int
			if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND lang IN ('zh-CN','en-US')", key).Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got != 2 {
				t.Errorf("第 %d 轮 %s 应有中英两行，实际 %d 行", round, key, got)
			}
		}
		var values []string
		if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", "admin.media.list.title", "zh-CN").Scan(&values).Error; err != nil {
			t.Fatal(err)
		}
		if len(values) != 1 || values[0] != custom {
			t.Errorf("第 %d 轮运营值被覆盖: %v", round, values)
		}
		if round == 1 {
			if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", "admin.media.bulk.delete", "en-US").Error; err != nil {
				t.Fatal(err)
			}
		}
	}
}
