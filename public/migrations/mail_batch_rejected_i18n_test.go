package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// TestMailBatchRejectedSeed 钉住本批新增词条：key 是扁平短 key，中英两条都存在，
// 连续跑两遍种子不会重复插入。改 key 时必须同步 746 的 SQL 与注册条件。
func TestMailBatchRejectedSeed(t *testing.T) {
	const version = "746-mail-batch-rejected-i18n"
	found := false
	for _, s := range migrations.AllSeeds() {
		if s.Version == version {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s 未注册", version)
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮种子失败：%v", round, err)
		}
		var n int64
		if err := db.Raw(`SELECT COUNT(*) FROM sys_i18n
			WHERE (item_key, lang) IN (('mailBatchReject','zh-CN'),('mailBatchReject','en-US'),
			('mailCsvBad','zh-CN'),('mailCsvBad','en-US'))`).Scan(&n).Error; err != nil {
			t.Fatalf("查词条失败：%v", err)
		}
		if n != 4 {
			t.Fatalf("第 %d 轮后应有 4 条词条，实际 %d", round, n)
		}
	}
}
