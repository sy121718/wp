package feature

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
)

const (
	mailI18nFixesSeedVersion = "522-mail-split-i18n-fixes"

	mailCampaignActionSeedVersion = "523-mail-campaign-action-i18n-fix"

	mailI18nFixesContactsKey = "admin.mail.marketing.contacts.empty"
	mailI18nFixesCampaignKey = "admin.mail.campaign.back"
	mailI18nFixesBulkKey     = "admin.mail.automation.bulk_delete_confirm"
	mailCampaignActionKey    = "admin.mail.campaign.empty.action"

	mailI18nFixesOperatorValue = "运营手动改写的文案"
)

type mailI18nExpectation struct {
	key  string
	lang string
	old  string
	new  string
}

func mailI18nFixesTable() []mailI18nExpectation {
	return []mailI18nExpectation{
		{mailI18nFixesContactsKey, "zh-CN",
			"可调整筛选条件，或在下方折叠区批量导入联系人。",
			"可调整筛选条件，或点右上角「导入联系人」批量导入。"},
		{mailI18nFixesContactsKey, "en-US",
			"Adjust the filters, or import contacts in the section below.",
			`Adjust the filters, or use "Import contacts" at the top right to bulk import.`},
		{mailI18nFixesCampaignKey, "zh-CN", "返回营销页", "返回群发活动"},
		{mailI18nFixesCampaignKey, "en-US", "Back to marketing", "Back to campaigns"},
		{mailI18nFixesBulkKey, "zh-CN", "",
			"删除选中的流程？进行中的实例会先停止。"},
		{mailI18nFixesBulkKey, "en-US", "",
			"Delete the selected flows? Running instances are stopped first."},
	}
}

func mailCampaignActionTable() []mailI18nExpectation {
	return []mailI18nExpectation{
		{mailCampaignActionKey, "zh-CN", "回营销页启动群发", "回群发活动页启动"},
		{mailCampaignActionKey, "en-US",
			"Back to marketing to start the campaign",
			"Back to campaigns to start sending"},
	}
}

type mailI18nRow struct {
	ItemKey    string
	Lang       string
	ItemValue  string
	Status     int
	Category   string
	CreateTime time.Time
	UpdateTime time.Time
}

func mailI18nSeed(t *testing.T, version string) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == version {
			return s
		}
	}
	t.Fatalf("种子 %s 未注册到 migrations.AllSeeds()", version)
	return migrations.Seed{}
}

func mailI18nTableKeys(table []mailI18nExpectation) []string {
	seen := map[string]bool{}
	var keys []string
	for _, exp := range table {
		if !seen[exp.key] {
			seen[exp.key] = true
			keys = append(keys, exp.key)
		}
	}
	return keys
}

func mailI18nValue(t *testing.T, db *gorm.DB, key, lang string) (string, bool) {
	t.Helper()
	var value string
	if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).
		Scan(&value).Error; err != nil {
		t.Fatalf("读取词条 %s/%s 失败: %v", key, lang, err)
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计词条 %s/%s 失败: %v", key, lang, err)
	}
	return value, n > 0
}

func mailI18nKeyCount(t *testing.T, db *gorm.DB, key string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", key).Scan(&n).Error; err != nil {
		t.Fatalf("统计键 %s 行数失败: %v", key, err)
	}
	return n
}

func mailI18nKeySnapshot(t *testing.T, db *gorm.DB, table []mailI18nExpectation) []mailI18nRow {
	t.Helper()
	keys := mailI18nTableKeys(table)
	var rows []mailI18nRow
	if err := db.Raw(`SELECT item_key, lang, item_value, status, coalesce(category, '') AS category,
			create_time, update_time
		FROM sys_i18n WHERE item_key IN (?)
		ORDER BY item_key, lang`, keys).Scan(&rows).Error; err != nil {
		t.Fatalf("读取 sys_i18n 快照失败: %v", err)
	}
	return rows
}

func mailI18nDecision(t *testing.T, db *gorm.DB, seed migrations.Seed) int64 {
	t.Helper()
	var got int64
	if err := db.Raw(seed.ConditionSQL).Scan(&got).Error; err != nil {
		t.Fatalf("执行 %s 判定 SQL 失败: %v", seed.Version, err)
	}
	return got
}

func mailI18nReplay(t *testing.T, db *gorm.DB, seed migrations.Seed) {
	t.Helper()
	stmts := migrations.SplitStatements(seed.SQL)
	if len(stmts) == 0 {
		t.Fatalf("%s 的 SQL 拆分后为空：注册内容异常", seed.Version)
	}
	for _, stmt := range stmts {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放 %s 语句失败（幂等性不成立）: %v\nSQL: %s", seed.Version, err, stmt)
		}
	}
}

func mailI18nAssertAllTargets(t *testing.T, db *gorm.DB, table []mailI18nExpectation, when string) {
	t.Helper()
	for _, exp := range table {
		value, found := mailI18nValue(t, db, exp.key, exp.lang)
		if !found {
			t.Errorf("%s：词条 %s/%s 不存在", when, exp.key, exp.lang)
			continue
		}
		if value != exp.new {
			t.Errorf("%s：%s/%s = %q，期望 %q", when, exp.key, exp.lang, value, exp.new)
		}
		if exp.old != "" && value == exp.old {
			t.Errorf("%s：%s/%s 仍是旧值 %q（迁移未生效）", when, exp.key, exp.lang, exp.old)
		}
	}
}

func mailI18nAssertIdempotent(t *testing.T, db *gorm.DB, seed migrations.Seed, table []mailI18nExpectation) {
	t.Helper()

	before := mailI18nKeySnapshot(t, db, table)
	if len(before) != len(table) {
		t.Fatalf("收口后应有 %d 行（%d 键 × 2 语言），实际 %d 行：%+v",
			len(table), len(mailI18nTableKeys(table)), len(before), before)
	}

	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("重跑前 %s 判定 = %d，期望 1", seed.Version, got)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重跑全部种子失败: %v", err)
	}
	if after := mailI18nKeySnapshot(t, db, table); !reflect.DeepEqual(before, after) {
		t.Errorf("重跑全部种子后快照发生变化：\n前 %+v\n后 %+v", before, after)
	}

	mailI18nReplay(t, db, seed)
	if after := mailI18nKeySnapshot(t, db, table); !reflect.DeepEqual(before, after) {
		t.Errorf("重放 %s 后快照发生变化（幂等性不成立）：\n前 %+v\n后 %+v", seed.Version, before, after)
	}
	for _, exp := range table {
		if exp.old != "" {
			continue
		}
		if n := mailI18nKeyCount(t, db, exp.key); n != 2 {
			t.Errorf("重放后 %s 行数 = %d，期望 2（补种语句重复插入）", exp.key, n)
		}
	}
	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Errorf("重放后 %s 判定 = %d，期望仍为 1", seed.Version, got)
	}
	mailI18nAssertAllTargets(t, db, table, "重放后")
}

func mailI18nAssertPartialStatesRejected(t *testing.T, db *gorm.DB, seed migrations.Seed, table []mailI18nExpectation) {
	t.Helper()
	keys := mailI18nTableKeys(table)

	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("完整态判定 = %d，期望 1（后续反例都以它为基线）", got)
	}
	recoverSeed := func(step string) {
		t.Helper()
		mailI18nReplay(t, db, seed)
		if got := mailI18nDecision(t, db, seed); got != 1 {
			t.Fatalf("%s 之后重放 %s 应恢复为完成态，判定 = %d", step, seed.Version, got)
		}
	}
	rollback := func(exp mailI18nExpectation) {
		t.Helper()
		if err := db.Exec("UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?",
			exp.old, exp.key, exp.lang).Error; err != nil {
			t.Fatalf("构造中间态失败: %v", err)
		}
	}

	t.Run("只有一个语言收口", func(t *testing.T) {

		for _, exp := range table {
			if exp.old == "" {
				continue
			}
			rollback(exp)
			if got := mailI18nDecision(t, db, seed); got != 0 {
				t.Errorf("%s/%s 回滚后判定 = %d，期望 0", exp.key, exp.lang, got)
			}
			recoverSeed("单语言回滚")
			return
		}
		t.Fatal("表中没有带旧值的期望，反例无法构造")
	})

	t.Run("只收口了一个键", func(t *testing.T) {
		if len(keys) < 2 {
			t.Skipf("只有一个键，无法构造「另一键未收口」的中间态")
		}

		for _, exp := range table {
			if exp.key == keys[0] {
				continue
			}
			if exp.old == "" {
				if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ?", exp.key).Error; err != nil {
					t.Fatalf("构造中间态失败: %v", err)
				}
				continue
			}
			rollback(exp)
		}
		if got := mailI18nDecision(t, db, seed); got != 0 {
			t.Errorf("只有 %s 收口时判定 = %d，期望 0", keys[0], got)
		}
		recoverSeed("其余键回滚")
	})

	t.Run("补种键缺一个语言", func(t *testing.T) {
		for _, exp := range table {
			if exp.old != "" {
				continue
			}
			if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?",
				exp.key, exp.lang).Error; err != nil {
				t.Fatalf("构造中间态失败: %v", err)
			}
			if got := mailI18nDecision(t, db, seed); got != 0 {
				t.Errorf("补种缺 %s/%s 时判定 = %d，期望 0", exp.key, exp.lang, got)
			}
			recoverSeed("删除补种语言行")
			return
		}
		t.Skip("本迁移没有补种键，无需该反例")
	})

	t.Run("全都回滚", func(t *testing.T) {
		for _, exp := range table {
			if exp.old == "" {
				if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ?", exp.key).Error; err != nil {
					t.Fatalf("构造中间态失败: %v", err)
				}
				continue
			}
			rollback(exp)
		}
		if got := mailI18nDecision(t, db, seed); got != 0 {
			t.Errorf("全部回滚后判定 = %d，期望 0", got)
		}
		recoverSeed("全部回滚")
		mailI18nAssertAllTargets(t, db, table, "自愈后")
	})
}

func mailI18nAssertOperatorValuePreserved(t *testing.T, db *gorm.DB, seed migrations.Seed, table []mailI18nExpectation) {
	t.Helper()

	var target *mailI18nExpectation
	for i := range table {
		if table[i].lang == "zh-CN" {
			target = &table[i]
			break
		}
	}
	if target == nil {
		t.Fatal("表中没有 zh-CN 期望，无法构造运营改写场景")
	}

	if err := db.Exec("UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?",
		mailI18nFixesOperatorValue, target.key, target.lang).Error; err != nil {
		t.Fatalf("模拟运营改写失败: %v", err)
	}

	mailI18nReplay(t, db, seed)
	value, found := mailI18nValue(t, db, target.key, target.lang)
	if !found {
		t.Fatalf("%s/%s 词条丢失", target.key, target.lang)
	}
	if value != mailI18nFixesOperatorValue {
		t.Errorf("运营改写的值被覆盖：%q → %q", mailI18nFixesOperatorValue, value)
	}

	for _, exp := range table {
		if exp.key != target.key || exp.lang == target.lang {
			continue
		}
		if other, ok := mailI18nValue(t, db, exp.key, exp.lang); !ok || other != exp.new {
			t.Errorf("%s/%s = %q（found=%v），期望未被波及的 %q", exp.key, exp.lang, other, ok, exp.new)
		}
	}

	mailI18nReplay(t, db, seed)
	if value, _ := mailI18nValue(t, db, target.key, target.lang); value != mailI18nFixesOperatorValue {
		t.Errorf("重复重放后运营值 = %q，期望 %q", value, mailI18nFixesOperatorValue)
	}
	if got := mailI18nDecision(t, db, seed); got != 0 {
		t.Errorf("运营改写过的那条不满足门槛，判定应保持 0，实际 %d", got)
	}
}

func TestMailSplitI18nFixesApplyExpectedValues(t *testing.T) {
	db := mailSplitDB(t)

	mailSplitApplySeeds(t, db)

	table := mailI18nFixesTable()
	seed := mailI18nSeed(t, mailI18nFixesSeedVersion)
	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("跑完全部种子后 522 判定 = %d，期望 1（说明三处没有同时收口）", got)
	}
	mailI18nAssertAllTargets(t, db, table, "终态")

	if n := mailI18nKeyCount(t, db, mailI18nFixesBulkKey); n != 2 {
		t.Errorf("%s 应有 2 行（zh-CN / en-US），实际 %d", mailI18nFixesBulkKey, n)
	}
}

func TestMailSplitI18nFixesIsIdempotent(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertIdempotent(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

func TestMailSplitI18nFixesConditionRejectsPartialStates(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertPartialStatesRejected(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

func TestMailSplitI18nFixesPreservesOperatorEditedValue(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertOperatorValuePreserved(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

func TestMailCampaignActionI18nFixAppliesExpectedValue(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	table := mailCampaignActionTable()
	seed := mailI18nSeed(t, mailCampaignActionSeedVersion)
	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("跑完全部种子后 523 判定 = %d，期望 1", got)
	}
	mailI18nAssertAllTargets(t, db, table, "终态")

	for _, stale := range []string{"回营销页启动群发", "Back to marketing to start the campaign"} {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND item_value = ?",
			mailCampaignActionKey, stale).Scan(&n).Error; err != nil {
			t.Fatalf("统计旧文案失败: %v", err)
		}
		if n != 0 {
			t.Errorf("旧页名文案 %q 仍留在 sys_i18n（%d 行）", stale, n)
		}
	}
}

func TestMailCampaignActionI18nFixIsIdempotent(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertIdempotent(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}

func TestMailCampaignActionI18nFixConditionRejectsPartialState(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertPartialStatesRejected(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}

func TestMailCampaignActionI18nFixPreservesOperatorEditedValue(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertOperatorValuePreserved(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}

func mailI18nAssertReplayAffectsNoRows(t *testing.T, db *gorm.DB, seed migrations.Seed, table []mailI18nExpectation) {
	t.Helper()

	before := mailI18nKeySnapshot(t, db, table)
	if len(before) == 0 {
		t.Fatal("收口后快照为空：前置不成立")
	}

	stmts := migrations.SplitStatements(seed.SQL)
	if len(stmts) == 0 {
		t.Fatalf("%s 的 SQL 拆分后为空：注册内容异常", seed.Version)
	}
	for _, stmt := range stmts {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		res := db.Exec(stmt)
		if res.Error != nil {
			t.Fatalf("重放 %s 语句失败（幂等性不成立）: %v\nSQL: %s", seed.Version, res.Error, stmt)
		}
		if res.RowsAffected != 0 {
			t.Errorf("收口态下重放 %s 的语句影响了 %d 行，应为 0 行"+
				"（改值必须精确匹配 item_value = old_value，补种必须 ON CONFLICT DO NOTHING）:\nSQL: %s",
				seed.Version, res.RowsAffected, stmt)
		}
	}

	if after := mailI18nKeySnapshot(t, db, table); !reflect.DeepEqual(before, after) {
		t.Errorf("重放 %s 后快照发生变化（含 update_time）:\n前 %+v\n后 %+v", seed.Version, before, after)
	}
}

func TestMailSplitI18nFixesReplayAffectsNoRows(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertReplayAffectsNoRows(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

func TestMailCampaignActionI18nFixReplayAffectsNoRows(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertReplayAffectsNoRows(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}
