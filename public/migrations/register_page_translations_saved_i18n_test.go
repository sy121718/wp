package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// register_page_translations_saved_i18n_test.go — 613 的判据与 SQL 对账（对照 607 的写法）。
//
// 613 与其它 i18n 批次的区别是**它还删行**：只核对「新两条在不在」不够 ——
// 判据漏枚举任何一个 (item_key, lang)，那条就永远补不上（判据已满足、整段 SQL 被跳过），
// 而 DELETE 也会一起停在未执行的状态，于是表里同时留着新的两条与旧的三条。
// 两个方向都要钉：新增的 4 个组合齐全、旧三段被删掉。

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

	// 两条整句都必须带 `%s`：不带占位符时 tf 把参数丢掉，页面上就是「已保存 条译文。」
	// 而这不报错（adminui.TranslateArgs 只在没有占位符时回落 fallback 后再 Sprintf，
	// 参数被静默丢弃）。句值里没有 `%s` 属于文案错误，只能在这里被拦住。
	for id, value := range want {
		if !strings.Contains(value, "%s") {
			t.Errorf("%s 的句值缺少 %%s 占位符：%q", id, value)
		}
	}

	// 旧三段必须在本批被删掉：留着就是两份真源。断言的是 SQL 文本里出现了针对它们
	// 三条 key 的 DELETE（真实删行由下面的 PREPARE 用例覆盖）。
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
	// DELETE 必须排在 INSERT 之前：反过来的话「本次 INSERT 的行」会被同一条 DELETE 带走，
	// 而 ON CONFLICT 也救不回来（行已不存在）—— 症状是「跑完迁移词条还是空的」。
	if strings.Index(seed.SQL, "DELETE FROM sys_i18n") > strings.Index(seed.SQL, "INSERT INTO sys_i18n") {
		t.Error("613 的 DELETE 必须在 INSERT 之前")
	}

	// 判据只数新增的 4 个组合（不把被删的旧三条纳进来 —— 纳进来会让条件永不成立、
	// 每次启动都重跑一遍 DELETE）。
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

// TestPageTranslationsSavedSeedRunsOnEmptySchema 走一遍真执行：旧三条被删、新两条落库、判据翻转。
//
// 用生产迁移搭出来的 schema（support.NewMigratedPGTestDB），且先 PREPARE 一次 ——
// mustSQL 只校验 embed 文件存在、不校验 SQL 可执行，而迁移链一旦中断会堵住其后全部注册项。
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
	// 先摆出「192 已跑过」的现场：旧三条在库，新两条不在 —— 判据为 0，SQL 会真跑。
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
	// 旧三段必须消失：这是本批与其它 i18n 批次唯一不同的地方。
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

	// 运营改过的值不被覆盖：ON CONFLICT DO NOTHING 的语义在这里被钉住。
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
