package migrations_test

import (
	"go_wp/public/test/support"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestAdminPluginWriteUISeedGuardAgreement(t *testing.T) {
	seed := findSeedByVersion(t, "608-admin-plugin-write-ui-i18n")
	pairs := seedPairs(t, seed.SQL)
	want := map[string]bool{"admin.plugins.write.noJs|zh-CN": true, "admin.plugins.write.noJs|en-US": true, "服务器内部错误，请稍后重试|zh-CN": true, "服务器内部错误，请稍后重试|en-US": true}
	if len(pairs) != 4 {
		t.Fatalf("pairs=%v", pairs)
	}
	for _, pair := range pairs {
		id := pair.key + "|" + pair.lang
		if !want[id] {
			t.Fatalf("unexpected pair %s", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing pairs: %v", want)
	}
	const guard = "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('admin.plugins.write.noJs', '服务器内部错误，请稍后重试')"
	if seed.ConditionSQL != guard || seed.TableName != "sys_i18n" {
		t.Fatalf("guard=%s", seed.ConditionSQL)
	}
	if !strings.HasSuffix(strings.TrimSpace(seed.SQL), "ON CONFLICT (item_key, lang) DO NOTHING;") {
		t.Fatal("operator edits must survive")
	}
}

func TestAdminPluginWriteUISeedParsesAndBackfillsPairs(t *testing.T) {
	seed := findSeedByVersion(t, "608-admin-plugin-write-ui-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE plugin_write_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE plugin_write_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	keys := []string{"admin.plugins.write.noJs", "服务器内部错误，请稍后重试"}
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN ?", keys)
	mustExec(t, db, seed.SQL)
	if got := guardValue(t, db, seed); got != 1 {
		t.Fatalf("complete guard=%d", got)
	}
	for _, key := range keys {
		for _, lang := range []string{"zh-CN", "en-US"} {
			mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang)
			mustExec(t, db, "INSERT INTO sys_i18n (item_key,lang,item_value,status) VALUES (?, 'fr-FR', 'third language', 1) ON CONFLICT (item_key,lang) DO NOTHING", key)
			if got := guardValue(t, db, seed); got != 0 {
				t.Fatalf("missing %s/%s guard=%d", key, lang, got)
			}
			mustExec(t, db, seed.SQL)
			if got := guardValue(t, db, seed); got != 1 {
				t.Fatalf("backfilled guard=%d", got)
			}
		}
	}
}
