package migrations_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// 598 的门槛判据必须覆盖「中文在、英文缺」的状态。
//
// 缺陷形态（review-sol task-33 的 P2）：判据只数 lang='zh-CN' 的 10 行 → 运营库里任意一条
// en-US 缺失（历史库缺行、有人手工删过英文、或本批 SQL 曾半途失败）时，中文计数已经满足，
// 判据返回 1 → RunSeeds 直接跳过 → 缺的英文**永远补不上**，英文界面回退到组件包内的中文兜底
// （错得安静：没有异常、没有日志）。这与 AGENTS.md §数据库 的定则相悖：
// 「判据的偏差方向要刻意选**宁可重跑，不可静默跳过**」。
//
// 修法：判据枚举本批 10 个 key × 2 种语言 = 20 个 (item_key, lang) 组合，缺任意一项即为 0；
// SQL 是 ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、不覆盖运营改过的值。
//
// 本用例顺带把「这条 seed 真的能跑」钉住：support.NewMigratedPGTestDB 给的是**没有跑过 seed**
// 的新库（表结构由生产迁移建成），第一次 RunSeeds 就是 598 的 SQL 第一回真执行 ——
// AGENTS.md：条件命中就没跑，光看「测试绿」不等于「这条 seed 能跑」。
func TestMailContactWriteUISeedBackfillsMissingLanguage(t *testing.T) {
	const version = "598-mail-contact-write-ui-i18n"
	seed := findSeedByVersion(t, version)

	// ① 判据与 SQL 的组合集合必须一致：判据点名 10 个 key、SQL 落 20 个 (key, lang) 组合，
	//    语言恰好是判据里写死的那两种。任一侧单改（加了 key、加了语言）这里立刻红。
	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != 20 {
		t.Fatalf("598 的 SQL 应有 20 个 (key, lang) 组合，实际 %d 个：%v", len(pairs), pairs)
	}
	langs := map[string]int{}
	keys := map[string]bool{}
	for _, p := range pairs {
		langs[p.lang]++
		keys[p.key] = true
	}
	if langs["zh-CN"] != 10 || langs["en-US"] != 10 || len(langs) != 2 {
		t.Fatalf("598 的 SQL 语言分布应为 zh-CN 10 / en-US 10，实际 %v", langs)
	}
	if len(keys) != 10 {
		t.Fatalf("598 的 SQL 应覆盖 10 个 key，实际 %d 个", len(keys))
	}
	guardKeys := keysMentionedIn(t, seed.ConditionSQL)
	if strings.Join(guardKeys, "|") != strings.Join(sortedKeys(keys), "|") {
		t.Fatalf("判据点名的 key 与 SQL 不一致：\n判据 %v\nSQL  %v", guardKeys, sortedKeys(keys))
	}
	for _, lang := range sortedLangs(langs) {
		if !strings.Contains(seed.ConditionSQL, "'"+lang+"'") {
			t.Fatalf("判据没点名 SQL 里用到的语言 %s：%s", lang, seed.ConditionSQL)
		}
	}
	if strings.Contains(seed.ConditionSQL, "LIKE") {
		t.Fatalf("判据不得用前缀 LIKE（AGENTS.md §数据库）：%s", seed.ConditionSQL)
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}

	// ② 第一次 RunSeeds：新库里 598 的 SQL 真被执行，20 行齐全、判据翻成 1。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds（598 的 SQL 会在这里真执行）：%v", err)
	}
	if got := countPairs(t, db, keys, ""); got != 20 {
		t.Fatalf("首轮后 598 的 20 行应齐全，实际 %d 行", got)
	}
	if applied := guardValue(t, db, seed); applied != 1 {
		t.Fatalf("首轮后判据应为 1（已完成），实际 %d", applied)
	}
	seedEnOfRefresh := valueOf(t, db, "admin.mail.contacts.bulkDelete.refresh", "en-US")
	if seedEnOfRefresh == "" {
		t.Fatal("首轮后 bulkDelete.refresh/en-US 应有值")
	}

	// ③ 把库改造成缺陷形态：中文齐全（其中一条是运营改过值）+ 少一条英文 + 多一条第三语言。
	const operatorValue = "运营改过的中文（不得被 seed 覆盖）"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = 'zh-CN'",
		operatorValue, "admin.mail.contacts.bulk.noneSelected")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'",
		"admin.mail.contacts.bulkDelete.refresh")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
		VALUES (?, 'ja-JP', '運営が入れた日本語', 200, 'admin', 'operator', 1, now(), now())`,
		"admin.mail.contacts.bulkTag.refresh")

	// 缺陷判据（修复前这里是 1 → 静默跳过 → 英文永远补不上）
	if applied := guardValue(t, db, seed); applied != 0 {
		t.Fatalf("中文齐全、英文缺一条时判据必须为 0（否则会静默跳过、缺的英文补不上），实际 %d", applied)
	}

	// ④ 补种：缺的英文回到 seed 原值，运营改过的中文保持，第三语言行原样不动，不产生新语言行。
	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮补种 RunSeeds：%v", round, err)
		}
		if applied := guardValue(t, db, seed); applied != 1 {
			t.Fatalf("第 %d 轮补种后判据应为 1，实际 %d", round, applied)
		}
		if got := valueOf(t, db, "admin.mail.contacts.bulkDelete.refresh", "en-US"); got != seedEnOfRefresh {
			t.Errorf("第 %d 轮后缺的英文应被补回 %q，实际 %q", round, seedEnOfRefresh, got)
		}
		if got := valueOf(t, db, "admin.mail.contacts.bulk.noneSelected", "zh-CN"); got != operatorValue {
			t.Errorf("第 %d 轮后运营改过的中文值被覆盖：实际 %q（ON CONFLICT DO NOTHING 下不该发生）", round, got)
		}
		if got := valueOf(t, db, "admin.mail.contacts.bulkTag.refresh", "ja-JP"); got != "運営が入れた日本語" {
			t.Errorf("第 %d 轮后第三语言行被动了：实际 %q", round, got)
		}
		if got := countPairs(t, db, keys, ""); got != 21 {
			t.Errorf("第 %d 轮后本批应有 21 行（20 + 第三语言 1），实际 %d 行", round, got)
		}
		if got := countPairs(t, db, keys, "ja-JP"); got != 1 {
			t.Errorf("第 %d 轮后第三语言行数应仍为 1，实际 %d（seed 只写中英两种语言）", round, got)
		}
	}
}

type seedPair struct{ key, lang string }

// seedPairs 从 seed 的 SQL 里抽出 (item_key, lang) 组合：只认以 "('key', 'lang'," 开头的
// VALUES 行，注释行（--）与 `ON CONFLICT` 之类的尾巴天然被排除。
func seedPairs(t *testing.T, sqlText string) []seedPair {
	t.Helper()
	var out []seedPair
	for _, line := range strings.Split(sqlText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "('") {
			continue
		}
		parts := strings.Split(line, "', '")
		if len(parts) < 2 {
			t.Fatalf("看不懂的 VALUES 行：%s", line)
		}
		out = append(out, seedPair{key: strings.TrimPrefix(parts[0], "('"), lang: parts[1]})
	}
	if len(out) == 0 {
		t.Fatal("seed 的 SQL 里没有解析出任何 (key, lang) 组合：SQL 形状变了就更新这个解析器")
	}
	return out
}

var quotedKeyRe = regexp.MustCompile(`'(admin\.mail\.[A-Za-z0-9._]+)'`)

// keysMentionedIn 抽出判据 SQL 里点名的 item_key 并排序。
func keysMentionedIn(t *testing.T, sqlText string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range quotedKeyRe.FindAllStringSubmatch(sqlText, -1) {
		seen[m[1]] = true
	}
	return sortedKeys(seen)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedLangs(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for k := range counts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func guardValue(t *testing.T, db *gorm.DB, seed migrations.Seed) int64 {
	t.Helper()
	var applied int64
	// 与迁移器同形（migrator.go 对种子判定就是 db.Raw(ConditionSQL)，不传参）
	if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil {
		t.Fatalf("执行判据失败：%v", err)
	}
	return applied
}

// countPairs 数本批 10 个 key 在库里有多少行；lang 为空表示不筛语言。
func countPairs(t *testing.T, db *gorm.DB, keys map[string]bool, lang string) int {
	t.Helper()
	q := "SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ?"
	args := []interface{}{sortedKeys(keys)}
	if lang != "" {
		q += " AND lang = ?"
		args = append(args, lang)
	}
	var n int
	if err := db.Raw(q, args...).Scan(&n).Error; err != nil {
		t.Fatalf("统计本批行数失败：%v", err)
	}
	return n
}

func valueOf(t *testing.T, db *gorm.DB, key, lang string) string {
	t.Helper()
	var v string
	if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Scan(&v).Error; err != nil {
		t.Fatalf("读取 %s/%s 失败：%v", key, lang, err)
	}
	return v
}

func mustExec(t *testing.T, db *gorm.DB, sqlText string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sqlText, args...).Error; err != nil {
		t.Fatalf("执行 %q 失败：%v", sqlText, err)
	}
}
