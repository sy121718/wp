package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestNavigationAddSourceSeedGuardCoversBothLanguages(t *testing.T) {
	seed := findSeedByVersion(t, "601-navigation-add-source-ui-i18n")

	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != 6 {
		t.Fatalf("601 的 SQL 应有 6 个 (key, lang) 组合（3 key × 2 语言），实际 %d 个：%v", len(pairs), pairs)
	}
	langs := map[string]int{}
	keys := map[string]bool{}
	for _, p := range pairs {
		langs[p.lang]++
		keys[p.key] = true
	}
	if langs["zh-CN"] != 3 || langs["en-US"] != 3 || len(langs) != 2 {
		t.Fatalf("601 的 SQL 语言分布应为 zh-CN 3 / en-US 3，实际 %v", langs)
	}
	if len(keys) != 3 {
		t.Fatalf("601 的 SQL 应覆盖 3 个 key，实际 %d 个：%v", len(keys), sortedKeys(keys))
	}

	guardKeys := navigationKeysMentionedIn(t, seed.ConditionSQL)
	if strings.Join(guardKeys, "|") != strings.Join(sortedKeys(keys), "|") {
		t.Fatalf("判据点名的 key 与 SQL 不一致：\n判据 %v\nSQL  %v", guardKeys, sortedKeys(keys))
	}
	for _, lang := range sortedLangs(langs) {
		if !strings.Contains(seed.ConditionSQL, "'"+lang+"'") {
			t.Fatalf("判据没点名 SQL 里用到的语言 %s（只数中文会让英文缺行被静默跳过）：%s", lang, seed.ConditionSQL)
		}
	}
	if strings.Contains(seed.ConditionSQL, "LIKE") {
		t.Fatalf("判据不得用前缀 LIKE（AGENTS.md §数据库）：%s", seed.ConditionSQL)
	}
	if !strings.Contains(seed.ConditionSQL, "COUNT(*) >= 6") {
		t.Fatalf("判据的上界应为本批的 6 个组合，实际：%s", seed.ConditionSQL)
	}
}

func TestNavigationAddSourceSeedSkipsWhenAllSixPairsPresent(t *testing.T) {
	seed := findSeedByVersion(t, "601-navigation-add-source-ui-i18n")
	keys := map[string]bool{}
	for _, p := range seedPairs(t, seed.SQL) {
		keys[p.key] = true
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds（601 的 SQL 会在这里真执行）：%v", err)
	}
	if got := countPairs(t, db, keys, ""); got != 6 {
		t.Fatalf("首轮后 601 的 6 行应齐全，实际 %d 行", got)
	}
	if applied := guardValue(t, db, seed); applied != 1 {
		t.Fatalf("六项齐时判据应为 1（跳过），实际 %d", applied)
	}

	const operatorValue = "运营改过的中文（不得被 seed 覆盖）"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = 'zh-CN'",
		operatorValue, "admin.navigations.addSource.result.partial")

	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮 RunSeeds：%v", round, err)
		}
		if applied := guardValue(t, db, seed); applied != 1 {
			t.Fatalf("第 %d 轮后判据应仍为 1，实际 %d", round, applied)
		}
		if got := countPairs(t, db, keys, ""); got != 6 {
			t.Errorf("第 %d 轮后本批行数应仍为 6（判据为 1 时不再写库），实际 %d", round, got)
		}
		if got := valueOf(t, db, "admin.navigations.addSource.result.partial", "zh-CN"); got != operatorValue {
			t.Errorf("第 %d 轮后运营值被改写：实际 %q", round, got)
		}
	}
}

func TestNavigationAddSourceSeedRunsWhenOneEnglishIsMissing(t *testing.T) {
	seed := findSeedByVersion(t, "601-navigation-add-source-ui-i18n")
	keys := map[string]bool{}
	for _, p := range seedPairs(t, seed.SQL) {
		keys[p.key] = true
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds：%v", err)
	}
	seedEN := valueOf(t, db, "admin.navigations.addSource.result.allAdded", "en-US")
	if seedEN == "" {
		t.Fatal("首轮后 addSource.result.allAdded/en-US 应有值")
	}

	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'",
		"admin.navigations.addSource.result.allAdded")
	if got := countPairs(t, db, keys, "zh-CN"); got != 3 {
		t.Fatalf("造场景失败：中文应齐全（3 行），实际 %d 行", got)
	}
	if applied := guardValue(t, db, seed); applied != 0 {
		t.Fatalf("中文齐、英文缺一条时判据必须为 0（否则会静默跳过、缺的英文补不上），实际 %d", applied)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("补种 RunSeeds：%v", err)
	}
	if got := valueOf(t, db, "admin.navigations.addSource.result.allAdded", "en-US"); got != seedEN {
		t.Errorf("缺的英文应被补回 %q，实际 %q", seedEN, got)
	}
	if got := countPairs(t, db, keys, ""); got != 6 {
		t.Errorf("补种后应回到 6 行，实际 %d 行", got)
	}
	if applied := guardValue(t, db, seed); applied != 1 {
		t.Errorf("补种后判据应翻成 1，实际 %d", applied)
	}
}

func TestNavigationAddSourceSeedBackfillPreservesOperatorValues(t *testing.T) {
	seed := findSeedByVersion(t, "601-navigation-add-source-ui-i18n")
	keys := map[string]bool{}
	for _, p := range seedPairs(t, seed.SQL) {
		keys[p.key] = true
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds：%v", err)
	}

	const operatorValue = "运营改过的中文（不得被 seed 覆盖）"
	const operatorJA = "運営が入れた日本語"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = 'zh-CN'",
		operatorValue, "admin.navigations.addSource.result.allSkipped")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'",
		"admin.navigations.addSource.result.partial")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
		VALUES (?, 'ja-JP', ?, 200, 'admin', 'operator', 1, now(), now())`,
		"admin.navigations.addSource.result.partial", operatorJA)

	if applied := guardValue(t, db, seed); applied != 0 {
		t.Fatalf("缺一行英文时判据必须为 0，实际 %d", applied)
	}

	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮补种 RunSeeds：%v", round, err)
		}
		if got := valueOf(t, db, "admin.navigations.addSource.result.allSkipped", "zh-CN"); got != operatorValue {
			t.Errorf("第 %d 轮后运营改过的中文值被覆盖：实际 %q", round, got)
		}
		if got := valueOf(t, db, "admin.navigations.addSource.result.partial", "en-US"); got == "" {
			t.Errorf("第 %d 轮后缺的英文应已补回，实际为空", round)
		}
		if got := valueOf(t, db, "admin.navigations.addSource.result.partial", "ja-JP"); got != operatorJA {
			t.Errorf("第 %d 轮后第三语言行被动了：实际 %q", round, got)
		}

		if got := countPairs(t, db, keys, ""); got != 7 {
			t.Errorf("第 %d 轮后本批应有 7 行（6 + 第三语言 1），实际 %d 行", round, got)
		}
		if got := countPairs(t, db, keys, "ja-JP"); got != 1 {
			t.Errorf("第 %d 轮后第三语言行数应仍为 1，实际 %d（seed 只写中英两种语言）", round, got)
		}
	}
}
