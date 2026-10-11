package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// register_mail_page_notice_i18n_test.go — 614 的判据与 SQL 对账。
//
// 这条批次最容易被漏的不是「句子写错」而是「key 根本没进判据」：少枚举一个 (item_key, lang)，
// 那条词条就永远补不上（判据已满足、整段 SQL 被跳过），而且没有任何症状 ——
// 中文环境看不出、enums 对账判据也只看 a.msg.* / a.err.* / a.test.* 三种形态。
// 所以判据要与 SQL 逐条对齐，并且要能在空库上真跑一遍。

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
	// 九个 key，每个两种语言，且互不重复。
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

	// 判据必须逐条枚举这 18 个组合。数字与组合任一不符都判失败 ——
	// 这正是「少枚举一个组合 = 那条词条永远补不上」这道口子。
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

	// 带位置参数的句子，`%s` 的个数必须与 Go 侧 WithArgs 的实参个数一致。
	// 写错个数不报错（adminui.TranslateArgs 只在没有占位符时回落兜底再 Sprintf），
	// 表现是页面上少一个数字 —— 只能在这里钉住。
	want := map[string]int{
		"mail.bulk.allDone|zh-CN":       3, // 动作 / 数量 / 名词
		"mail.bulk.allSkipped|zh-CN":    3, // 名词 / 动作 / 跳过数
		"mail.bulk.partial|zh-CN":       4, // 动作 / 数量 / 名词 / 跳过数
		"mail.msg.automationTick|zh-CN": 1, // 补投数
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

// TestMailPageNoticeSeedRunsOnEmptySchema 在真迁移建出的 schema 上跑一遍：
// 判据在缺行时为 0、补齐后为 1，且运营改过的值不被覆盖。
func TestMailPageNoticeSeedRunsOnEmptySchema(t *testing.T) {
	seed := findSeedByVersion(t, "614-mail-page-notice-i18n")
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	// mustSQL 只校验 embed 文件存在、不校验 SQL 可执行（表/列语义），迁移链一旦中断会堵住其后注册项。
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

	// 运营改过的值不被覆盖；缺的那一条补上。
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
