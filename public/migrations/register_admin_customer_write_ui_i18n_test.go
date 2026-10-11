package migrations_test

import (
	"go_wp/public/test/support"
	"gorm.io/gorm"
	"testing"
)

func TestAdminCustomerWriteUISeedParsesAndBackfillsPairs(t *testing.T) {
	seed := findSeedByVersion(t, "610-admin-customer-write-ui-i18n")
	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != 16 {
		t.Fatalf("pairs=%v", pairs)
	}
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE customer_write_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE customer_write_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, seed.SQL)
	for _, pair := range pairs {
		key, lang := pair.key, pair.lang
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang)
		mustExec(t, db, "INSERT INTO sys_i18n (item_key,lang,item_value,status) VALUES (?, ?, ?, 1) ON CONFLICT (item_key,lang) DO NOTHING", key, "fr-FR", "third language")
		if got := guardValue(t, db, seed); got != 0 {
			t.Fatalf("missing %s guard=%d", lang, got)
		}
		mustExec(t, db, seed.SQL)
		if got := guardValue(t, db, seed); got != 1 {
			t.Fatalf("backfill %s guard=%d", lang, got)
		}
	}
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", "operator text", "admin.customers.write.noJs", "zh-CN")
	mustExec(t, db, seed.SQL)
	var value string
	if err := db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "admin.customers.write.noJs", "zh-CN").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "operator text" {
		t.Fatalf("operator edit lost: %s", value)
	}
}
