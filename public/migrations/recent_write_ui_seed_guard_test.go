package migrations_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestRecentWriteUISeedGuardsRequireCompleteBilingualSet(t *testing.T) {
	const (
		zhLang     = "zh-CN"
		enLang     = "en-US"
		thirdLang  = "ja-JP"
		thirdValue = "運営が入れた日本語"
	)
	batches := []struct {
		version string
		keys    []string
	}{
		{"590-project-page-jump-i18n", []string{
			"admin.theme.ok.created", "admin.theme.ok.activated", "admin.theme.ok.deleted"}},
		{"591-navigation-jump-i18n", []string{"admin.navigations.actionDone"}},
		{"592-admin-ui-new-keys-i18n", []string{
			"admin.datarules.back_to_edit", "admin.i18n.deleted",
			"admin.products.actionDone", "admin.products.bulk.noneSelected"}},
		{"593-admin-blocks-write-ui-i18n", []string{
			"admin.blocks.write.refresh", "admin.blocks.write.noJs"}},
		{"595-admin-i18n-write-ui-i18n", []string{
			"admin.i18nWrite.bulkResult.allDeleted", "admin.i18nWrite.bulkResult.allSkipped",
			"admin.i18nWrite.bulkResult.partial", "admin.i18nWrite.refresh", "admin.i18nWrite.noJs"}},
	}

	type plan struct {
		seed  migrations.Seed
		keys  map[string]bool
		pairs int
	}
	plans := make([]plan, 0, len(batches))
	for _, b := range batches {
		seed := findSeedByVersion(t, b.version)
		keys := map[string]bool{}
		for _, k := range b.keys {
			keys[k] = true
		}
		want := map[string]bool{}
		for k := range keys {
			want[k+"|"+zhLang] = true
			want[k+"|"+enLang] = true
		}
		pairs := seedPairs(t, seed.SQL)
		got := map[string]bool{}
		for _, p := range pairs {
			got[p.key+"|"+p.lang] = true
		}
		if strings.Join(sortedKeys(got), "|") != strings.Join(sortedKeys(want), "|") {
			t.Fatalf("%s 的 SQL 不是 key × {zh-CN, en-US} 的完整笛卡尔积：\n实际 %v\n期望 %v",
				b.version, sortedKeys(got), sortedKeys(want))
		}
		if len(got) != len(pairs) {
			t.Fatalf("%s 的 SQL 有重复 (key, lang) 组合：%v", b.version, sortedKeys(got))
		}
		if gk := guardSeedKeys(seed.ConditionSQL); strings.Join(gk, "|") != strings.Join(sortedKeys(keys), "|") {
			t.Fatalf("%s 的判据点名的 key 与 SQL 不一致：\n判据 %v\nSQL  %v", b.version, gk, sortedKeys(keys))
		}
		for _, lang := range []string{zhLang, enLang} {
			if !strings.Contains(seed.ConditionSQL, "'"+lang+"'") {
				t.Fatalf("%s 的判据没点名语言 %s（只数一种语言时另一种缺行仍判已完成）：%s",
					b.version, lang, seed.ConditionSQL)
			}
		}
		if strings.Contains(strings.ToUpper(seed.ConditionSQL), "LIKE") {
			t.Fatalf("%s 的判据不得用前缀 LIKE（AGENTS.md §数据库）：%s", b.version, seed.ConditionSQL)
		}
		m := seedCountGuardRe.FindStringSubmatch(seed.ConditionSQL)
		if m == nil {
			t.Fatalf("%s 的判据形状不认识（期望 COUNT(*) >= n）：%s", b.version, seed.ConditionSQL)
		}
		if m[1] != ">=" || m[2] != strconv.Itoa(len(want)) {
			t.Fatalf("%s 的阈值必须是精确组合数 %d（低了缺词也能满足、高了永不满足→每次启动重跑），实际 `%s`：%s",
				b.version, len(want), m[0], seed.ConditionSQL)
		}
		plans = append(plans, plan{seed: seed, keys: keys, pairs: len(want)})
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds（这五批的 SQL 会在这里真执行）：%v", err)
	}
	for _, p := range plans {
		version, keys := p.seed.Version, p.keys
		order := sortedKeys(keys)
		firstKey, lastKey := order[0], order[len(order)-1]

		if got := guardValue(t, db, p.seed); got != 1 {
			t.Fatalf("%s：完整双语集合在库里时判据必须为 1，实际 %d（阈值高于真实组合数就会永远重跑）", version, got)
		}
		if got := countPairs(t, db, keys, ""); got != p.pairs {
			t.Fatalf("%s：首轮后应有 %d 行，实际 %d", version, p.pairs, got)
		}

		seedEnValue := valueOf(t, db, lastKey, enLang)
		if seedEnValue == "" {
			t.Fatalf("%s：%s/%s 在首轮后应有值", version, lastKey, enLang)
		}
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", lastKey, enLang)
		mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
			VALUES (?, ?, ?, 200, 'admin', 'operator', 1, now(), now())`, lastKey, thirdLang, thirdValue)
		if got := guardValue(t, db, p.seed); got != 0 {
			t.Fatalf("%s：中文齐、英文缺一条时必须为 0（否则静默跳过、缺的英文永远补不上）；"+
				"额外第三语言行不得把计数垫到阈值。实际 %d", version, got)
		}
		for round := 1; round <= 2; round++ {
			if err := migrations.RunSeeds(db); err != nil {
				t.Fatalf("%s：段 1 第 %d 轮补种 RunSeeds：%v", version, round, err)
			}
			if got := guardValue(t, db, p.seed); got != 1 {
				t.Fatalf("%s：段 1 第 %d 轮补种后判据应为 1，实际 %d", version, round, got)
			}
			if got := valueOf(t, db, lastKey, enLang); got != seedEnValue {
				t.Errorf("%s：段 1 第 %d 轮后缺的英文应被补回 %q，实际 %q", version, round, seedEnValue, got)
			}
			if got := valueOf(t, db, lastKey, thirdLang); got != thirdValue {
				t.Errorf("%s：段 1 第 %d 轮后第三语言行被动了：实际 %q", version, round, got)
			}
			if got := countPairs(t, db, keys, ""); got != p.pairs+1 {
				t.Errorf("%s：段 1 第 %d 轮后应有 %d 行（%d + 第三语言 1），实际 %d",
					version, round, p.pairs+1, p.pairs, got)
			}
			if got := countPairs(t, db, keys, thirdLang); got != 1 {
				t.Errorf("%s：段 1 第 %d 轮后第三语言行数应仍为 1（seed 只写中英两种语言），实际 %d",
					version, round, got)
			}
		}
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", lastKey, thirdLang)

		seedZhValue := valueOf(t, db, firstKey, zhLang)
		if seedZhValue == "" {
			t.Fatalf("%s：%s/%s 在首轮后应有值", version, firstKey, zhLang)
		}
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", firstKey, zhLang)
		if got := guardValue(t, db, p.seed); got != 0 {
			t.Fatalf("%s：英文齐、中文缺一条时必须为 0（镜像方向同样会静默跳过）。实际 %d", version, got)
		}
		for round := 1; round <= 2; round++ {
			if err := migrations.RunSeeds(db); err != nil {
				t.Fatalf("%s：段 2 第 %d 轮补种 RunSeeds：%v", version, round, err)
			}
			if got := guardValue(t, db, p.seed); got != 1 {
				t.Fatalf("%s：段 2 第 %d 轮补种后判据应为 1，实际 %d", version, round, got)
			}
			if got := valueOf(t, db, firstKey, zhLang); got != seedZhValue {
				t.Errorf("%s：段 2 第 %d 轮后缺的中文应被补回 %q，实际 %q", version, round, seedZhValue, got)
			}
			if got := countPairs(t, db, keys, ""); got != p.pairs {
				t.Errorf("%s：段 2 第 %d 轮后应回到 %d 行，实际 %d", version, round, p.pairs, got)
			}
		}

		const operatorValue = "运营改过的中文（不得被 seed 覆盖）"
		mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?",
			operatorValue, firstKey, zhLang)
		if got := guardValue(t, db, p.seed); got != 1 {
			t.Fatalf("%s：改过运营值（集合仍完整）时判据应为 1、不该再补种，实际 %d", version, got)
		}
		for round := 1; round <= 2; round++ {
			if err := migrations.RunSeeds(db); err != nil {
				t.Fatalf("%s：段 3 第 %d 轮 RunSeeds：%v", version, round, err)
			}
			if got := valueOf(t, db, firstKey, zhLang); got != operatorValue {
				t.Errorf("%s：段 3 第 %d 轮后运营改过的中文值被覆盖：实际 %q（ON CONFLICT DO NOTHING 下不该发生）",
					version, round, got)
			}
			if got := guardValue(t, db, p.seed); got != 1 {
				t.Errorf("%s：段 3 第 %d 轮后判据应为 1，实际 %d", version, round, got)
			}
		}
	}
}

var seedCountGuardRe = regexp.MustCompile(`COUNT\(\*\)\s*(>=|=)\s*(\d+)`)

var seedGuardKeyRe = regexp.MustCompile(`'([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+)'`)

func guardSeedKeys(condSQL string) []string {
	seen := map[string]bool{}
	for _, m := range seedGuardKeyRe.FindAllStringSubmatch(condSQL, -1) {
		seen[m[1]] = true
	}
	return sortedKeys(seen)
}
