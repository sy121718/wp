package migrations_test

import (
	"testing"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestProductAtomicWritesSeedParsesAndBackfillsEveryLanguagePair(t *testing.T) {
	seed := findSeedByVersion(t, "695-product-atomic-writes-i18n")
	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != 10 {
		t.Fatalf("应恰好覆盖五个新键的中英文：%v", pairs)
	}
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE product_atomic_writes_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE product_atomic_writes_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, seed.SQL)
	for _, pair := range pairs {
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", pair.key, pair.lang)
		mustExec(t, db, "INSERT INTO sys_i18n (item_key,lang,item_value,status) VALUES (?, ?, ?, 1) ON CONFLICT (item_key,lang) DO NOTHING", pair.key, "fr-FR", "third language")
		if got := guardValue(t, db, seed); got != 0 {
			t.Fatalf("缺少 %s/%s 时第三语言不得掩盖缺口：%d", pair.key, pair.lang, got)
		}
		mustExec(t, db, seed.SQL)
		if got := guardValue(t, db, seed); got != 1 {
			t.Fatalf("补齐 %s/%s 后门槛不成立：%d", pair.key, pair.lang, got)
		}
	}
	const key = "prAtomicHint"
	const edited = "运营已修改的提示"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", edited, key, "zh-CN")
	mustExec(t, db, seed.SQL)
	var value string
	if err := db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", key, "zh-CN").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != edited {
		t.Fatalf("重复种子覆盖运营文案：%q", value)
	}
}
