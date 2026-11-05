package migrations_test

import (
	"regexp"
	"testing"

	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestWorkbenchActionTargetAvailabilitySeedPreparesAndReplays(t *testing.T) {
	seed := findSeedByVersion(t, "695a-workbench-action-target-availability-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE workbench_action_target_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE workbench_action_target_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Exec(seed.SQL).Error; err != nil {
			t.Fatal(err)
		}
	}
	var ready int
	if err := db.Raw(seed.ConditionSQL).Scan(&ready).Error; err != nil || ready != 1 {
		t.Fatalf("动作目标六条双语词条不完整: %d %v", ready, err)
	}
	var value string
	if err := db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", workbenchenums.ErrPreviewActionTargetUnavailable, "zh-CN").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	// 声明处只给 key + 状态码，成品句归词库：期望值直接从本批 seed 的 SQL 里取那一行
	// （同一个 key 的 zh-CN），与库里读回的值做等式断言。跨包无法复用 workbench 域的
	// 读取器，所以在这里按同一形状写一个只认本文件的窄读取器。
	want := seededItemValue(t, seed.SQL, workbenchenums.ErrPreviewActionTargetUnavailable, "zh-CN")
	if value != want {
		t.Fatalf("重选提示与 seed 词条不同: got=%q want=%q", value, want)
	}
	if value == "" || value == workbenchenums.ErrPreviewActionTargetUnavailable {
		t.Fatalf("词条不能是空串或裸 key: %q", value)
	}
}

// seededItemValue 从 seed SQL 里取某个 (item_key, lang) 的 item_value。
// 只服务本文件：按 VALUES 元组切成 (key, lang, value) 三段，取精确匹配的那一行，
// 取不到就判失败 —— 词条不存在时不能静默通过。
func seededItemValue(t *testing.T, sql, key, lang string) string {
	t.Helper()
	re := regexp.MustCompile(`\('([^']+)',\s*'(zh-CN|en-US)',\s*'([^']*)'`)
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		if m[1] == key && m[2] == lang {
			return m[3]
		}
	}
	t.Fatalf("seed 里没有 %s/%s 的词条", key, lang)
	return ""
}
