package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func pageTranslationsSavedValues(t *testing.T, sqlText string) map[string]string {
	t.Helper()
	rows := regexp.MustCompile(`(?m)^\('([^']+)', '([^']+)', '((?:[^']|'')*)', 200,`).FindAllStringSubmatch(sqlText, -1)
	if len(rows) != 4 {
		t.Fatalf("期望 4 条新词条行（2 key × 2 语言），实际 %d", len(rows))
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row[1]+"|"+row[2]] = strings.ReplaceAll(row[3], "''", "'")
	}
	return values
}

func TestPageTranslationsSavedSeedAgreement(t *testing.T) {
	t.Parallel()
	seed := findSeedByVersion(t, "613-page-translations-saved-i18n")
	want := map[string]string{
		"admin.page_translations.saved|zh-CN":         "已保存 %s 条译文。",
		"admin.page_translations.saved|en-US":         "Saved %s translations.",
		"admin.page_translations.saved_rebuild|zh-CN": "已保存 %s 条译文。译文变更已标记全站待重建（下次构建生效）。",
		"admin.page_translations.saved_rebuild|en-US": "Saved %s translations. The changes marked the whole site for rebuild (they take effect at the next build).",
	}
	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != len(want) {
		t.Fatalf("613 必须恰好四条 (key, lang) 组合，实际 %v", pairs)
	}
	seen := map[string]bool{}
	values := pageTranslationsSavedValues(t, seed.SQL)
	for _, pair := range pairs {
		id := pair.key + "|" + pair.lang
		value, ok := want[id]
		if !ok || seen[id] {
			t.Fatalf("613 出现未预期或重复的组合 %s", id)
		}
		seen[id] = true
		if values[id] != value {
			t.Errorf("%s 的句值 = %q，期望 %q", id, values[id], value)
		}
	}

	for id, value := range want {
		if !strings.Contains(value, "%s") {
			t.Errorf("%s 的句值缺少 %%s 占位符：%q", id, value)
		}
	}

	for _, old := range []string{
		"admin.page_translations.saved_lead",
		"admin.page_translations.saved_mid",
		"admin.page_translations.saved_tail",
	} {
		if !strings.Contains(seed.SQL, old) {
			t.Errorf("613 必须删掉旧词条 %s（否则表里两份真源并存）", old)
		}
	}
	if !strings.Contains(seed.SQL, "DELETE FROM sys_i18n") {
		t.Error("613 必须包含 DELETE FROM sys_i18n")
	}

	if strings.Index(seed.SQL, "DELETE FROM sys_i18n") > strings.Index(seed.SQL, "INSERT INTO sys_i18n") {
		t.Error("613 的 DELETE 必须在 INSERT 之前")
	}

	const guard = "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
		"WHERE (item_key, lang) IN (" +
		"('admin.page_translations.saved', 'zh-CN'), ('admin.page_translations.saved', 'en-US'), " +
		"('admin.page_translations.saved_rebuild', 'zh-CN'), ('admin.page_translations.saved_rebuild', 'en-US'))"
	if seed.TableName != "sys_i18n" || seed.ConditionSQL != guard {
		t.Fatalf("613 的判据必须逐条枚举本批的四个组合，实际 %s", seed.ConditionSQL)
	}
	if !strings.HasSuffix(strings.TrimSpace(seed.SQL), "ON CONFLICT (item_key, lang) DO NOTHING;") {
		t.Fatal("613 必须在冲突时保留运营改过的值")
	}
}

func TestPageTranslationsSavedSeedRunsOnEmptySchema(t *testing.T) {
	seed := findSeedByVersion(t, "613-page-translations-saved-i18n")
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE page_translations_saved_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE page_translations_saved_seed").Error
	}); err != nil {
		t.Fatalf("PREPARE seed: %v", err)
	}

	keys := map[string]bool{
		"admin.page_translations.saved":         true,
		"admin.page_translations.saved_rebuild": true,
	}

	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN (?, ?, ?)",
		"admin.page_translations.saved_lead", "admin.page_translations.saved_mid", "admin.page_translations.saved_tail")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN (?, ?)",
		"admin.page_translations.saved", "admin.page_translations.saved_rebuild")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
		VALUES ('admin.page_translations.saved_lead', 'zh-CN', '已保存 ', 200, 'admin', 'legacy', 1, now(), now())`)
	if got := guardValue(t, db, seed); got != 0 {
		t.Fatalf("新两条缺失时判据 = %d，期望 0（否则 SQL 被静默跳过）", got)
	}

	mustExec(t, db, seed.SQL)
	if got := guardValue(t, db, seed); got != 1 {
		t.Fatalf("跑完后判据 = %d，期望 1", got)
	}
	if got := countPairs(t, db, keys, ""); got != 4 {
		t.Fatalf("新词条落库 %d 行，期望 4", got)
	}

	for _, old := range []string{
		"admin.page_translations.saved_lead",
		"admin.page_translations.saved_mid",
		"admin.page_translations.saved_tail",
	} {
		mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) "+
			"VALUES (?, 'zh-CN', 'legacy', 200, 'admin', 'legacy', 1, now(), now()) "+
			"ON CONFLICT (item_key, lang) DO NOTHING", old)
	}
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN (?, ?)",
		"admin.page_translations.saved", "admin.page_translations.saved_rebuild")
	mustExec(t, db, seed.SQL)
	for _, old := range []string{
		"admin.page_translations.saved_lead",
		"admin.page_translations.saved_mid",
		"admin.page_translations.saved_tail",
	} {
		if got := valueOf(t, db, old, "zh-CN"); got != "" {
			t.Errorf("旧词条 %s 应被删除，实际仍有值 %q", old, got)
		}
	}

	const operatorValue = "Operator edited sentence %s"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?",
		operatorValue, "admin.page_translations.saved", "zh-CN")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?",
		"admin.page_translations.saved", "en-US")
	if got := guardValue(t, db, seed); got != 0 {
		t.Fatalf("缺一条时判据 = %d，期望 0", got)
	}
	mustExec(t, db, seed.SQL)
	if got := valueOf(t, db, "admin.page_translations.saved", "zh-CN"); got != operatorValue {
		t.Errorf("运营改过的值被覆盖：%q", got)
	}
	if got := valueOf(t, db, "admin.page_translations.saved", "en-US"); got != pageTranslationsSavedValues(t, seed.SQL)["admin.page_translations.saved|en-US"] {
		t.Errorf("缺的那条没补上：%q", got)
	}
}
