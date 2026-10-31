package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func mailPageNoticeValues(t *testing.T, sqlText string) map[string]string {
	t.Helper()
	rows := regexp.MustCompile(`(?m)^\('([^']+)', '([^']+)', '((?:[^']|'')*)', 200,`).FindAllStringSubmatch(sqlText, -1)
	if len(rows) != 18 {
		t.Fatalf("期望 18 条词条行（9 key × 2 语言），实际 %d", len(rows))
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row[1]+"|"+row[2]] = strings.ReplaceAll(row[3], "''", "'")
	}
	return values
}

func TestMailPageNoticeSeedAgreement(t *testing.T) {
	t.Parallel()
	seed := findSeedByVersion(t, "614-mail-page-notice-i18n")
	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != 18 {
		t.Fatalf("614 必须恰好 18 个 (key, lang) 组合，实际 %d", len(pairs))
	}

	keys := map[string]int{}
	seen := map[string]bool{}
	for _, pair := range pairs {
		id := pair.key + "|" + pair.lang
		if seen[id] {
			t.Fatalf("614 出现重复组合 %s", id)
		}
		seen[id] = true
		keys[pair.key]++
	}
	if len(keys) != 9 {
		t.Fatalf("614 必须覆盖 9 个 key，实际 %d：%v", len(keys), keys)
	}
	for key, n := range keys {
		if n != 2 {
			t.Errorf("%s 的 (key, lang) 组合数 = %d，期望 2（中英各一条）", key, n)
		}
	}

	conds := regexp.MustCompile(`\('([^']+)', '([^']+)'\)`).FindAllStringSubmatch(seed.ConditionSQL, -1)
	if len(conds) != 18 {
		t.Fatalf("判据里的组合数 = %d，期望 18", len(conds))
	}
	for _, c := range conds {
		if !seen[c[1]+"|"+c[2]] {
			t.Errorf("判据里的组合 %s/%s 在 SQL 里不存在（判据与 SQL 漂移）", c[1], c[2])
		}
	}
	if !strings.Contains(seed.ConditionSQL, "COUNT(*) >= 18") {
		t.Errorf("判据阈值必须与组合数一致（18），实际 %s", seed.ConditionSQL)
	}
	if seed.TableName != "sys_i18n" {
		t.Errorf("614 的表名 = %q，期望 sys_i18n", seed.TableName)
	}
	if !strings.HasSuffix(strings.TrimSpace(seed.SQL), "ON CONFLICT (item_key, lang) DO NOTHING;") {
		t.Fatal("614 必须在冲突时保留运营改过的值")
	}

	want := map[string]int{
		"mail.bulk.allDone|zh-CN":       3,
		"mail.bulk.allSkipped|zh-CN":    3,
		"mail.bulk.partial|zh-CN":       4,
		"mail.msg.automationTick|zh-CN": 1,
		"mail.bulk.allDone|en-US":       3,
		"mail.bulk.allSkipped|en-US":    3,
		"mail.bulk.partial|en-US":       4,
		"mail.msg.automationTick|en-US": 1,
	}
	values := mailPageNoticeValues(t, seed.SQL)
	for id, n := range want {
		got := strings.Count(values[id], "%s")
		if got != n {
			t.Errorf("%s 的 %%s 个数 = %d，期望 %d（与 Go 侧 WithArgs 对不上就会吞掉数字）：%q",
				id, got, n, values[id])
		}
	}
}

func TestMailPageNoticeSeedRunsOnEmptySchema(t *testing.T) {
	seed := findSeedByVersion(t, "614-mail-page-notice-i18n")
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}

	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE mail_page_notice_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE mail_page_notice_seed").Error
	}); err != nil {
		t.Fatalf("PREPARE seed: %v", err)
	}

	keys := []string{
		"mail.bulk.allDone", "mail.bulk.allSkipped", "mail.bulk.partial",
		"mail.msg.automationTick", "mail.page.templateListFailed", "mail.page.contactStatusBad",
		"mail.page.campaignIDRequired", "mail.page.runIDRequired", "mail.page.automationIDRequired",
	}
	for _, k := range keys {
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ?", k)
	}
	if got := guardValue(t, db, seed); got != 0 {
		t.Fatalf("新词条全缺时判据 = %d，期望 0（否则 SQL 被静默跳过）", got)
	}
	mustExec(t, db, seed.SQL)
	if got := guardValue(t, db, seed); got != 1 {
		t.Fatalf("补齐后判据 = %d，期望 1", got)
	}
	for _, k := range keys {
		if got := countRowsFor(t, db, k); got != 2 {
			t.Errorf("%s 落库 %d 行，期望 2", k, got)
		}
	}

	const operatorValue = "Operator edited notice"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?",
		operatorValue, "mail.bulk.partial", "zh-CN")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?",
		"mail.bulk.partial", "en-US")
	if got := guardValue(t, db, seed); got != 0 {
		t.Fatalf("缺一条时判据 = %d，期望 0", got)
	}
	mustExec(t, db, seed.SQL)
	if got := valueOf(t, db, "mail.bulk.partial", "zh-CN"); got != operatorValue {
		t.Errorf("运营改过的值被覆盖：%q", got)
	}
	if got := valueOf(t, db, "mail.bulk.partial", "en-US"); got != mailPageNoticeValues(t, seed.SQL)["mail.bulk.partial|en-US"] {
		t.Errorf("缺的那条没补上：%q", got)
	}
}

func countRowsFor(t *testing.T, db *gorm.DB, key string) int {
	t.Helper()
	var n int64
	if err := db.Connection(func(conn *gorm.DB) error {
		return conn.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", key).Scan(&n).Error
	}); err != nil {
		t.Fatalf("计数 %s 失败: %v", key, err)
	}
	return int(n)
}
