package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestAdminOrdersWriteUISeedBackfillsMissingLanguage(t *testing.T) {
	const version = "604-admin-orders-write-ui-i18n"
	seed := findSeedByVersion(t, version)

	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != 20 {
		t.Fatalf("604 的 SQL 应有 20 个 (key, lang) 组合，实际 %d 个：%v", len(pairs), pairs)
	}
	langs := map[string]int{}
	keys := map[string]bool{}
	for _, p := range pairs {
		langs[p.lang]++
		keys[p.key] = true
	}
	if langs["zh-CN"] != 10 || langs["en-US"] != 10 || len(langs) != 2 {
		t.Fatalf("604 的 SQL 语言分布应为 zh-CN 10 / en-US 10，实际 %v", langs)
	}
	if len(keys) != 10 {
		t.Fatalf("604 的 SQL 应覆盖 10 个 key，实际 %d 个", len(keys))
	}

	guardKeys := guardSeedKeys(seed.ConditionSQL)
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

	wantPlaceholders := map[string][]string{
		"admin.orders.bulkStatus.allUpdated": {"{count}"},
		"admin.orders.bulkStatus.allSkipped": {"{count}"},
		"admin.orders.bulkStatus.partial":    {"{updated}", "{skipped}"},
		"admin.orders.bulkCancel.allUpdated": {"{count}"},
		"admin.orders.bulkCancel.allSkipped": {"{count}"},
		"admin.orders.bulkCancel.partial":    {"{updated}", "{skipped}"},
		"admin.orders.bulk.noneSelected":     nil,
		"admin.orders.bulk.refresh":          nil,
		"admin.orders.bulkStatusConfirm":     nil,
		"admin.orders.write.noJs":            nil,
	}
	if len(wantPlaceholders) != 10 {
		t.Fatalf("断言表本身应覆盖本批 10 个 key，实际 %d 个", len(wantPlaceholders))
	}
	values := seedValues(t, seed.SQL)
	for _, p := range pairs {
		want, tracked := wantPlaceholders[p.key]
		if !tracked {
			t.Fatalf("SQL 里出现了断言表没登记的新 key %q（新词条必须同一批登记判据与占位符口径）", p.key)
		}
		value, ok := values[p.key+"|"+p.lang]
		if !ok {
			t.Fatalf("解析不出 %s/%s 的 item_value", p.key, p.lang)
		}
		if strings.Contains(value, "%s") {
			t.Errorf("%s/%s 不得使用 %%s 位置占位符（基座只认命名占位符）：%q", p.key, p.lang, value)
		}
		for _, ph := range want {
			if !strings.Contains(value, ph) {
				t.Errorf("%s/%s 缺少占位符 %s：%q", p.key, p.lang, ph, value)
			}
		}
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds（604 的 SQL 会在这里真执行）：%v", err)
	}
	if got := countPairs(t, db, keys, ""); got != 20 {
		t.Fatalf("首轮后 604 的 20 行应齐全，实际 %d 行", got)
	}
	if applied := guardValue(t, db, seed); applied != 1 {
		t.Fatalf("首轮后判据应为 1（已完成），实际 %d", applied)
	}
	seedEnOfRefresh := valueOf(t, db, "admin.orders.bulk.refresh", "en-US")
	if seedEnOfRefresh == "" {
		t.Fatal("首轮后 bulk.refresh/en-US 应有值")
	}

	const operatorValue = "运营改过的中文（不得被 seed 覆盖）"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = 'zh-CN'",
		operatorValue, "admin.orders.bulk.noneSelected")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'",
		"admin.orders.bulk.refresh")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
		VALUES (?, 'ja-JP', '運営が入れた日本語', 200, 'admin', 'operator', 1, now(), now())`,
		"admin.orders.bulkStatusConfirm")

	if applied := guardValue(t, db, seed); applied != 0 {
		t.Fatalf("中文齐全、英文缺一条时判据必须为 0（否则会静默跳过、缺的英文补不上），实际 %d", applied)
	}

	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮补种 RunSeeds：%v", round, err)
		}
		if applied := guardValue(t, db, seed); applied != 1 {
			t.Fatalf("第 %d 轮补种后判据应为 1，实际 %d", round, applied)
		}
		if got := valueOf(t, db, "admin.orders.bulk.refresh", "en-US"); got != seedEnOfRefresh {
			t.Errorf("第 %d 轮后缺的英文应被补回 %q，实际 %q", round, seedEnOfRefresh, got)
		}
		if got := valueOf(t, db, "admin.orders.bulk.noneSelected", "zh-CN"); got != operatorValue {
			t.Errorf("第 %d 轮后运营改过的中文值被覆盖：实际 %q（ON CONFLICT DO NOTHING 下不该发生）", round, got)
		}
		if got := valueOf(t, db, "admin.orders.bulkStatusConfirm", "ja-JP"); got != "運営が入れた日本語" {
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

func seedValues(t *testing.T, sqlText string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(sqlText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "('") {
			continue
		}
		parts := strings.Split(line, "', '")
		if len(parts) < 3 {
			t.Fatalf("看不懂的 VALUES 行（item_value 里若出现单引号，请改写这个解析器）：%s", line)
		}
		out[strings.TrimPrefix(parts[0], "('")+"|"+parts[1]] = strings.TrimPrefix(parts[2], "'")
	}
	if len(out) == 0 {
		t.Fatal("seed 的 SQL 里没有解析出任何 (key, lang) 行：SQL 形状变了就更新这个解析器")
	}
	return out
}
