package feature

// mail_split_i18n_fixes_migration_test.go — 拆页后 i18n 收口迁移（522 / 523）的落地结果、
// 幂等性与判定门槛（回归）。
//
// 为什么需要它：521 把「邮件营销」拆成四页，但**词条不会跟着页面走**。
// 键一旦在 sys_i18n 里存在，`.["t"](key, fallback)` 的取值链
// （当前语言 → 默认语言 → fallback → key，见 pkg/i18n/snapshot.go:39-57）
// 就永远落不到 fallback —— 只改模板兜底文案在已安装库上不生效。
// 本轮已经三次出现「模板 fallback 改了、DB 值没跟」（437 → 522 → 523），
// 所以拆页后的文案修正必须走迁移，而且必须有回归钉住。
//
// 为什么 522 与 523 放在同一个文件：
//   - 两者是同一类、同一批（523 是 522 漏改的同页空态动作），断言结构逐条同构；
//   - 523 另开一个 Version 而不是改 522，是因为 seed 按 Version 记执行历史，
//     跑过的 Version 不再重跑 —— 改 522 的 SQL 对已安装库无效。这条约束本身
//     也需要被测试记录（见 523 的幂等用例）。
//
// 三条容易做错、这里逐条钉死：
//  1. 终态：每条语句的效果在每语言上各自就位（两语言缺一即为半成品）；
//  2. 幂等：迁移器逐语句执行、不包事务 —— 绕过 ConditionSQL 直接重放整段 SQL，
//     必须不改写已收口的行（连 update_time 都不刷新）、不重复插补种行；
//  3. 判定门槛：ConditionSQL 以「全部收口」为准，只做了一部分必须返回 0 ——
//     单条判定会让半成品被误判成已完成而永不修复（224 / 521 的教训）。
//
// 另有一条单独钉「不覆盖运营手改」：437 / 522 / 523 都用 item_value = old_value 精确匹配，
// 运营在后台改过的文案必须原样留着。

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
)

const (
	// mailI18nFixesSeedVersion 522 在注册表里的版本号（联系人空态文案 + 活动返回按钮 + 流程页补种）。
	mailI18nFixesSeedVersion = "522-mail-split-i18n-fixes"
	// mailCampaignActionSeedVersion 523 在注册表里的版本号（活动报表页空态动作的旧页名）。
	mailCampaignActionSeedVersion = "523-mail-campaign-action-i18n-fix"

	mailI18nFixesContactsKey = "admin.mail.marketing.contacts.empty"
	mailI18nFixesCampaignKey = "admin.mail.campaign.back"
	mailI18nFixesBulkKey     = "admin.mail.automation.bulk_delete_confirm"
	mailCampaignActionKey    = "admin.mail.campaign.empty.action"

	// mailI18nFixesOperatorValue 模拟运营在后台手改的文案。
	mailI18nFixesOperatorValue = "运营手动改写的文案"
)

// mailI18nExpectation 一条键语言的「旧值 → 新值」期望。old 为空表示迁移之前该键不存在（补种）。
type mailI18nExpectation struct {
	key  string
	lang string
	old  string
	new  string
}

// mailI18nFixesTable 522 的全部期望（与 522_mail_split_i18n_fixes.sql 逐条对应）。
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

// mailCampaignActionTable 523 的全部期望（与 523_mail_campaign_action_i18n_fix.sql 逐条对应）。
// 旧值来自 415_i18n_empty_actions.sql:39-40；模板 fallback 在 mail_campaign.html:87 与 :118。
func mailCampaignActionTable() []mailI18nExpectation {
	return []mailI18nExpectation{
		{mailCampaignActionKey, "zh-CN", "回营销页启动群发", "回群发活动页启动"},
		{mailCampaignActionKey, "en-US",
			"Back to marketing to start the campaign",
			"Back to campaigns to start sending"},
	}
}

// mailI18nRow sys_i18n 参与者（本文件断言用到的列）。
type mailI18nRow struct {
	ItemKey    string
	Lang       string
	ItemValue  string
	Status     int
	Category   string
	CreateTime time.Time
	UpdateTime time.Time
}

// mailI18nSeed 按版本号取种子定义；未注册即失败。
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

// mailI18nTableKeys 取表里出现过的键（去重、按首次出现顺序）。
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

// mailI18nValue 查一条词条的值；不存在返回 ("", false)。
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

// mailI18nKeyCount 某键的行数（补种语句重放时不许重复插，所以数行数）。
func mailI18nKeyCount(t *testing.T, db *gorm.DB, key string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", key).Scan(&n).Error; err != nil {
		t.Fatalf("统计键 %s 行数失败: %v", key, err)
	}
	return n
}

// mailI18nKeySnapshot 本表涉及键的全字段快照（含 update_time）：
// 重放不许改动任何一列 —— 已收口的行连 update_time 都不该被刷新。
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

// mailI18nDecision 执行某迁移的 ConditionSQL，返回判定值（1 = 已完成、跳过）。
func mailI18nDecision(t *testing.T, db *gorm.DB, seed migrations.Seed) int64 {
	t.Helper()
	var got int64
	if err := db.Raw(seed.ConditionSQL).Scan(&got).Error; err != nil {
		t.Fatalf("执行 %s 判定 SQL 失败: %v", seed.Version, err)
	}
	return got
}

// mailI18nReplay 绕过 ConditionSQL 直接重放某迁移的 SQL（逐条执行，失败即定位到语句）。
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

// mailI18nAssertAllTargets 断言表里每条期望都落在新值上。
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

// mailI18nAssertIdempotent 幂等两条路径：① 再跑一次全部种子；② 绕过 ConditionSQL 重放该迁移。
// 两条都必须让涉及键的全字段快照逐字段不变（含 update_time），补种行不增。
func mailI18nAssertIdempotent(t *testing.T, db *gorm.DB, seed migrations.Seed, table []mailI18nExpectation) {
	t.Helper()

	before := mailI18nKeySnapshot(t, db, table)
	if len(before) != len(table) {
		t.Fatalf("收口后应有 %d 行（%d 键 × 2 语言），实际 %d 行：%+v",
			len(table), len(mailI18nTableKeys(table)), len(before), before)
	}

	// ① 条件判定路径：判定为 1 时种子被跳过，库不该有任何变化。
	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("重跑前 %s 判定 = %d，期望 1", seed.Version, got)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重跑全部种子失败: %v", err)
	}
	if after := mailI18nKeySnapshot(t, db, table); !reflect.DeepEqual(before, after) {
		t.Errorf("重跑全部种子后快照发生变化：\n前 %+v\n后 %+v", before, after)
	}

	// ② 真实重跑路径：判定门槛失效或人工重放时走的就是这里 ——
	//    改值的 UPDATE 必须因 item_value = old_value 不再匹配而 0 行受影响，
	//    补种的 INSERT 必须 ON CONFLICT DO NOTHING。
	mailI18nReplay(t, db, seed)
	if after := mailI18nKeySnapshot(t, db, table); !reflect.DeepEqual(before, after) {
		t.Errorf("重放 %s 后快照发生变化（幂等性不成立）：\n前 %+v\n后 %+v", seed.Version, before, after)
	}
	for _, exp := range table {
		if exp.old != "" {
			continue // 只有补种的键需要防重复插
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

// mailI18nAssertPartialStatesRejected 判定门槛反例：只做了一部分必须返回 0，
// 否则半成品会被永久误判成已完成。每个反例做完立刻重放自愈，避免反例互相污染。
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
		// 取表里第一条有旧值的期望：把它自己回滚掉，另一语言仍是新值。
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
		// 把第二个键的所有语言全部回滚，第一个键保持收口。
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
				continue // 只针对「迁移之前不存在」的补种键
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

// mailI18nAssertOperatorValuePreserved 「不覆盖运营手改」：精确匹配 old_value 的意义就在这里。
//
// 顺带记录一个必然结果：判定门槛要求「值等于目标新值」，因此被运营改过的那条永远不满足
// → 判定保持 0 → 每次启动都会重跑这段 SQL。它没有副作用（UPDATE 0 行 + ON CONFLICT DO NOTHING），
// 但必须证明「重跑不改动运营值」。
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

	// 同键的另一语言没被动过，仍应保持收口后的值。
	for _, exp := range table {
		if exp.key != target.key || exp.lang == target.lang {
			continue
		}
		if other, ok := mailI18nValue(t, db, exp.key, exp.lang); !ok || other != exp.new {
			t.Errorf("%s/%s = %q（found=%v），期望未被波及的 %q", exp.key, exp.lang, other, ok, exp.new)
		}
	}

	// 再次重放：运营值依旧不动（幂等 + 不覆盖）。
	mailI18nReplay(t, db, seed)
	if value, _ := mailI18nValue(t, db, target.key, target.lang); value != mailI18nFixesOperatorValue {
		t.Errorf("重复重放后运营值 = %q，期望 %q", value, mailI18nFixesOperatorValue)
	}
	if got := mailI18nDecision(t, db, seed); got != 0 {
		t.Errorf("运营改写过的那条不满足门槛，判定应保持 0，实际 %d", got)
	}
}

// TestMailSplitI18nFixesApplyExpectedValues 522 应用后的终态：
// 判定门槛为「已完成」，六条语句的效果逐条就位。
func TestMailSplitI18nFixesApplyExpectedValues(t *testing.T) {
	db := mailSplitDB(t)
	// 全部种子（190 种初值 → 437 改一次 → 522 收口），收敛到生产终态。
	mailSplitApplySeeds(t, db)

	table := mailI18nFixesTable()
	seed := mailI18nSeed(t, mailI18nFixesSeedVersion)
	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("跑完全部种子后 522 判定 = %d，期望 1（说明三处没有同时收口）", got)
	}
	mailI18nAssertAllTargets(t, db, table, "终态")

	// 补种的那条必须两语言各一行（重复插会让同一键出现两份词条）。
	if n := mailI18nKeyCount(t, db, mailI18nFixesBulkKey); n != 2 {
		t.Errorf("%s 应有 2 行（zh-CN / en-US），实际 %d", mailI18nFixesBulkKey, n)
	}
}

// TestMailSplitI18nFixesIsIdempotent 522 的幂等两条路径。
func TestMailSplitI18nFixesIsIdempotent(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertIdempotent(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

// TestMailSplitI18nFixesConditionRejectsPartialStates 522 的判定门槛反例。
func TestMailSplitI18nFixesConditionRejectsPartialStates(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertPartialStatesRejected(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

// TestMailSplitI18nFixesPreservesOperatorEditedValue 522 不覆盖运营手改。
func TestMailSplitI18nFixesPreservesOperatorEditedValue(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertOperatorValuePreserved(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

// TestMailCampaignActionI18nFixAppliesExpectedValue 523 应用后的终态：
// 活动报表页空态动作的两语言都必须指向「群发活动」，判定门槛为已完成。
func TestMailCampaignActionI18nFixAppliesExpectedValue(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	table := mailCampaignActionTable()
	seed := mailI18nSeed(t, mailCampaignActionSeedVersion)
	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Fatalf("跑完全部种子后 523 判定 = %d，期望 1", got)
	}
	mailI18nAssertAllTargets(t, db, table, "终态")

	// 旧页名不许留在库里：这两条旧文案是「营销页」被 521 拆散后必然失效的残留。
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

// TestMailCampaignActionI18nFixIsIdempotent 523 的幂等两条路径。
func TestMailCampaignActionI18nFixIsIdempotent(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertIdempotent(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}

// TestMailCampaignActionI18nFixConditionRejectsPartialState 523 的判定门槛反例：
// 只改了 zh-CN 必须返回 0。
func TestMailCampaignActionI18nFixConditionRejectsPartialState(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertPartialStatesRejected(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}

// TestMailCampaignActionI18nFixPreservesOperatorEditedValue 523 不覆盖运营手改
// （old_value 不匹配时不得改写）。
func TestMailCampaignActionI18nFixPreservesOperatorEditedValue(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertOperatorValuePreserved(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}

// mailI18nAssertReplayAffectsNoRows 幂等性的**直接**证据（与 521 的
// TestMailSplitMenuReplayAffectsNoRows 同一套思路）：收口态下逐语句重放，
// 每条必须 0 行受影响，且含 update_time 的快照逐行不变。
//
// 只看「值没变」会漏掉两类写入：改值语句命中了一个恰好同值的行、补种语句走到了
// 会刷新行的分支 —— 它们都保持值幂等，却每次重放都刷 update_time（下游按
// update_time 做增量失效时会反复失效），也是「WHERE 守卫被放宽」的唯一可观测信号。
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

// TestMailSplitI18nFixesReplayAffectsNoRows 522 重放零写入。
func TestMailSplitI18nFixesReplayAffectsNoRows(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertReplayAffectsNoRows(t, db, mailI18nSeed(t, mailI18nFixesSeedVersion), mailI18nFixesTable())
}

// TestMailCampaignActionI18nFixReplayAffectsNoRows 523 重放零写入。
func TestMailCampaignActionI18nFixReplayAffectsNoRows(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)
	mailI18nAssertReplayAffectsNoRows(t, db, mailI18nSeed(t, mailCampaignActionSeedVersion), mailCampaignActionTable())
}
