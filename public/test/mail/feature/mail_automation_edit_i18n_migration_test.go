package feature

// mail_automation_edit_i18n_migration_test.go — 524 迁移专项（拆页后自动化编辑器新增词条）。
//
// 背景：自动化编辑器步骤化重做后，模板（internal/templates/admin/mail/mail_automation_edit.html）
// 与 internal/module/mail/enums/mail_ui_labels.go 里新增了 admin.mail.automation_edit.* 词条。
// 键一旦不在 sys_i18n 里，取词链（当前语言 → 默认语言 → fallback → key）就落回模板兜底，
// 英文界面于是显示中文；反过来，模板兜底改了、库里值没跟，已安装库上永远显示旧文案。
// 524 把 51 个键 × 2 语言写进 sys_i18n，本文件钉死四件事：
//  1. 落库齐备：SQL 里能解析出的每个 (键, 语言) 元组都必须在库里、值逐字一致、不接受空串或英文列写中文；
//  2. 判定门槛：缺任意一条即为未完成（ConditionSQL 必须报 0），且 RunSeeds 能自愈补回；
//  3. 幂等：连跑两次 RunSeeds、以及绕过判定逐条重放，都不得改动任何一行（含 update_time）；
//  4. 文案一致：模板兜底与 LabelPair 的中文必须与 524 的中文逐字相同 —— 只改一边就是假翻译。
//
// 断言口径的分工（禁止另建第三套）：键是否「被用但没种」由 mail_i18n_key_guard_test.go 与
// scripts/check-i18n-keys-seeded.sh 负责；190 等更早迁移负责的旧键文案归各自的迁移用例，
// 本文件只判 524 这 51 个键的落地、幂等与文案一致性，对旧键只查「库里确有中文值」。

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
)

const (
	// mailAEI18nSeedVersion 524 在注册表里的版本号。
	mailAEI18nSeedVersion = "524-mail-automation-edit-i18n"
	// mailAEI18nKeyPrefix 本迁移负责的键前缀。
	mailAEI18nKeyPrefix = "admin.mail.automation_edit."
	// mailAEI18nTemplatePath 自动化编辑器模板（模板直写的兜底文案来源）。
	mailAEI18nTemplatePath = "../../../../internal/templates/admin/mail/mail_automation_edit.html"
	// mailAEI18nEnumsPath 步骤/节点/单位文案的 LabelPair 来源。
	mailAEI18nEnumsPath = "../../../../internal/module/mail/enums/mail_ui_labels.go"

	mailAEI18nLangZh = "zh-CN"
	mailAEI18nLangEn = "en-US"

	// mailAEI18nMinKeys 解析下限：低于它说明解析口径与文件形态失配，用例会静默空转。
	mailAEI18nMinKeys = 40
)

var (
	mailAEI18nEscapedPrefix = strings.ReplaceAll(mailAEI18nKeyPrefix, ".", `\.`)

	// 524 SQL 的数据元组形态：('admin.mail.automation_edit.x', 'zh-CN', '文案', …
	mailAEI18nSQLValueRe = regexp.MustCompile(`\(\s*'(` + mailAEI18nEscapedPrefix +
		`[A-Za-z0-9_.]+)'\s*,\s*'(zh-CN|en-US)'\s*,\s*'((?:[^']|'')*)'`)
	// 模板与枚举的取词形态：("admin.mail.automation_edit.x", "中文兜底")
	mailAEI18nFallbackRe = regexp.MustCompile(`"(` + mailAEI18nEscapedPrefix + `[A-Za-z0-9_.]+)"\s*,\s*"([^"]*)"`)
	// 任意位置的键字面量（查「用了但库里没有」）。
	mailAEI18nKeyLiteralRe = regexp.MustCompile(`"(` + mailAEI18nEscapedPrefix + `[A-Za-z0-9_.]+)"`)
	// 覆盖式赋值：UPDATE sys_i18n SET item_value = '新值' … WHERE … item_key = '键' …（525 修正 524 的形态）。
	mailAEI18nUpdateStmtRe = regexp.MustCompile(`(?is)UPDATE\s+sys_i18n\s+SET[^;]*?item_value\s*=\s*'((?:[^']|'')*)'` +
		`[^;]*?WHERE[^;]*?item_key\s*=\s*'(` + mailAEI18nEscapedPrefix + `[A-Za-z0-9_.]+)'([^;]*)(?:;|$)`)
	mailAEI18nUpdateLangRe = regexp.MustCompile(`lang\s*=\s*'(zh-CN|en-US)'`)
	// 英文文案里不该出现汉字/假名。刻意不含全角标点区（U+FF00-FFEF）：
	// `＋ Add a step` 里的全角加号是与中文版对齐的排版符号，不是「英文列写了中文」。
	mailAEI18nCJKRe = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}]`)
)

// mailAEI18nCompose 拼合「键 + 语言」查表键。
func mailAEI18nCompose(key, lang string) string { return key + "\x00" + lang }

// mailAEI18nSQLValues 解析迁移 SQL 里的「键 + 语言 → 文案」（SQL 里 ” 是单引号转义）。
func mailAEI18nSQLValues(sqlText string) map[string]string {
	values := make(map[string]string)
	for _, m := range mailAEI18nSQLValueRe.FindAllStringSubmatch(sqlText, -1) {
		values[mailAEI18nCompose(m[1], m[2])] = strings.ReplaceAll(m[3], "''", "'")
	}
	return values
}

// mailAEI18nSQLPairs 把 524 的 SQL 解析成「键 + 语言 + 文案」期望表（old 为空：本迁移全是补种）。
func mailAEI18nSQLPairs(t *testing.T) []mailI18nExpectation {
	t.Helper()

	values := mailAEI18nSQLValues(mailI18nSeed(t, mailAEI18nSeedVersion).SQL)
	if len(values) == 0 {
		t.Fatalf("%s 的 SQL 里没有解析到任何 (键, 语言, 文案) 元组：文件形态变了，本文件会静默空转",
			mailAEI18nSeedVersion)
	}
	pairs := make([]mailI18nExpectation, 0, len(values))
	for composed, value := range values {
		key, lang, _ := strings.Cut(composed, "\x00")
		pairs = append(pairs, mailI18nExpectation{key: key, lang: lang, new: value})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key != pairs[j].key {
			return pairs[i].key < pairs[j].key
		}
		return pairs[i].lang < pairs[j].lang
	})
	return pairs
}

// mailAEI18nSQLZh 524 的中文文案（键 → 文案）。
func mailAEI18nSQLZh(t *testing.T) map[string]string {
	t.Helper()

	zh := make(map[string]string)
	for composed, value := range mailAEI18nSQLValues(mailI18nSeed(t, mailAEI18nSeedVersion).SQL) {
		key, lang, _ := strings.Cut(composed, "\x00")
		if lang == mailAEI18nLangZh {
			zh[key] = value
		}
	}
	if len(zh) == 0 {
		t.Fatalf("%s 的 SQL 里没有解析到中文文案：解析口径失配", mailAEI18nSeedVersion)
	}
	return zh
}

// mailAEI18nGroup 取某前缀下的全部期望（用于「整组缺失」反例）。
func mailAEI18nGroup(pairs []mailI18nExpectation, prefix string) []mailI18nExpectation {
	var group []mailI18nExpectation
	for _, exp := range pairs {
		if strings.HasPrefix(exp.key, prefix) {
			group = append(group, exp)
		}
	}
	return group
}

// mailAEI18nSource 读一个来源文件（模板或 Go 源码）。
func mailAEI18nSource(t *testing.T, path string) string {
	t.Helper()

	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(buf)
}

// mailAEI18nFallbacks 提取「键 + 中文兜底」；同一文件里同键两种兜底即缺陷。
func mailAEI18nFallbacks(t *testing.T, where, src string) map[string]string {
	t.Helper()

	pairs := make(map[string]string)
	for _, m := range mailAEI18nFallbackRe.FindAllStringSubmatch(src, -1) {
		if prev, ok := pairs[m[1]]; ok && prev != m[2] {
			t.Errorf("%s 里同一个键 %s 出现两种兜底文案：%q 与 %q", where, m[1], prev, m[2])
			continue
		}
		pairs[m[1]] = m[2]
	}
	return pairs
}

// mailAEI18nLiterals 提取文本里出现的全部键字面量。
func mailAEI18nLiterals(src string) map[string]bool {
	keys := make(map[string]bool)
	for _, m := range mailAEI18nKeyLiteralRe.FindAllStringSubmatch(src, -1) {
		keys[m[1]] = true
	}
	return keys
}

// mailAEI18nSortedKeys 排序后的键列表（让失败输出稳定可读）。
func mailAEI18nSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// mailAEI18nSortedSet 排序后的键集合。
func mailAEI18nSortedSet(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// mailAEI18nDBZh 库里该前缀的中文词条（键 → 文案）：含 190 等更早迁移种下的旧键。
func mailAEI18nDBZh(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()

	rows, err := db.Raw(`SELECT item_key, item_value FROM sys_i18n
		WHERE item_key LIKE ? AND lang = ? AND status = 1`,
		mailAEI18nKeyPrefix+"%", mailAEI18nLangZh).Rows()
	if err != nil {
		t.Fatalf("读取 sys_i18n 中文词条失败: %v", err)
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatalf("扫描 sys_i18n 行失败: %v", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历 sys_i18n 行失败: %v", err)
	}
	return values
}

// mailAEI18nAssignments 从一段迁移 SQL 里提取「键 + 语言 → 文案」的全部赋值证据：
// INSERT VALUES 元组，以及 UPDATE … SET item_value = … WHERE item_key = … AND lang = …
// （后者是同批「修正前一个迁移写下的值」的形态：新增键走新 Version，改值也在新 Version 里改）。
func mailAEI18nAssignments(sqlText string) map[string]string {
	values := mailAEI18nSQLValues(sqlText)
	for _, m := range mailAEI18nUpdateStmtRe.FindAllStringSubmatch(sqlText, -1) {
		lang := ""
		if lm := mailAEI18nUpdateLangRe.FindStringSubmatch(m[3]); lm != nil {
			lang = lm[1]
		}
		if lang == "" {
			// 语言条件没写在这条语句里：保守跳过，不猜。
			continue
		}
		values[mailAEI18nCompose(m[2], lang)] = strings.ReplaceAll(m[1], "''", "'")
	}
	return values
}

// mailAEI18nExpectedZh 该前缀在整条迁移链上的中文终态（seed 已按 Version 稳定升序，后者覆盖前者）。
// 只判文件与文件之间的一致性，不读库 —— 运营在后台改库值不该让本守卫变红。
func mailAEI18nExpectedZh(t *testing.T) map[string]string {
	t.Helper()

	zh := make(map[string]string)
	for _, seed := range migrations.AllSeeds() {
		for composed, value := range mailAEI18nAssignments(seed.SQL) {
			key, lang, _ := strings.Cut(composed, "\x00")
			if lang != mailAEI18nLangZh || !strings.HasPrefix(key, mailAEI18nKeyPrefix) {
				continue
			}
			zh[key] = value
		}
	}
	if len(zh) == 0 {
		t.Fatalf("整条迁移链里没有解析到任何 %s* 的中文赋值：解析口径失配", mailAEI18nKeyPrefix)
	}
	return zh
}

// mailAEI18nExec 执行一条 SQL（args 为占位参数），失败即终止。
func mailAEI18nExec(t *testing.T, db *gorm.DB, sqlText string, args ...any) {
	t.Helper()

	if err := db.Exec(sqlText, args...).Error; err != nil {
		t.Fatalf("执行 SQL 失败: %v\nSQL: %s", err, sqlText)
	}
}

// TestMailAutomationEditI18nMigrationSeedsEveryKey 524 的每个键语言都落库、值逐字一致。
func TestMailAutomationEditI18nMigrationSeedsEveryKey(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	seed := mailI18nSeed(t, mailAEI18nSeedVersion)
	pairs := mailAEI18nSQLPairs(t)
	keys := mailI18nTableKeys(pairs)

	if len(keys) < mailAEI18nMinKeys {
		t.Fatalf("从 %s 解析出的键只有 %d 个（%v）：文件形态与解析口径失配，本用例会静默空转",
			mailAEI18nSeedVersion, len(keys), keys)
	}
	if len(pairs) != len(keys)*2 {
		t.Errorf("SQL 里的元组数 %d 与「键数 × 2 语言」%d 不符：存在只种了一种语言的键",
			len(pairs), len(keys)*2)
	}

	rows := mailI18nKeySnapshot(t, db, pairs)
	byComposed := make(map[string]mailI18nRow, len(rows))
	for _, row := range rows {
		byComposed[mailAEI18nCompose(row.ItemKey, row.Lang)] = row
	}
	for _, exp := range pairs {
		row, ok := byComposed[mailAEI18nCompose(exp.key, exp.lang)]
		if !ok {
			t.Errorf("键 %s 的 %s 没有落库：英文界面会退回模板兜底（中文）或裸 key", exp.key, exp.lang)
			continue
		}
		if row.ItemValue != exp.new {
			t.Errorf("键 %s(%s) 落库值 = %q，SQL 期望 %q", exp.key, exp.lang, row.ItemValue, exp.new)
		}
		if strings.TrimSpace(row.ItemValue) == "" {
			t.Errorf("键 %s(%s) 落库值为空：等于没种", exp.key, exp.lang)
		}
		if row.Status != 1 {
			t.Errorf("键 %s(%s) 的 status = %d，应为 1（否则前台取词会被过滤）", exp.key, exp.lang, row.Status)
		}
	}

	// 英文列不许是中文照抄。
	for _, exp := range pairs {
		if exp.lang != mailAEI18nLangEn {
			continue
		}
		if mailAEI18nCJKRe.MatchString(exp.new) {
			t.Errorf("键 %s 的英文文案里出现中文：%q", exp.key, exp.new)
		}
	}

	if got := mailI18nDecision(t, db, seed); got != 1 {
		t.Errorf("收口态判定 = %d，应为 1", got)
	}
}

// TestMailAutomationEditI18nConditionRejectsPartialState 判定门槛：缺任意一条即未完成，且 RunSeeds 能自愈。
func TestMailAutomationEditI18nConditionRejectsPartialState(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	seed := mailI18nSeed(t, mailAEI18nSeedVersion)
	pairs := mailAEI18nSQLPairs(t)
	reseed := func(t *testing.T) {
		t.Helper()
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("RunSeeds 补种失败: %v", err)
		}
		if got := mailI18nDecision(t, db, seed); got != 1 {
			t.Fatalf("补种后判定 = %d，应为 1（判定未过时必须重跑 SQL 自愈）", got)
		}
	}

	t.Run("完整态判定为已完成", func(t *testing.T) {
		if got := mailI18nDecision(t, db, seed); got != 1 {
			t.Fatalf("完整态判定 = %d，应为 1", got)
		}
	})

	t.Run("少一条语言即视为未完成并自愈", func(t *testing.T) {
		target := pairs[0]
		mailAEI18nExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", target.key, target.lang)

		if got := mailI18nDecision(t, db, seed); got != 0 {
			t.Errorf("删掉 %s(%s) 后判定 = %d，应为 0：半成品被判成已完成就会永不修复",
				target.key, target.lang, got)
		}
		reseed(t)

		value, ok := mailI18nValue(t, db, target.key, target.lang)
		if !ok || value != target.new {
			t.Errorf("%s(%s) 自愈后 = (%q, 存在=%v)，应为 %q", target.key, target.lang, value, ok, target.new)
		}
	})

	t.Run("整组键缺失即视为未完成并自愈", func(t *testing.T) {
		group := mailAEI18nGroup(pairs, mailAEI18nKeyPrefix+"err.")
		if len(group) == 0 {
			t.Fatal("没有解析到 err.* 组的键：解析口径异常")
		}
		for _, exp := range group {
			mailAEI18nExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", exp.key, exp.lang)
		}

		if got := mailI18nDecision(t, db, seed); got != 0 {
			t.Errorf("删掉整组 %d 条（err.*）后判定 = %d，应为 0", len(group), got)
		}
		reseed(t)

		for _, exp := range group {
			value, ok := mailI18nValue(t, db, exp.key, exp.lang)
			if !ok || value != exp.new {
				t.Errorf("%s(%s) 自愈后 = (%q, 存在=%v)，应为 %q", exp.key, exp.lang, value, ok, exp.new)
			}
		}
	})

	t.Run("列表外的键不参与判定", func(t *testing.T) {
		// 反证门槛不是「全库行数」：先删掉 524 的一整组键，再插入等量（行数守恒）的
		// 列表外键 —— 全库计数口径会判成已完成（漏判），IN 列表口径必须判 0。
		group := mailAEI18nGroup(pairs, mailAEI18nKeyPrefix+"err.")
		if len(group) == 0 {
			t.Fatal("没有解析到 err.* 组的键：解析口径异常")
		}

		probes := make([]string, 0, len(group))
		for i := 0; i < len(group); i++ {
			probes = append(probes, mailAEI18nKeyPrefix+"probe_not_registered_"+strconv.Itoa(i))
		}
		t.Cleanup(func() {
			for _, probe := range probes {
				_ = db.Exec("DELETE FROM sys_i18n WHERE item_key = ?", probe).Error
			}
		})

		for _, exp := range group {
			mailAEI18nExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", exp.key, exp.lang)
		}
		for _, probe := range probes {
			for _, lang := range []string{mailAEI18nLangZh, mailAEI18nLangEn} {
				value := "probe entry"
				if lang == mailAEI18nLangZh {
					value = "探针词条"
				}
				mailAEI18nExec(t, db, `INSERT INTO sys_i18n
					(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
					VALUES (?, ?, ?, 200, 'admin', 'probe', 1, now(), now())`,
					probe, lang, value)
			}
		}

		if got := mailI18nDecision(t, db, seed); got != 0 {
			t.Errorf("删掉 %d 条约 524 键、插入等量列表外键后判定 = %d，应为 0 —— "+
				"门槛若按全库行数判定，存量库会永久跳过本迁移", len(group), got)
		}

		for _, probe := range probes {
			mailAEI18nExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ?", probe)
		}
		reseed(t)
	})

	if strings.Contains(seed.ConditionSQL, "?") || strings.Contains(seed.ConditionSQL, "$1") {
		t.Errorf("ConditionSQL 含参数占位符：它由 db.Raw 直接执行、不做参数替换，会被当字面量而永远判定失败")
	}
}

// TestMailAutomationEditI18nMigrationIsIdempotent 524 连跑两次、以及逐条重放都不得写入任何一行。
func TestMailAutomationEditI18nMigrationIsIdempotent(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	seed := mailI18nSeed(t, mailAEI18nSeedVersion)
	pairs := mailAEI18nSQLPairs(t)

	before := mailI18nKeySnapshot(t, db, pairs)
	if len(before) == 0 {
		t.Fatal("收口后 sys_i18n 快照为空：前置不成立")
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("第二次 RunSeeds 失败: %v", err)
	}
	after := mailI18nKeySnapshot(t, db, pairs)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("第二次 RunSeeds 改动了 sys_i18n（含 update_time）：\n前 %+v\n后 %+v", before, after)
	}

	// 绕过判定逐条重放：补种必须带 ON CONFLICT DO NOTHING，否则会刷新 update_time。
	mailI18nAssertReplayAffectsNoRows(t, db, seed, pairs)
}

// TestMailAutomationEditI18nTextMatchesSources 524 的中文必须与模板兜底、LabelPair 逐字一致。
// 这是本轮反复出现的缺陷形态：模板改了、库里没跟（437 → 522 → 523），必须自动化挡住。
func TestMailAutomationEditI18nTextMatchesSources(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	seedZh := mailAEI18nSQLZh(t)  // 524 负责的键 → 中文
	dbZh := mailAEI18nDBZh(t, db) // 库里该前缀的全部中文（含 190 等更早迁移）

	tplSrc := mailGuardStripComments(mailAEI18nSource(t, mailAEI18nTemplatePath))
	enumSrc := mailAEI18nSource(t, mailAEI18nEnumsPath)
	tpl := mailAEI18nFallbacks(t, "模板 "+mailAEI18nTemplatePath, tplSrc)
	enum := mailAEI18nFallbacks(t, "枚举 "+mailAEI18nEnumsPath, enumSrc)

	if len(tpl) == 0 {
		t.Fatalf("%s 里没有解析到任何 %s* 兜底：文件形态变了，本用例会静默空转",
			mailAEI18nTemplatePath, mailAEI18nKeyPrefix)
	}
	if len(enum) == 0 {
		t.Fatalf("%s 里没有解析到任何 LabelPair 文案：文件形态变了，本用例会静默空转", mailAEI18nEnumsPath)
	}

	used := make(map[string]bool)
	for _, src := range []struct {
		where    string
		fallback map[string]string
		literals map[string]bool
	}{
		{"模板", tpl, mailAEI18nLiterals(tplSrc)},
		{"枚举", enum, mailAEI18nLiterals(enumSrc)},
	} {
		for _, key := range mailAEI18nSortedSet(src.literals) {
			used[key] = true

			value, ok := dbZh[key]
			if !ok || strings.TrimSpace(value) == "" {
				t.Errorf("%s 里的键 %s 在 sys_i18n 里没有中文值：英文站会退回模板兜底（中文）或裸 key",
					src.where, key)
				continue
			}

			want, owned := seedZh[key]
			if !owned {
				// 190 等更早迁移负责的旧键：只判「库里确有中文值」，文案归各自的迁移用例。
				continue
			}
			if value != want {
				t.Errorf("键 %s 的库内中文 %q 与 %s 的 %q 不一致：运行期取的是库值，只改一侧就是假收口",
					key, value, mailAEI18nSeedVersion, want)
			}
			if fallback, hasFallback := src.fallback[key]; hasFallback && fallback != want {
				t.Errorf("%s 里键 %s 的兜底 %q 与 %s 的中文 %q 不一致：只改一边就是假翻译",
					src.where, key, fallback, mailAEI18nSeedVersion, want)
			}
		}
	}

	// 模板里出现却没带兜底的键：链式取词（{{tr := .["t"]}} 后 tr("k")）缺词条时静默输出空串。
	for _, key := range mailAEI18nSortedSet(mailAEI18nLiterals(tplSrc)) {
		if _, ok := tpl[key]; !ok {
			t.Errorf("模板里的键 %s 没有中文兜底：缺词条时会静默输出空串，既不 500 也无日志", key)
		}
	}

	// 524 种了、但模板与枚举里都找不到的键：多半是旧表格键没删干净，会留下永不显示的词条。
	enumLiterals := mailAEI18nLiterals(enumSrc)
	for _, key := range mailAEI18nSortedKeys(seedZh) {
		if !used[key] || (!mailAEI18nLiterals(tplSrc)[key] && !enumLiterals[key]) {
			t.Errorf("524 种了 %s，但模板与枚举里都没有这个键：会留下永不显示的词条", key)
		}
	}
}

// TestMailAutomationEditI18nGuardIsNotVacuous 守卫自检：解析器对坏样本必须报出问题。
func TestMailAutomationEditI18nGuardIsNotVacuous(t *testing.T) {
	t.Run("SQL 元组解析含转义单引号", func(t *testing.T) {
		sample := `INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
			('admin.mail.automation_edit.demo', 'zh-CN', '它''s 文案', 200, 'admin'),` + "\n" + `
			('admin.mail.automation_edit.demo', 'en-US', 'plain', 200, 'admin');`
		values := mailAEI18nSQLValues(sample)
		if len(values) != 2 {
			t.Fatalf("坏样本解析出 %d 条，应为 2 条", len(values))
		}
		if got := values[mailAEI18nCompose("admin.mail.automation_edit.demo", mailAEI18nLangZh)]; got != "它's 文案" {
			t.Errorf("转义单引号未复原：得到 %q", got)
		}
	})

	t.Run("模板取词支持跨行与无兜底两种形态", func(t *testing.T) {
		spanned := "{{ .[\"t\"](\n  \"admin.mail.automation_edit.demo\",\n  \"兜底文案\"\n) }}"
		if got := mailAEI18nFallbacks(t, "样本", spanned); got["admin.mail.automation_edit.demo"] != "兜底文案" {
			t.Errorf("跨行取词没有解析出兜底：%v", got)
		}
		bare := `{{ .["t"]("admin.mail.automation_edit.demo") }}`
		if got := mailAEI18nFallbacks(t, "样本", bare); len(got) != 0 {
			t.Errorf("无兜底取词被当成了有兜底：%v", got)
		}
		if !mailAEI18nLiterals(bare)["admin.mail.automation_edit.demo"] {
			t.Error("键字面量收集失效：无兜底取词的键也应被收集")
		}
	})

	t.Run("英文中文检测器可用", func(t *testing.T) {
		if !mailAEI18nCJKRe.MatchString("中文文案") {
			t.Error("中文检测器漏报：英文列写中文应被抓到")
		}
		if mailAEI18nCJKRe.MatchString("Add step, e.g. 2 hours") {
			t.Error("中文检测器误报：纯英文被当成中文")
		}
		// 全角符号是刻意对齐的排版，不算「英文列写了中文」（524 的 add_step 就是 `＋ Add a step`）。
		if mailAEI18nCJKRe.MatchString("＋ Add a step") {
			t.Error("中文检测器误报：全角加号被当成了汉字")
		}
	})
}
